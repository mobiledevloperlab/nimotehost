# Deploy and maintain NimoteHost

Use the intended OS account on the local network. NimoteCode continues to use its existing authenticated SSH connection and the Host binary's absolute path. The Host HTTP listener stays on loopback.

## Install from a website download

Download a matching CLI or desktop archive from [GitHub Releases](https://github.com/mobiledevloperlab/nimotehost/releases). Choose Windows ZIP or macOS/Linux tar.gz. The published platform suffixes are `amd64` for x64 and `arm64` for ARM64. Linux and Windows also publish a legacy `386` CLI archive.

Before extracting or executing anything, compare the file's SHA-256 with the `digest` field of the exact asset in the [official repository release metadata](https://api.github.com/repos/mobiledevloperlab/nimotehost/releases/latest). For a pinned release, use the same API's `/releases/tags/vMAJOR.MINOR.PATCH` path. On Windows use `Get-FileHash -Algorithm SHA256`. On Linux use `sha256sum`, and on macOS use `shasum -a 256`. Extract the complete archive. Run `./nimotehost setup` on Unix or `.\nimotehost.exe setup` on Windows. For later maintenance retain a verified standalone deployment executable outside the installed directory. The bundle CLI in newly published releases also supports deployment commands when run from an extracted directory.

## Install over SSH

After user approval and SSH host-key verification, determine the target platform through the trusted SSH connection. Linux and macOS provide `uname -s` and `uname -m`. In a translated macOS process, `sysctl -n hw.optional.arm64` identifies Apple Silicon. On Windows, native PowerShell can report `[System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture` when supported. Map x86_64 or X64 to amd64, aarch64 or ARM64 to arm64, and i386 or X86 to 386. Stop if the platform is unknown.

1. Resolve a stable tag using the official GitHub Release API. Download the exact `nimotehost-bootstrap-VERSION-OS-ARCH.bin` on Unix or `nimotehost-bootstrap-VERSION-windows-ARCH.exe` on Windows. VERSION omits the initial v. Require a published release, exact asset name and valid SHA-256 metadata. Verify the bytes before executing them. Bootstrap assets first appear with a release containing this change.
2. Transfer the verified executable to a private temporary directory on the target. On Unix give it owner execute permission. Invoke it by absolute path with `deploy install --version vMAJOR.MINOR.PATCH --require-protocol 1` on the same SSH account. Omit `--version` to use GitHub's latest published stable tag. Use `--desktop` to install the GUI archive. An existing desktop edition is kept on update.
3. Parse deployment JSON and verify `deploy health`. Use the returned `binary` path for the existing SSH RPC transport. Negotiate with `capabilities` as described in the [client contract](client-api.md).

The installer downloads over HTTPS from the fixed GitHub repository. It checks metadata, exact asset size, SHA-256, the selected platform and release version, and every staged bundle file. The official repository and its HTTPS API are the default trust anchor. GitHub release publishers can replace an asset and its digest. An independently pinned archive digest can be supplied with `--sha256 HEX`. The existing sidecar files are available for offline integrity checks. OS signing and macOS notarization are outside this verification mechanism.

Download retries use fresh temporary files for transient errors and truncation. Unknown or missing assets and invalid digests fail closed. GitHub can publish the CLI before the desktop asset becomes available. GitHub API access is unauthenticated and subject to rate limits.

## Management commands

Run these on the target. Use an executable outside the fixed installation directory for install, upgrade and uninstall.

| Command | Meaning |
| --- | --- |
| `nimotehost --version` | Existing executable version command |
| `nimotehost deploy status` | Inspect the fixed installation and report authenticated runtime state. A stopped Host is a valid result |
| `nimotehost deploy health` | Require a successful authenticated loopback status request |
| `nimotehost deploy start` | Start a registered managed service and wait for health |
| `nimotehost deploy stop` | Request idle-only shutdown and leave registration available for the next start or login |
| `nimotehost deploy upgrade` | Same installation transaction as deploy install. Stable downgrades are refused |
| `nimotehost deploy uninstall` | Stop only when idle, remove the service and the fixed bundle, retain state |
| `nimotehost deploy uninstall --purge-data` | Also remove the current user Host state, including credentials, records and logs. Obtain explicit confirmation in a calling UI |

Bundle updates use the existing idle-shutdown barrier. Hosts without that capability require a planned manual stop once tasks finish. A bundle whose manifest matches the installed version uses the current files. On failed startup after activation, setup attempts to restore the previous bundle and service. Install and upgrade serialize with other fixed-bundle deployment mutations. An interrupted download leaves the installation untouched. If the process is forcibly killed during activation, inspect the sibling `.nimotehost-previous-*` backup and `nimotehost.setup-lock` or `NimoteHost.setup-lock` before intervention. Stop any setup still running and preserve the backup before removing a stale lock or restoring files. Failed and previous bundle directories are retained for recovery. A bundle that cannot be verified requires repair or manual cleanup. Uninstall leaves separate recovery backups and unrelated shell startup files in place. The previous bundle's program files are distinct from the user state.

| Platform | Native service lifetime |
| --- | --- |
| Linux | systemd user unit starts with the user manager. SSH disconnect is independent while the manager stays active. A system policy that ends the manager after the last logout can also stop Host. Administrator-managed lingering keeps the manager available after logout and at boot. On hosts without systemd user services the foreground CLI remains available |
| macOS | LaunchAgent runs in the GUI login domain and starts at login after reboot. It can outlive a separate SSH connection while that domain exists. A headless SSH-only account without an active GUI domain is unsupported |
| Windows | Least-privilege interactive scheduled task starts at logon. It can outlive SSH disconnect while that user session exists. A machine with no interactive login cannot start this task through SSH. New task registration allows up to three retries after failure |

## JSON contract

Deployment commands write one JSON object on stdout. Error messages may also appear on stderr. Keep stdout as the deployment audit record. `deploy help` prints human-readable usage. Existing CLI and RPC formats retain their own contracts.

```json
{"schema":1,"timestamp":"2026-11-19T16:00:00Z","action":"status","ok":true,"os":"linux","arch":"arm64","scope":"user","uid":1000,"binary":"/home/example/.local/share/nimotehost/nimotehost","installed":true,"service_registered":true,"running":true,"version":"2.5.0","running_version":"2.5.0","protocol":1,"min_protocol":1}
```

`uid` is Unix-only. `installed` describes a regular executable at the fixed path. `service_registered` describes the local registration marker. The authenticated reply determines `running`. `version` and protocol fields come from installed build metadata when available. Older build manifests may omit the protocol range. The timestamp records operation completion in UTC. Successful or partially completed operations can include `release` and `archive_sha256` identifying the selected artifact. Error responses contain `error.code` and diagnostic `error.message`. Fields unavailable at the time of failure are omitted. Use the structured code as the contract. Probe again after an interrupted SSH exchange or a post-install discovery failure.

| Exit | Error code |
| --- | --- |
| 0 | success |
| 1 | operation_failed |
| 2 | invalid_arguments |
| 3 | unsupported_platform |
| 4 | permission_denied |
| 5 | host_stopped |
| 6 | conflict |
| 7 | release_unavailable |
| 8 | integrity_failure |
| 9 | incompatible_protocol |
| 10 | interrupted |

The runtime protocol is independent of release SemVer. The deployment command checks that the candidate supports both the requested client protocol and the bootstrap's own protocol before touching the existing service. Older v2.4.0 builds are treated as protocol 1. After starting the service, use existing runtime capabilities negotiation. When the authenticated API rejects the caller's protocol, report that explicitly and leave the service running.
