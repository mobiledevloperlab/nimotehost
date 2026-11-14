# Security

**English** | [简体中文](SECURITY.zh-CN.md)

Host executes commands as the current OS user. Its local token grants that
user's command execution authority. Keep it private and use authenticated SSH;
never expose the loopback HTTP listener through an unauthenticated proxy.
Do not send tokens, Host state files, private project output or credentials in
public issues.

For vulnerabilities, use GitHub's **Security → Report a vulnerability** in the
Nimote Host repository. The repository owner must enable private vulnerability
reporting when creating the public repository. If the option is unavailable,
contact the maintainer through the GitHub profile and request a private channel
without including exploit details or secrets in the initial message.

During the 1.0.0-dev phase, fixes target the latest development version only;
there is no support or response-time SLA. A public source release is not an
assertion of a completed independent security audit or native device acceptance.

See [the protocol and execution boundaries](docs/completed.md) for authentication,
resource limits, terminal/agent behavior and restart semantics.

## Restricted devices / 受限设备

Registered observer devices use independent credentials through a fixed SSH forced
command. They can read the account's Host sessions and output, but cannot create
processes, send input, approve tasks or shut down Host. Revocation is checked on
subsequent requests, including requests over an existing RPC stream; in-flight
requests and data already disclosed are not rolled back.

Each device needs a unique SSH key with `restrict,command="... device-bridge ID"`.
Never also install this key for unrestricted shell access. Credentials stay on the
Host; registration does not itself install or remove SSH keys. Removing a device's
restricted key is a separate SSH administration operation. Existing unrestricted
SSH keys/passwords remain full OS-account access and cannot be isolated by Host
revocation. Full execution-capable device isolation requires a different security
boundary (separate OS identity/sandbox), not a client-provided device ID.

受限设备拥有独立只读身份；撤销在后续请求生效，不影响其他设备和后台任务。
同账号完整 Shell 权限可读取账号凭证，不在设备隔离保证范围内。不要将只读设备
密钥同时配置为普通 SSH 登录密钥。只读范围包含会话输出，授权前需考虑其中的敏感内容。
