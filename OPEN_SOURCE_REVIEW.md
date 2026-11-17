# Open-source release review / 开源发布检查记录

**简体中文** | English below

检查日期：2026-11-17。本地分支迁移为 `master`，插件目录与示例链接同步更新；远程默认分支更新仍需托管服务认证。范围为当前独立 NimoteHost 工作树及本地 Git 引用可达历史。

## 已检查与修复

- 根目录 Apache-2.0、NOTICE、贡献许可说明和中英文第三方声明存在；CLI 构建将许可、NOTICE 和依赖清单随包分发。
- 对 `third_party/dependencies.json` 中的依赖版本与本地 Go 模块原文逐项比较：上游根目录 LICENSE/COPYING/NOTICE/PATENTS 均有对应原文，内容一致。coder/websocket 的上游 `LICENSE.txt` 在此保存为 `LICENSE`，内容相同。
- 桌面构建记录独立模块清单，并复制每个解析到的模块根目录许可文件；没有根目录许可时拒绝构建。根目录许可检查不等于全部嵌套源码、字体或系统动态库的逐文件审计。
- 修复源码导出和独立构建检查遗漏 `pkg/`、`examples/`、`plugin-catalog/` 的问题；导出回归测试覆盖这三个目录。
- 忽略本地 `.nimote/` 任务状态；源码导出排除 Git 元数据、本地状态和构建目录，拒绝符号链接及常见凭据文件。
- 对所有本地 Git 引用可达的 839 个历史文件版本扫描私钥、AWS access key、GitHub token、常见 OpenAI/Anthropic key 格式，未发现匹配。此扫描不能识别所有凭据形式，也不覆盖远端独有引用。
- GitHub CI 已覆盖 macOS/Linux/Windows 的 x64、ARM64 CLI 和原生桌面包；Linux/Windows 另有 x86 CLI。构建上传缺少产物时失败，导出源码在 CI 中独立测试与构建后才上传。

代码与工作流说明使用英文；文档与生成的说明页面保留中英文，翻译资源及 Unicode 回归数据保留国际化用途。

## 历史隐私处理与发布前确认

已按发布要求从所有本地引用可达历史中移除旧 `docs/audits/`：共 9 个历史文件，
涉及 3 次提交，包括带本机路径的原始验收日志与设备验收记录。当前维护的功能、
协议与安全边界文档保留。旧对象已从仓库清理，恢复备份仅存于仓库外私有目录。


- 已按所有者要求重写全部 34 次提交的作者与提交者身份，统一为 `Nimote contributors <contributors@nimotehost.invalid>`；提交树、说明与时间均保持一致。仓库内旧引用、reflog 和不可达对象已清理，旧历史仅备份在仓库外私有目录。本地 Git 身份也已更新。远端已有历史需另行替换，发布时不要混入旧克隆。
- 品牌资源说明称图标由生成素材制成，但仓库未包含创作过程或权利来源证明。所有者应确认 `docs/nimotehost*.png`、GUI 图标及派生资源可公开分发；本次检查不能凭文件内容认定权属。
- 当前仓库无法证明全部自有代码和文档的权属。贡献者需具有授予 Apache-2.0 的权限；涉及其他 Nimote 项目的接口说明不授予其他仓库的源码权利。
- GitHub 仓库应启用 Actions、Issues 和 private vulnerability reporting；校验公开仓库地址、CODEOWNERS 对应账号与实际维护者一致。
- 六种原生桌面目标需由 GitHub Actions 实际运行确认；本机验证不能证明 Windows/Linux 图形界面或 ARM runner 已验收。macOS 包尚未签名、公证。

GitHub runner 标签依据：[GitHub-hosted runners](https://docs.github.com/en/actions/reference/runners/github-hosted-runners)。
Apache-2.0 再分发条款原文：[Apache License 2.0](https://www.apache.org/licenses/LICENSE-2.0.html)。

## English

Review date: 2026-11-17. The local branch is migrated to `master`; plugin catalog and example URLs are updated accordingly. Updating the remote default branch still requires hosting-service authentication. Scope: the standalone NimoteHost working tree and history
reachable through local Git refs.

### Reviewed and fixed

- The Apache-2.0 license, NOTICE, contribution licensing terms and bilingual third-party notices are present. CLI distributions include licenses, NOTICE and dependency manifests.
- Compared versions in `third_party/dependencies.json` and preserved license text against local Go modules. Upstream root LICENSE/COPYING/NOTICE/PATENTS texts are accounted for and identical. coder/websocket's upstream `LICENSE.txt` is preserved as `LICENSE` with identical contents.
- Desktop builds record a separate resolved module inventory and copy module root license files, failing when no root license exists. This does not constitute a file-level audit of nested code, fonts or system dynamic libraries.
- Fixed source export and clean build checks omitting `pkg/`, `examples/` and `plugin-catalog/`. Export regression tests cover all three directories.
- Added `.nimote/` local task state to ignore rules. Source export excludes Git metadata, local state and build outputs, and rejects symlinks and common credential files.
- Scanned 839 historical file versions reachable through local refs for private keys, AWS access keys, GitHub tokens and common OpenAI/Anthropic key formats; no matches were found. This cannot identify every credential format or cover remote-only refs.
- GitHub CI covers macOS/Linux/Windows x64 and ARM64 CLI and native desktop packages, plus Linux/Windows x86 CLI. Missing upload artifacts fail the build. Exported source is independently tested and built in CI before upload.

Source code and workflow descriptions use English. Documentation and generated explanatory pages retain English and Chinese; translation resources and Unicode regression data retain their internationalization purpose.

### History privacy and owner confirmation before publishing

Removed the obsolete `docs/audits/` tree from all history reachable through local refs:
9 historical files across 3 commits, including raw local-machine logs and device
acceptance records. Maintained feature, protocol and security documentation remains.
Old objects were pruned; recovery backups exist only in private directories outside
the repository.


- At the owner’s request, author and committer identities in all 34 commits were rewritten to `Nimote contributors <contributors@nimotehost.invalid>`. Commit trees, messages and timestamps are unchanged. Old local refs, reflogs and unreachable objects were removed; recovery history is stored only in a private directory outside the repository. Local Git identity was also updated. Existing remote history still requires replacement; do not mix old clones into the public repository.
- The asset README describes generated artwork, but the repository contains no creation records or rights documentation. Confirm redistribution rights for `docs/nimotehost*.png`, GUI icons and derivatives. File contents alone cannot establish ownership.
- The repository cannot establish ownership of every original source file and document. Contributors must have authority to grant Apache-2.0 rights. Interface documentation referencing other Nimote projects does not license their source trees.
- Enable GitHub Actions, Issues and private vulnerability reporting. Verify the public repository address and that CODEOWNERS identifies the actual maintainer.
- All six native desktop targets require actual GitHub Actions runs. Local verification cannot establish Windows/Linux GUI acceptance or ARM runner success. macOS bundles remain unsigned and unnotarized.

References: [GitHub-hosted runners](https://docs.github.com/en/actions/reference/runners/github-hosted-runners),
[Apache License 2.0](https://www.apache.org/licenses/LICENSE-2.0.html).
