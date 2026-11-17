# Community plugin catalog

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
