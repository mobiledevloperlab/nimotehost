# Open-source release review

Review date: 2026-11-17. The local branch is migrated to `master`; plugin catalog and example URLs are updated accordingly. Updating the remote default branch still requires hosting-service authentication. Scope: the standalone NimoteHost working tree and history
reachable through local Git refs.

## Reviewed and fixed

- The Apache-2.0 license, NOTICE, contribution licensing terms and bilingual third-party notices are present. CLI distributions include licenses, NOTICE and dependency manifests.
- Compared versions in `third_party/dependencies.json` and preserved license text against local Go modules. Upstream root LICENSE/COPYING/NOTICE/PATENTS texts are accounted for and identical. coder/websocket's upstream `LICENSE.txt` is preserved as `LICENSE` with identical contents.
- Desktop builds record a separate resolved module inventory and copy module root license files, failing when no root license exists. This does not constitute a file-level audit of nested code, fonts or system dynamic libraries.
- Fixed source export and clean build checks omitting `pkg/`, `examples/` and `plugin-catalog/`. Export regression tests cover all three directories.
- Added `.nimote/` local task state to ignore rules. Source export excludes Git metadata, local state and build outputs, and rejects symlinks and common credential files.
- Scanned 839 historical file versions reachable through local refs for private keys, AWS access keys, GitHub tokens and common OpenAI/Anthropic key formats; no matches were found. This cannot identify every credential format or cover remote-only refs.
- GitHub CI covers macOS/Linux/Windows x64 and ARM64 CLI and native desktop packages, plus Linux/Windows x86 CLI. Missing upload artifacts fail the build. Exported source is independently tested and built in CI before upload.

Source code and workflow descriptions use English. Documentation and generated explanatory pages retain English and Chinese; translation resources and Unicode regression data retain their internationalization purpose.

## History privacy and owner confirmation before publishing

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
