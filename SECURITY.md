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

See [the protocol and execution boundaries](docs/host.en.md) for authentication,
resource limits, terminal/agent behavior and restart semantics.
