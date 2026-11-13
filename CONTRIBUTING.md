# Contributing

**English** | [简体中文](CONTRIBUTING.zh-CN.md)

Use Go 1.26.5 or later. Work from the Host module root:

```sh
go test -mod=readonly -race ./...
go vet ./...
go run ./tools/build
```

Keep changes focused and explain the behavior, compatibility impact and checks
in the pull request. Include regression coverage for changes to authentication,
persistence, process lifecycle, terminal handling and protocol validation.
Windows and macOS changes need the respective native CI results. Do not put
private credentials, command output or local state in issues, fixtures or PRs.

Contributions are submitted under Apache-2.0. Submit only work you are authorized
to license. Preserve upstream copyright and license notices. Disclose copied
or generated code and review its provenance and correctness before submission.
No copyright assignment or separate contributor agreement is required.

See [maintenance and releases](docs/maintenance.md) for dependency updates and
compatibility rules, and [SECURITY.md](SECURITY.md) for private vulnerability
reporting.

Keep English and Chinese documentation in sync when changing public behavior,
commands or protocols. Do not translate command names, JSON fields, paths or
capability names.

## Governance and decisions

The maintainers listed in `.github/CODEOWNERS` review changes, merge PRs and
publish releases. Repository administrators manage access and branch protection.
Contributors retain authorship; submitting a PR does not grant merge access.

Open an issue before implementing a new adapter, dependency, protocol/state
change or substantial architecture change. Describe the problem, proposed API,
compatibility, alternatives and validation plan. Record the accepted direction
and reasons in that issue before starting a large implementation. Small fixes
and documentation corrections can go directly to a PR.

A maintainer other than the author should approve a PR before merge. Required CI
must pass, review comments must be resolved, and public behavior changes need
matching documentation and regression coverage. If no independent reviewer is
available, record that exception and its rationale in the PR; do not describe
self-review as independent approval. Security reports follow SECURITY.md.

Discuss technical disagreements in the issue, with reproducible evidence. The
responsible CODEOWNER records the final decision and tradeoffs. Maintainers may
close proposals outside the documented scope, with an explanation. Keep debate
respectful; report harassment privately through the contact process in SECURITY.md.
There is no response-time guarantee during the development preview. If a request
has no response after 14 days, post one reminder in the same thread; lack of a
reply does not imply acceptance.

Maintainer candidates should have a history of useful contributions and reviews.
An existing maintainer proposes the scope of access in a public issue; existing
maintainers record agreement before an administrator grants access. Maintainership
can be relinquished or removed for inactivity or abuse; record the change and
handoff publicly without disclosing private security or personal information.

## Adapter contributions and clean builds

Follow the [adapter development guide](docs/agent-adapters.md) for Agent changes.
On Linux/macOS with Go 1.26.5+ and a C compiler (for race tests), run
`sh scripts/check-clean-build.sh` from a source checkout. It copies source to a
temporary directory, disables parent workspaces and toolchain auto-downloads,
uses empty module/build caches, downloads dependencies, verifies modules, runs
race tests and vet, and builds the full native distribution. It requires network
access to dependencies and does not install or stop Host. Temporary files are
removed on exit. Set `TMPDIR` to a disk with enough space if needed.
Native Windows checks run separately in CI; this shell command requires a Unix
environment. The minimum Go version is declared in go.mod; newer Go versions
are supported subject to CI and the same clean-build checks.
