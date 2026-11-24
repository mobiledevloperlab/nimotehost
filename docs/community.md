# NimoteHost Community

Community keeps protocol 1 and the existing Host CLI, GUI, services, SSH RPC,
MCP, Go SDK and HCP plug-ins. Agent adapters and their approval policies,
sessions, worktrees, diff and explicit verification belong to Host. Connect
links GitHub Issue orchestration into a downstream executable. Community
reports both GitHub Issue capability flags as false and its owner API returns
404 for the Issue task route.

Existing Issue records and worktrees remain under the same private Host state
directory. Connect reads the same records, receipts, worktrees and session
evidence using the same user account and state directory. The legacy full
Host release also reads that data. Deploy uninstall retains state by default.

## Build and install

Use the Go version in go.mod or a later compatible patch. Run
    go run ./tools/build
to make a CLI distribution for this machine. Pass -gui on a desktop machine
with the native C compiler and GUI dependencies. The default distribution
version is 2.5.0-community. Extract a bundle and run its installer as the
desktop user. On Windows, run the bundled PowerShell script or
nimotehost.exe setup. The Host service keeps the existing label and fixed CLI
path.

The Community workflow publishes stable releases for
`community-vMAJOR.MINOR.PATCH` tags. Community CLI and GUI discover them independently.
For a specific version, use an extracted bootstrap outside the install path.

    nimotehost deploy upgrade --version community-v2.5.0

GitHub's repository-wide Latest release remains v2.4.1 for legacy full Host
clients. Community clients look up their own release tags.

Installed Connect Hosts use the Connect bundle for updates and removal.
Community setup checks the installed edition before stopping a service or
activating files. It also checks for active GitHub Issue tasks on legacy full
Hosts before making an in-place upgrade.
To switch editions intentionally, stop active work and use the installed
package's documented uninstall procedure with state retention, then install
the desired edition. Keep the previous verified archive or the backup path
reported by setup for recovery. If setup fails to start the new Host, its
transaction restores the previous distribution and service state.
