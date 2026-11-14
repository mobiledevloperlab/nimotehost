# Changelog

**English** | [简体中文](CHANGELOG.zh-CN.md)

## Unreleased — 2.4.0

- Refine desktop spacing, labeled sidebar navigation, status and metric cards, and light/dark palettes; open current approvals directly and cover contrast and control sizing.

- Simplify the desktop to four main pages, readable session/device names, search and filters, conversation text, and on-demand technical details.
- Match NimoteCode's six languages (English, Simplified Chinese, Spanish, Japanese, Korean, Russian), reusing go-i18n with immediate switching and persistent preferences.
- Add catalog, template, layout, session-state and GUI lifecycle checks; run race and vet in desktop CI.

- Preserve executable modes in GUI bundles, isolate damaged session output, and require resync for future event cursors.
- Bound encoded history projections, disclose truncation, and page Codex transcripts in the GUI.
- Add expiring mutation IDs with a durable monotonic retirement watermark and safe receipt cleanup; retain legacy replay protection and expose storage diagnostics.
- Sync parent directories after atomic state replacements on Unix; preserve the Windows durability boundary.

- Persist mutation reservations/receipts and evidenced final Agent state; reject
  conflicting or uncertain retries without replaying work.
- Add Task-linked sessions, bounded lifecycle history, cursor-based foreground
  notifications and workspace-guarded existing Git review.
- Consolidate maintained docs into [completed](docs/completed.md) and
  [pending](docs/pending.md) functionality; remove historical/duplicate reports.

- Reuse the existing Coder SDK's generic JSON-RPC engine for Codex/ACP sessions
  and history queries; centralize transport lifecycle and approval handoff.
- Cancel RPC workers before joining on invalid streams, require Bearer authentication,
  accept JSON media-type parameters, reject oversized RPC replies, and isolate mutable session results.
- Split HTTP history routes and RPC client code; align history query/server/client deadlines.

- Validate native macOS 26.2/arm64 launchd and Windows build 26200/amd64 Task
  Scheduler setup, restart, idle upgrade and uninstall in logged-in user sessions.
- Reload the saved LaunchAgent definition on explicit restart to avoid a stuck
  kickstart after clean shutdown; retain its saved environment.
- Encode Task Scheduler XML as UTF-16LE with a BOM and restore/remove the local
  definition after registration failure, allowing setup rollback to complete.
- Include native command stderr in service errors without exposing command stdout.

- Add one-command installers for source and offline bundles, verified transactional setup, retained rollback bundles, and atomic idle-only shutdown for safe updates.
- Simplify single-terminal setup; verify readiness, protect running tasks during reinstall, preserve tool paths on Unix, and provide actionable service diagnostics.
- Package deployable archives with bilingual installation and service guidance.
- Provide English and Simplified Chinese documentation.
- Extract standalone NimoteHost from Nimote Connect.
- Authenticated loopback RPC, persistent tasks/output/events, PTY/ConPTY and
  installed Codex/Claude Code/OpenCode terminal adapters.
- Per-user Linux/macOS/Windows service integration and opt-in MCP write tools.
- Cross-platform versioned builds with dependency notices and SHA256 checksums.

These service checks do not cover OS reboot or user logout. Full mobile/Agent
handoff and real-agent approval acceptance remain outstanding. Structured Codex/ACP
approval interfaces exist; the umbrella capability stays conservative. This is a development preview.
