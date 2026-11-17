#!/usr/bin/env python3
"""Six-hour upstream monitor. Standard library only; never sends model prompts."""

import concurrent.futures
import hashlib
import json
import os
import pathlib
import re
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request

ROOT = pathlib.Path(__file__).resolve().parents[1]
OUT = ROOT / "dist/agent-monitor"
MARKER = "<!-- nimote-agent-monitor:v1 -->"
STATE = re.compile(r"<!-- nimote-agent-state:(\{[^\n]+\}) -->")
VERSION = re.compile(r"\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?\Z")
PACKAGES = {
    "codex": "@openai/codex",
    "opencode": "opencode-ai",
    "claude-acp": "@agentclientprotocol/claude-agent-acp",
    "claude-code": "@anthropic-ai/claude-code",
    "claude-sdk": "@anthropic-ai/claude-agent-sdk",
    "acp-sdk": "@agentclientprotocol/sdk",
}
AGENT_TESTS = "Test.*(Agent|ACP|Codex|OpenCode|Adapter|Approval|Conversation|Permission)"


def request_json(url, payload=None, method=None, token=None):
    headers = {"Accept": "application/json", "User-Agent": "nimotehost-agent-monitor"}
    if token:
        headers["Authorization"] = "Bearer " + token
        headers["X-GitHub-Api-Version"] = "2022-11-28"
    data = None if payload is None else json.dumps(payload).encode()
    if data is not None:
        headers["Content-Type"] = "application/json"
    for attempt in range(3):
        try:
            req = urllib.request.Request(url, data=data, headers=headers, method=method)
            with urllib.request.urlopen(req, timeout=20) as response:
                return json.load(response)
        except urllib.error.HTTPError as error:
            # Never retry writes: a response can be lost after a successful POST.
            if data is not None or error.code not in (429, 500, 502, 503, 504) or attempt == 2:
                raise
        except (urllib.error.URLError, TimeoutError):
            if data is not None or attempt == 2:
                raise
        time.sleep(attempt + 1)


def github(path, payload=None, method=None):
    repo = os.environ["GITHUB_REPOSITORY"]
    return request_json(f"https://api.github.com/repos/{repo}/{path}", payload, method,
                        os.environ["GH_TOKEN"])


def find_issue():
    for page in range(1, 101):
        issues = github(f"issues?state=all&creator=github-actions%5Bbot%5D&per_page=100&page={page}")
        for issue in issues:
            if "pull_request" not in issue and MARKER in (issue.get("body") or ""):
                return issue
        if len(issues) < 100:
            return None
    raise RuntimeError("monitor issue search exceeded 10,000 issues")


def read_state(issue):
    if issue is None:
        return {}
    match = STATE.search(issue.get("body") or "")
    if not match:
        raise ValueError("Monitor issue state is missing; restore its hidden state or delete the issue")
    state = json.loads(match[1])
    if not isinstance(state, dict) or not isinstance(state.get("versions"), dict):
        raise ValueError("Invalid monitor issue state")
    return state


def latest_versions():
    def fetch(entry):
        name, package = entry
        info = request_json(f"https://registry.npmjs.org/{package}/latest")
        version = info["version"]
        if not VERSION.fullmatch(version):
            raise ValueError(f"Invalid published version for {name}")
        return name, version

    with concurrent.futures.ThreadPoolExecutor(max_workers=6) as pool:
        versions = dict(pool.map(fetch, PACKAGES.items()))
    info = request_json("https://proxy.golang.org/github.com/coder/acp-go-sdk/@latest")
    version = info["Version"]
    if not version.startswith("v") or not VERSION.fullmatch(version[1:]):
        raise ValueError("Invalid ACP Go SDK version")
    versions["acp-go-sdk"] = version
    return versions


def should_check(versions, previous, force=False):
    return (force or versions != previous.get("versions") or
            (previous.get("status") != "pass" and previous.get("attempts", 0) < 2))


def write_json(path, data):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(data, indent=2) + "\n")


def summary(text):
    print(text)
    if os.environ.get("GITHUB_STEP_SUMMARY"):
        with open(os.environ["GITHUB_STEP_SUMMARY"], "a") as output:
            output.write(text + "\n")


def detect():
    versions = latest_versions()
    issue = find_issue()
    previous = read_state(issue)
    changed = should_check(versions, previous, os.getenv("MONITOR_FORCE") == "true")
    write_json(OUT / "snapshot.json", {"versions": versions, "previous": previous,
                                      "issue": issue["number"] if issue else None})
    if os.environ.get("GITHUB_OUTPUT"):
        with open(os.environ["GITHUB_OUTPUT"], "a") as output:
            output.write(f"changed={str(changed).lower()}\n")
    summary("Upstream versions:\n\n" + "\n".join(f"- {k}: `{v}`" for k, v in versions.items()) +
            ("\n\nChecks requested." if changed else "\n\nUnchanged; no CLI installs or tests."))


def run(command, log, env=None, timeout=180):
    # No shell expansion. Subprocesses never inherit the GitHub API token.
    child_env = dict(os.environ if env is None else env)
    if env is None:
        # Do not accidentally opt in to paid/live or root acceptance from a
        # developer's shell when running the monitor locally.
        child_env = {k: v for k, v in child_env.items() if not k.startswith("NIMOTE_TEST_")}
    for key in ("GH_TOKEN", "GITHUB_TOKEN", "NODE_AUTH_TOKEN", "NPM_TOKEN"):
        child_env.pop(key, None)
    log.parent.mkdir(parents=True, exist_ok=True)
    with log.open("w") as output:
        process = subprocess.Popen(command, cwd=ROOT, env=child_env, stdout=output,
                                   stderr=subprocess.STDOUT, start_new_session=True)
        try:
            return process.wait(timeout=timeout) == 0
        except subprocess.TimeoutExpired:
            import signal
            try:
                os.killpg(process.pid, signal.SIGKILL)
            except ProcessLookupError:
                # The command may exit between wait() timing out and killpg().
                pass
            process.wait()
            output.write("\nMonitor command timed out.\n")
            return False


def check():
    snapshot = json.loads((OUT / "snapshot.json").read_text())
    versions = snapshot["versions"]
    for name, version in versions.items():
        if not VERSION.fullmatch(version.removeprefix("v")):
            raise ValueError(f"Invalid version: {name}")
    results = {}
    # Compile once and reuse the same test binary for all three installations.
    binary = OUT / "host.test"
    results["build"] = run(["go", "test", "-mod=readonly", "-c", "-o", str(binary),
                            "./internal/host"], OUT / "build.log", timeout=300)
    write_json(OUT / "results.json", results)
    if results["build"]:
        results["regressions"] = run([str(binary), f"-test.run={AGENT_TESTS}", "-test.timeout=4m"],
                                     OUT / "regressions.log", timeout=260)
        write_json(OUT / "results.json", results)
    for provider, names in {"codex": ["codex"], "opencode": ["opencode"],
                            "claude": ["claude-acp", "claude-code"]}.items():
        with tempfile.TemporaryDirectory(prefix=f"nimote-monitor-{provider}-") as temporary:
            home = pathlib.Path(temporary)
            env = {k: v for k, v in os.environ.items()
                   if k in ("PATH", "LANG", "LC_ALL", "TMPDIR", "SYSTEMROOT")}
            for variable, directory in {
                "HOME": "home", "XDG_CONFIG_HOME": "config", "XDG_DATA_HOME": "data",
                "XDG_CACHE_HOME": "cache", "XDG_STATE_HOME": "state",
                "CODEX_HOME": "codex", "CLAUDE_CONFIG_DIR": "claude",
            }.items():
                path = home / directory
                path.mkdir()
                env[variable] = str(path)
            prefix = home / "tools"
            packages = [f"{PACKAGES[name]}@{versions[name]}" for name in names]
            installed = run(["npm", "install", "--prefix", str(prefix), "--no-audit",
                             "--no-fund", "--save-exact", "--loglevel=info",
                             "--fetch-retries=1", "--fetch-timeout=60000", *packages],
                            OUT / f"{provider}-install.log", env, timeout=180)
            binaries = {"codex": ["codex"], "opencode": ["opencode"],
                        "claude": ["claude-agent-acp", "claude"]}[provider]
            # Missing npm links must not silently select a globally installed CLI.
            installed = installed and all(
                os.access(prefix / "node_modules/.bin" / name, os.X_OK) for name in binaries)
            results[f"{provider}-install"] = installed
            if installed:
                run(["npm", "ls", "--prefix", str(prefix), "--depth=1", "--json"],
                    OUT / f"{provider}-dependencies.json", env, timeout=30)
            env["PATH"] = str(prefix / "node_modules/.bin") + os.pathsep + env["PATH"]
            env["NIMOTE_TEST_COMPAT_AGENT"] = provider
            results[f"{provider}-startup"] = bool(installed and results["build"] and run(
                [str(binary), "-test.run=^TestInstalledAgentCompatibility$", "-test.v",
                 "-test.timeout=75s"], OUT / f"{provider}-startup.log", env, timeout=90))
            if provider == "codex" and installed:
                results["codex-schema"] = run(
                    [str(prefix / "node_modules/.bin/codex"), "app-server", "generate-json-schema",
                     "--out", str(OUT / "codex-schema")], OUT / "codex-schema.log", env)
            write_json(OUT / "results.json", results)
    summary("No-model checks:\n\n" + "\n".join(
        f"- {name}: {'PASS' if passed else 'NEEDS INVESTIGATION'}" for name, passed in results.items()))
    return 0 if all(results.values()) else 1


def make_report(snapshot, results, job_result, run_url):
    versions, previous = snapshot["versions"], snapshot["previous"]
    passed = job_result == "success" and bool(results) and all(results.values())
    status = "pass" if passed else "needs-investigation"
    attempts = previous.get("attempts", 0) + 1 if versions == previous.get("versions") else 1
    state = {"versions": versions, "status": status, "attempts": attempts}
    fingerprint = hashlib.sha256(json.dumps(versions, sort_keys=True).encode()).hexdigest()[:12]
    lines = [MARKER, f"Agent compatibility monitor: **{status.upper()}**", "",
             f"[Workflow run and logs]({run_url}) · snapshot `{fingerprint}`", "",
             "| Upstream | Previous checked | Current latest |", "| --- | --- | --- |"]
    for name, version in versions.items():
        package = PACKAGES.get(name)
        url = (f"https://www.npmjs.com/package/{package}/v/{version}" if package else
               f"https://github.com/coder/acp-go-sdk/releases/tag/{version}")
        old = previous.get("versions", {}).get(name, "—")
        lines.append(f"| [{name}]({url}) | {old} | {version} |")
    lines += ["", "| Check | Result |", "| --- | --- |"]
    for name, result in results.items():
        lines.append(f"| {name} | {'PASS' if result else 'NEEDS INVESTIGATION'} |")
    if not results:
        lines.append("| Check job | No results; inspect workflow setup/install logs |")
    lines += ["", "Startup checks use the real installed CLIs and Host structured session creation.",
              "No prompt, model request, API key, or paid model acceptance is used.",
              "PASS covers startup and existing fixture regressions, not full runtime compatibility.",
              "Authentication, installation and network failures also require investigation; they do not prove a protocol break.",
              "Claude SDK, ACP SDK and ACP Go SDK latest versions are watch signals; checks use the adapter's resolved dependencies and this repository's pinned Go SDK.",
              "Codex JSON Schema is exported as an artifact; semantic schema diffing is not performed.",
              "", "\u9996\u6b21\u8fd0\u884c\u5efa\u7acb\u57fa\u7ebf；\u540e\u7eed\u6bcf 6 \u5c0f\u65f6\u68c0\u6d4b\u7248\u672c\u53d8\u5316，\u6709\u53d8\u5316\u624d\u8fd0\u884c\u68c0\u67e5。\u5931\u8d25\u7684\u540c\u4e00\u7248\u672c\u81ea\u52a8\u91cd\u8bd5\u4e00\u6b21，\u4e4b\u540e\u53ef\u624b\u52a8 force。",
              "\u8ba2\u9605\u6b64 Issue \u63a5\u6536\u66f4\u65b0；\u90ae\u4ef6\u53d6\u51b3\u4e8e\u4f60\u7684 GitHub \u901a\u77e5\u8bbe\u7f6e。\u4e0d\u8981\u5220\u9664\u6b63\u6587\u672b\u5c3e\u7684\u72b6\u6001\u6807\u8bb0。",
              "", f"<!-- nimote-agent-state:{json.dumps(state, separators=(',', ':'))} -->"]
    return "\n".join(lines), status


def report():
    snapshot = json.loads((OUT / "snapshot.json").read_text())
    result_path = OUT / "checks/results.json"
    results = json.loads(result_path.read_text()) if result_path.exists() else {}
    run_url = (f"{os.environ.get('GITHUB_SERVER_URL', 'https://github.com')}/"
               f"{os.environ['GITHUB_REPOSITORY']}/actions/runs/{os.environ['GITHUB_RUN_ID']}")
    body, status = make_report(snapshot, results, os.environ["MONITOR_CHECK_RESULT"], run_url)
    payload = {"title": f"Agent compatibility monitor: {status}", "body": body}
    issue_number = snapshot["issue"]
    if issue_number:
        payload["state"] = "open"
        # Body updates alone do not reliably notify subscribers.
        # Publish the notification before advancing the saved baseline. If the
        # comment fails, the next poll must still detect and report this change.
        github(f"issues/{issue_number}/comments", {"body": body})
        issue = github(f"issues/{issue_number}", payload, "PATCH")
    else:
        issue = github("issues", payload)
    summary(f"Monitor result: **{status}**. [Subscribe to the monitor issue]({issue['html_url']}).")


if __name__ == "__main__":
    if len(sys.argv) != 2 or sys.argv[1] not in ("detect", "check", "report"):
        raise SystemExit("usage: agent_compatibility.py detect|check|report")
    sys.exit({"detect": detect, "check": check, "report": report}[sys.argv[1]]())
