# Community plugin catalog / 社区插件目录

**Submit metadata only. Do not submit plugin source code or binaries here.**

Develop and release your plugin in your own GitHub repository. Upload a built
executable and its Manifest for each supported platform to a GitHub Release.
Then add one record to `index.json` through a pull request. A listing is not an
endorsement or a signature; consumers review permissions and hashes before install.

Use `tools/pluginpack -repository OWNER/REPO -tag v1.0.0 -release-out ...` to
produce the release assets and `catalog-entry.json`. Upload the executable and
platform Manifest to your own repository's Release. Merge the generated record's
platforms into one catalog entry. Submit that JSON record, not implementation code.

The starter catalog is intentionally empty: no unpublished release or nonexistent
plugin is advertised. After this file is published to the configured branch,
Host's default `community` source can search it without a new binary release.
A separate GitHub repository can host the same index format; configure its raw
JSON URL in GUI → Plugins → Plugin catalog → Catalog sources.

See [public catalog specification](../docs/plugin-system/catalog.md) and
[catalog schema](../docs/plugin-system/spec/v1/catalog.schema.json).

简体中文：开发者代码和发布包全部放在自己的 GitHub 仓库，只向此目录提交一条 JSON
记录，包含仓库、版本、平台、Release 资产名称和 Manifest SHA256。无需提交插件源码，
无需修改 Host 或客户端代码。当前目录为空，不虚构尚未发布的社区插件。
