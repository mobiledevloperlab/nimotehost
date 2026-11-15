# Security boundary — English / 简体中文

**Independent processes are not a security sandbox.** Third-party binaries are
untrusted. This release's portable execution boundary is the Host OS account,
explicitly disclosed through mandatory `process.same-user` grants. An enabled
binary can read files and credentials accessible to that account, change its
own files, open sockets, spawn programs, or tamper with Host state. Removing
credential environment variables does not prevent reading same-account files.
Do not run unreviewed plugins in a privileged/credential-bearing Host account.
Use a separately confined OS account/container/VM for hostile code. This release
does not provision that confinement or reuse isolated Session UIDs for plugin daemons.

Host-side safeguards:

- Existing authenticated SSH/loopback access and authorization precede dispatch.
  All plugin operations require owner access; existing device roles cannot gain
  execution privileges through the new endpoint or MCP.
- Install/update/uninstall and enable require explicit owner confirmation;
  sensitive invocations require confirmation. Generic clients must obtain an
  actual user decision before setting that field. The protocol cannot distinguish
  a human click from a malicious client already holding owner authority.
- Packages default disabled. Enable grants must exactly match declared known
  permissions; disable clears grants, cancels work and stops managed ACP sessions.
  Already-sent network requests or completed effects cannot be recalled.
- Source and binary are regular bounded files; namespaces and fixed entrypoints
  prevent traversal. No archive extraction, shell command concatenation, install
  hooks or untrusted argv are used. Online catalog installs use the same installer
  after bounded GitHub-only downloads and digest/identity/platform validation. SHA256 is checked at install, enable, restart
  and ACP launch. A digest establishes integrity relative to the reviewed package,
  not publisher authenticity. Same-account filesystem mutation/TOCTOU is outside
  the available boundary. Updates clear grants and require explicit re-enablement.
- Environment inherits only PATH and Windows system paths, plus plugin-local
  home/temp values. Raw plugin stderr is discarded; SDK error details are hidden.
  Audit logs contain bounded IDs/state metadata, never inputs, output or headers.
  Returned results are owner data and can themselves contain secrets; no generic
  redactor can guarantee otherwise. Escaping/control-character-safe client display
  is required for all plugin content.
- Existing Unix guardian and Windows Job ownership provide cleanup/cancellation.
  Configured CPU/memory/process quotas pass through the existing backend. Default
  portable limits bound frames, queues, runtime and retained data, not malicious
  child CPU/RSS/disk. macOS has no native hard quotas here; Linux needs delegated
  cgroups; Windows uses existing Job limits. Unix descendants that deliberately
  escape process groups remain outside cleanup without cgroups. These limitations
  already apply to ordinary same-account tasks and are not concealed by the SDK.

HTTP Inspector applies a separate, tested application policy: GET/POST/PUT/DELETE,
HTTP/HTTPS standard ports, 64 KiB request, 128 KiB response, 16 KiB response headers,
20-second maximum request timeout, no proxy, no redirect, no cookie jar and no
compression. Numeric/local/mapped/transition/reserved address classes are denied;
all DNS answers must pass, and dialing uses the checked numeric address to prevent
rebinding. TLS still validates the original hostname. Sensitive/transport request
headers are rejected; response headers use a small positive allowlist. Request
errors do not echo URLs or headers. Body/query data is intentionally user-supplied
and response bodies are intentionally visible; the plugin cannot infer all secrets
in arbitrary content. Public servers can have side effects; invocation confirmation
is required even for GET. Network routing/NAT changes and administrator/kernel
compromise require external network isolation, not an application URL checker.

简体中文：默认禁用、明确授权、完整性校验、进程监管和应用层 SSRF 防护均已实现；
权限声明不是 OS 沙箱。恶意同账户插件仍可读取账户文件，必须在外部隔离环境中运行。
撤销阻止后续调用并发出终止请求，无法撤回已经完成或已发送的副作用。


## Online catalog boundary

The [catalog workflow](catalog.md) adds explicit source trust and Manifest review,
not implicit trust in a GitHub listing. Search/download run only on user request;
source metadata never executes. No GitHub token, owner token, proxy environment,
cookies or arbitrary authentication headers are sent to GitHub/CDN hosts. HTTPS
and public DNS/IP pinning apply across a small redirect allowlist. Rate limits,
missing releases and rejected redirects return sanitized failures. Only a validated,
hash-matched executable is published, and remains disabled. No archive extraction
or install script runs. Source removal cancels pre-commit downloads, while already
committed installations and external effects are not rolled back. Sources/indexes
can be malicious; checksums establish byte integrity rather than publisher safety.
