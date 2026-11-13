# 第三方声明

[English](THIRD_PARTY_NOTICES.md) | **简体中文**

Nimote Host 自有代码和文档采用 Apache-2.0。第三方组件保留各自版权与许可证；
原文保存在源码的 `third_party/`，构建时复制到二进制分发目录的 `licenses/`。
本仓库未内置第三方组件源码。

| 组件 | 版本 | 许可证 | 上游 | 授权文本 |
| --- | --- | --- | --- | --- |
| creack/pty | v1.1.24 | MIT | https://github.com/creack/pty | [LICENSE](third_party/pty/LICENSE) |
| golang.org/x/sys | v0.47.0 | BSD-3-Clause，另含专利授权 | https://go.googlesource.com/sys | [LICENSE](third_party/x-sys/LICENSE)、[PATENTS](third_party/x-sys/PATENTS) |
| Go 运行时/工具链 | 实际构建版本，最低 1.26.5 | BSD-3-Clause，另含专利授权 | https://go.dev | [LICENSE](third_party/go/LICENSE)、[PATENTS](third_party/go/PATENTS) |

`third_party/dependencies.json` 记录已审阅的依赖版本。依赖图或替换项发生变化而许可记录
未更新时，构建会拒绝继续。二进制包中的 `MODULES.json` 和 `BUILD.json` 标明实际依赖、
工具链和构建参数。这些是构建追溯信息，不代表 SPDX/CycloneDX SBOM 或完整文件级许可审计。

Host 不打包 Agent 可执行程序或系统 SSH，用户自行安装的这些工具仍受各自许可证和
服务条款约束。许可证和上游授权文本以随附英文原文为准，中文说明不替代原文。

## 协议 SDK 依赖

MCP 使用官方 Go SDK；ACP 历史查询使用 Coder 维护的社区 Go SDK。
MCP SDK 的许可证说明了 Apache-2.0 / MIT 的迁移状态；应保留其完整原文。
间接模块（包括构建和测试依赖）记录于下表及 `third_party/dependencies.json`。

| Module | Version | License files |
| --- | --- | --- |
| cloud.google.com/go/compute/metadata | v0.3.0 | [LICENSE](third_party/cloud.google.com-go-compute-metadata/LICENSE) |
| github.com/coder/acp-go-sdk | v0.13.5 | [LICENSE](third_party/coder-acp-go-sdk/LICENSE) |
| github.com/golang-jwt/jwt/v5 | v5.3.1 | [LICENSE](third_party/golang-jwt-jwt-v5/LICENSE) |
| github.com/google/go-cmp | v0.7.0 | [LICENSE](third_party/google-go-cmp/LICENSE) |
| github.com/google/jsonschema-go | v0.4.3 | [LICENSE](third_party/google-jsonschema-go/LICENSE) |
| github.com/modelcontextprotocol/go-sdk | v1.8.0 | [LICENSE](third_party/modelcontextprotocol-go-sdk/LICENSE) |
| github.com/segmentio/asm | v1.1.3 | [LICENSE](third_party/segmentio-asm/LICENSE) |
| github.com/segmentio/encoding | v0.5.4 | [LICENSE](third_party/segmentio-encoding/LICENSE) |
| github.com/yosida95/uritemplate/v3 | v3.0.2 | [LICENSE](third_party/yosida95-uritemplate-v3/LICENSE) |
| golang.org/x/oauth2 | v0.35.0 | [LICENSE](third_party/x-oauth2/LICENSE) |
| golang.org/x/sync | v0.20.0 | [LICENSE](third_party/x-sync/LICENSE), [PATENTS](third_party/x-sync/PATENTS) |
| golang.org/x/time | v0.15.0 | [LICENSE](third_party/x-time/LICENSE), [PATENTS](third_party/x-time/PATENTS) |
| golang.org/x/tools | v0.42.0 | [LICENSE](third_party/x-tools/LICENSE), [PATENTS](third_party/x-tools/PATENTS) |
