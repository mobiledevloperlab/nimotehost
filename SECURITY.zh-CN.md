# 安全政策

[English](SECURITY.md) | **简体中文**

Host 以当前系统用户权限执行命令，本地 token 等价于该用户的命令执行权限。请妥善保管，
通过已认证 SSH 使用，不要通过未认证代理暴露回环 HTTP 接口。不要在公开 issue 中发送
token、Host 状态文件、私有项目输出或其他凭据。

报告漏洞时，使用 Nimote Host GitHub 仓库的 **Security → Report a vulnerability**。
仓库所有者在创建公开仓库时应开启私密漏洞报告。如果该入口不可用，请通过维护者的
GitHub 个人资料请求私密沟通渠道，初次联系不要包含利用细节或秘密。

在 `1.0.0-dev` 阶段，仅针对最新开发版本提供修复，不承诺支持或响应时间 SLA。
公开源码不代表已完成独立安全审计或真实设备验收。

认证、资源配额、终端/Agent 行为及重启语义见[协议与执行边界](docs/completed.md)。

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
