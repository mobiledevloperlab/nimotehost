"""Offline checks for update detection, durable issue state and failure reporting."""
import importlib.util
import json
import pathlib
import unittest
from unittest.mock import patch

SCRIPT = pathlib.Path(__file__).resolve().parents[1] / "scripts/agent_compatibility.py"
SPEC = importlib.util.spec_from_file_location("agent_monitor", SCRIPT)
monitor = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(monitor)


class AgentMonitorTests(unittest.TestCase):
    @unittest.skipUnless(hasattr(monitor.os, "killpg"), "POSIX process group cleanup")
    def test_timeout_remains_a_failure_if_process_exits_before_kill(self):
        import tempfile
        with tempfile.TemporaryDirectory() as temporary, \
             patch.object(monitor.subprocess, "Popen") as process, \
             patch.object(monitor.os, "killpg", side_effect=ProcessLookupError):
            process.return_value.wait.side_effect = [monitor.subprocess.TimeoutExpired("fixture", 1), 0]
            log = pathlib.Path(temporary) / "timeout.log"
            self.assertFalse(monitor.run(["fixture"], log, timeout=1))
            self.assertIn("timed out", log.read_text())

    def test_commands_do_not_inherit_tokens_or_paid_test_opt_ins(self):
        import os
        import tempfile
        with tempfile.TemporaryDirectory() as temporary, \
             patch.dict(os.environ, {"GH_TOKEN": "fixture", "GITHUB_TOKEN": "fixture",
                                     "NPM_TOKEN": "fixture", "NIMOTE_TEST_LIVE_AGENT": "claude"}), \
             patch.object(monitor.subprocess, "Popen") as process:
            process.return_value.wait.return_value = 0
            self.assertTrue(monitor.run(["example", "literal;$value"], pathlib.Path(temporary) / "log"))
            kwargs = process.call_args.kwargs
            self.assertNotIn("shell", kwargs)
            for key in ("GH_TOKEN", "GITHUB_TOKEN", "NPM_TOKEN", "NIMOTE_TEST_LIVE_AGENT"):
                self.assertNotIn(key, kwargs["env"])

    def test_first_run_change_force_and_bounded_failure_retry(self):
        versions = {"codex": "1.2.3"}
        self.assertTrue(monitor.should_check(versions, {}))
        baseline = {"versions": versions, "status": "pass", "attempts": 1}
        self.assertFalse(monitor.should_check(versions, baseline))
        self.assertTrue(monitor.should_check(versions, baseline, force=True))
        self.assertTrue(monitor.should_check({"codex": "1.2.4"}, baseline))
        baseline["status"] = "needs-investigation"
        self.assertTrue(monitor.should_check(versions, baseline))
        baseline["attempts"] = 2
        self.assertFalse(monitor.should_check(versions, baseline))

    def test_closed_issue_is_reused_across_pages(self):
        issue = {"number": 12, "state": "closed", "body": monitor.MARKER}
        with patch.object(monitor, "github", side_effect=[
            [{"body": "unrelated"}] * 100, [issue]
        ]) as api:
            self.assertEqual(monitor.find_issue(), issue)
        self.assertIn("state=all", api.call_args_list[0].args[0])
        self.assertIn("creator=github-actions", api.call_args_list[0].args[0])
        self.assertIn("page=2", api.call_args_list[1].args[0])

    def test_empty_repo_does_not_need_an_existing_issue(self):
        with patch.object(monitor, "github", return_value=[]):
            self.assertIsNone(monitor.find_issue())
        self.assertEqual(monitor.read_state(None), {})

    def test_report_roundtrip_and_no_false_success_after_job_failure(self):
        snapshot = {"versions": {"codex": "1.2.3"}, "previous": {}}
        for results, outcome in [({}, "failure"), ({"build": True}, "failure"),
                                 ({"startup": False}, "success")]:
            with self.subTest(results=results, outcome=outcome):
                body, status = monitor.make_report(snapshot, results, outcome, "https://example.test/run")
                self.assertEqual(status, "needs-investigation")
                state = monitor.read_state({"body": body})
                self.assertEqual(state["versions"], snapshot["versions"])
                self.assertEqual(state["attempts"], 1)
        body, status = monitor.make_report(snapshot, {"startup": True}, "success", "https://example.test/run")
        self.assertEqual(status, "pass")
        self.assertIn("not full runtime compatibility", body)

    def test_failed_snapshot_retry_does_not_reset_attempts(self):
        versions = {"codex": "1.2.3"}
        snapshot = {"versions": versions, "previous": {"versions": versions, "attempts": 1}}
        body, _ = monitor.make_report(snapshot, {}, "failure", "https://example.test/run")
        self.assertEqual(monitor.read_state({"body": body})["attempts"], 2)

    def test_corrupt_state_fails_instead_of_silently_resetting_baseline(self):
        for body in (monitor.MARKER, '<!-- nimote-agent-state:{"versions":[]} -->'):
            with self.subTest(body=body), self.assertRaises(ValueError):
                monitor.read_state({"body": body})

    def test_registry_versions_are_validated_before_install(self):
        with patch.object(monitor, "request_json", return_value={"version": "1.2.3;touch /tmp/no"}):
            with self.assertRaises(ValueError):
                monitor.latest_versions()

    def test_missing_local_binaries_cannot_pass_using_global_clis(self):
        import tempfile
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            monitor.write_json(root / "snapshot.json", {
                "versions": {name: "1.2.3" for name in monitor.PACKAGES},
            })
            with patch.object(monitor, "OUT", root), \
                 patch.object(monitor, "run", return_value=True) as command, \
                 patch.object(monitor.os, "access", return_value=False), \
                 patch.object(monitor, "summary"):
                self.assertEqual(monitor.check(), 1)
            results = json.loads((root / "results.json").read_text())
            for provider in ("codex", "opencode", "claude"):
                self.assertFalse(results[provider + "-startup"])
            self.assertFalse(any("-test.run=^TestInstalledAgentCompatibility$" in call.args[0]
                                 for call in command.call_args_list))

    def test_detect_skips_unchanged_versions_and_emits_output(self):
        import os
        import tempfile
        versions = {"codex": "1.2.3"}
        state = {"versions": versions, "status": "pass", "attempts": 1}
        issue = {"number": 12, "body": monitor.MARKER +
                 "\n<!-- nimote-agent-state:" + json.dumps(state) + " -->"}
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            with patch.object(monitor, "OUT", root), \
                 patch.object(monitor, "latest_versions", return_value=versions), \
                 patch.object(monitor, "find_issue", return_value=issue), \
                 patch.object(monitor, "summary"), \
                 patch.dict(os.environ, {"GITHUB_OUTPUT": str(root / "output"), "MONITOR_FORCE": "false"}):
                monitor.detect()
            self.assertEqual((root / "output").read_text(), "changed=false\n")
            self.assertEqual(json.loads((root / "snapshot.json").read_text())["issue"], 12)

    def test_update_posts_comment_to_notify_subscribers(self):
        import os
        import tempfile
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            monitor.write_json(root / "snapshot.json", {
                "versions": {"codex": "1.2.3"}, "previous": {}, "issue": 12,
            })
            with patch.object(monitor, "OUT", root), \
                 patch.object(monitor, "summary"), \
                 patch.object(monitor, "github", return_value={"html_url": "https://example.test/12"}) as api, \
                 patch.dict(os.environ, {"GITHUB_REPOSITORY": "owner/repo", "GITHUB_RUN_ID": "123",
                                         "MONITOR_CHECK_RESULT": "failure"}):
                monitor.report()
            self.assertEqual(api.call_args_list[0].args[0], "issues/12/comments")
            self.assertEqual(api.call_args_list[1].args[0], "issues/12")
            self.assertIn("NEEDS-INVESTIGATION", api.call_args_list[0].args[1]["body"])

    def test_notification_failure_does_not_advance_baseline(self):
        import os
        import tempfile
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            monitor.write_json(root / "snapshot.json", {
                "versions": {"codex": "1.2.3"}, "previous": {}, "issue": 12,
            })
            with patch.object(monitor, "OUT", root), \
                 patch.object(monitor, "github", side_effect=RuntimeError("notification unavailable")) as api, \
                 patch.dict(os.environ, {"GITHUB_REPOSITORY": "owner/repo", "GITHUB_RUN_ID": "123",
                                         "MONITOR_CHECK_RESULT": "failure"}):
                with self.assertRaisesRegex(RuntimeError, "notification unavailable"):
                    monitor.report()
            api.assert_called_once()
            self.assertEqual(api.call_args.args[0], "issues/12/comments")
