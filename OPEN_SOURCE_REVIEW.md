# Open-source release review

Review date: 2026-11-17. The local branch is migrated to `master`; plugin catalog and example URLs are updated accordingly. Updating the remote default branch still requires hosting-service authentication. Scope: the standalone NimoteHost working tree and history
reachable through local Git refs.

## Reviewed and fixed

- The Apache-2.0 license, NOTICE, contribution licensing terms and bilingual third-party notices are present. CLI distributions include licenses, NOTICE and dependency manifests.
- Compared versions in `third_party/dependencies.json` and preserved license text against local Go modules. Upstream root LICENSE/COPYING/NOTICE/PATENTS texts are accounted for and identical. coder/websocket's upstream `LICENSE.txt` is preserved as `LICENSE` with identical contents.
- Desktop builds record a separate resolved module inventory and copy module root license files, failing when no root license exists. This does not constitute a file-level audit of nested code, fonts or system dynamic libraries.
- Fixed source export and clean build checks omitting `pkg/`, `examples/` and `plugin-catalog/`. Export regression tests cover all three directories.
- Added `.nimote/` local task state to ignore rules. Source export excludes Git metadata, local state and build outputs, and rejects symlinks and common credential files.
- Rescanned 936 historical blobs (including reflog-reachable objects) and current tracked files reachable through local refs for private keys, AWS access keys, GitHub tokens and common OpenAI/Anthropic key formats; no matches were found. The source exporter now rejects these provider token patterns and encrypted private-key headers too. This cannot identify every credential format or cover remote-only refs.
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
- Ordinary PRs and pushes to `master`/`main` now build all eight CLI and six native desktop targets, validate standalone exports and run clean-source checks at minimum/stable Go versions. Manual edition selection and tag-only release publishing remain available. Jobs have bounded timeouts, short artifact retention and credential-free checkouts. Normal validation uses the release toolchain; the minimum-version job remains explicit. Runner labels were checked against the [GitHub runner reference](https://docs.github.com/en/actions/reference/runners/github-hosted-runners).
- All six native desktop targets require actual GitHub Actions runs. Local verification cannot establish Windows/Linux GUI acceptance or ARM runner success. macOS bundles remain unsigned and unnotarized.

References: [GitHub-hosted runners](https://docs.github.com/en/actions/reference/runners/github-hosted-runners),
[Apache License 2.0](https://www.apache.org/licenses/LICENSE-2.0.html).

## Current privacy follow-up (2026-11-17)

- The ignored local `env.md` still contains private environment/credential material. Do not copy the whole working directory into a public archive. Validate and rotate any real credentials through their providers; no remote login or credential validity check was performed.
- Some newer commits use the public GitHub account name and GitHub noreply email, in addition to the rewritten contributor identity described above. The earlier 34-commit rewrite does not imply that all subsequent commits are anonymous. These identities will be public; no further history rewrite was performed.
- `docs/host-hardening-tests.md` and related acceptance records disclose test OS/build versions, tooling, model configuration names and environment capabilities. No credential pattern was found in them, but owners should decide whether these operational details are appropriate for publication.
- The private follow-up file `OPEN_SOURCE_PENDING.zh-CN.md` is now explicitly ignored. Local Git remotes contain a private hosting address; Git configuration is excluded from source exports and is not a tracked file.
- Asset/source ownership confirmation and real native installation/GUI acceptance remain open. Build checks do not establish redistribution rights, interactive usability or code-signing/notarization.

## Validation of this CI update

On macOS arm64 with Go 1.27.4: root and GUI race tests, both modules' vet and module verification, all 19 Python tests (including expanded credential-rejection subcases), source export and actionlint passed. Built all eight CLI distributions and both macOS GUI distributions; verified archive checksums, license presence and exclusion of private filenames for all ten packages. The Intel macOS GUI was cross-built, not executed. Native Windows/Linux builds, a fresh minimum/stable clean-source CI run and interactive GUI acceptance were not executed in this local check. No changes were pushed and no remote release was published.
