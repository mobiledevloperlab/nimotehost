# GitHub plugin catalog

Both installation paths are supported:

1. **Local**: review a local bundle and use the existing install/update operations.
2. **Catalog**: search metadata records, review a GitHub Release Manifest, confirm,
   download/check the program and install it **disabled**. Enabling is a separate
   permission decision. No plugin-specific Host/client code is needed.

**Developers submit JSON metadata only. Their source code and binary releases stay
in their own GitHub repositories.** The default directory is
[`plugin-catalog/index.json`](../../plugin-catalog/index.json). Its format is public
and can also be hosted in a separate GitHub registry repository. New listing data
requires no Host binary update. The starter index is empty; unpublished plugins
are never presented as available. The default remote source becomes reachable
when the new index file is actually published to the configured GitHub branch.
This source-tree change does not itself push commits or create GitHub releases.

## Developer workflow

1. Develop in your own repository against the public SDK or NDJSON specification.
   Set your publisher/source and version in your Manifest.
2. Build an executable for each platform you support. From the Host checkout,
   package a native build, for example:

```sh
go run ./tools/pluginpack -binary /absolute/my-plugin \
  -manifest /absolute/manifest.template.json -out /absolute/local-bundle \
  -repository your-account/your-plugin -tag v1.0.0 \
  -release-out /absolute/release-assets
```

3. Publish the generated executable and platform `.manifest.json` in **your own**
   repository's `v1.0.0` GitHub Release. The tool also emits `catalog-entry.json`.
4. Submit just that JSON record to the public index. For multiple platforms,
   combine `assets` into one record with the same ID/version/repository/tag. Do not
   submit your plugin source or executable to the Host/catalog repository.
5. Users can search the index, inspect the exact Manifest and install your release.
   Updates change the metadata record to a new reviewed release; installed plugins
   must be disabled before an explicit update. There is no automatic update.

The generated local bundle continues to work offline. The two release assets are
separate files, avoiding archive extraction and install scripts. Asset naming is
chosen by the publisher; the packer uses `<id>-<os>-<arch>[.exe]` and
`<id>-<os>-<arch>.manifest.json`. Packaging is native by default: use native runners
for each platform, as the packer records its own OS/architecture.

An index record looks like this (replace all example values/digests):

```json
{
  "schema_version": "1.0",
  "plugins": [{
    "id": "my-plugin",
    "name": "My plugin",
    "description": "Capabilities provided by my independent plugin",
    "repository": "your-account/your-plugin",
    "version": "1.0.0",
    "tag": "v1.0.0",
    "assets": {
      "darwin/arm64": {
        "manifest_asset": "my-plugin-darwin-arm64.manifest.json",
        "manifest_sha256": "64_LOWERCASE_HEX_DIGITS",
        "binary_asset": "my-plugin-darwin-arm64"
      }
    }
  }]
}
```

Validate against [catalog.schema.json](spec/v1/catalog.schema.json). One latest
listing per plugin ID per source; repository names are `owner/repo`, tags and asset
names are safe single path components. No arbitrary download URL, command, archive
path or executable hook can be specified in the index. The manifest includes the
executable SHA256; the index pins the exact Manifest SHA256. These checks detect
changed bytes relative to the reviewed metadata, not a malicious publisher.

GitHub's public release asset URL format is documented by the
[GitHub release assets API](https://docs.github.com/en/rest/releases/assets).
The Host constructs those URLs from validated repository/tag/asset components;
private repositories and authenticated downloads are intentionally unsupported.

## User interface and sources

GUI → **Plugins** exposes **Install locally** and **Plugin catalog**. The catalog
supports keyword search, platform availability, review/download/install, explicit
updates, download progress/cancel and reconnecting to recent install jobs.
**Catalog sources** accepts public GitHub raw JSON URLs or GitHub `blob` links
(normalized to raw URLs), with explicit owner confirmation. Sources persist in
private `plugin-catalogs.json`. There are at most eight sources. A failed source is
reported without hiding healthy sources. Search results are capped at 256/1 MiB;
`truncated:true` means narrow the query. No catalog network calls happen on normal
Host startup or capability polling.

A directory listing is not an endorsement/signature. Adding a directory does not
install or enable any plugin. Revoking a source cancels its pending downloads,
except a local install commit already underway. Installed packages remain under
normal plugin disable/uninstall controls.

## HCP operations (additive to 1.0)

All operations use authenticated owner-only `POST /v1/extensions`, with
`protocol:"1.0"`. Discover the feature flag `plugin_catalog_v1` first.

| Operation | Fields | Result |
| --- | --- | --- |
| `plugins.catalog.sources` | none | configured `{id,url}` sources |
| `plugins.catalog.add` | `id`, `source` URL, `confirmed:true` | persist source |
| `plugins.catalog.remove` | source `id`, `confirmed:true` | revoke/remove source |
| `plugins.catalog.search` | optional `query` | `{plugins,failures,truncated?}` |
| `plugins.catalog.get` | plugin `id`, source ID in `source` | `{entry,manifest,sha256}` for current platform |
| `plugins.install_remote` | `id`, source ID, reviewed `sha256`, `confirmed:true` | accepted install job |
| `plugins.update_remote` | same; installed package must be disabled | accepted update job |
| `plugins.install.status` | job `id` | state/progress/bytes/error |
| `plugins.install.jobs` | none | retained jobs, newest first |
| `plugins.install.cancel` | job `id` | cancel before local commit |

Job states: downloading → installing → succeeded/failed/canceled. Maximum two
active downloads, sixteen retained jobs, two-minute download deadline. Job state
survives GUI/SSH disconnects within this daemon instance, not Host restart. Results
are inspected after uncertain responses; no automatic retry/install replay occurs.
The local install commit rejects cancellation once started. Reopening the GUI can
inspect the recent jobs rather than submitting another install request.

Safety: HTTPS-only GitHub hosts, bounded redirect allowlist, no proxy/cookie jar/
credential environment, public-address DNS checks and numeric IP pinning, 512 KiB
index / 60 KiB Manifest / 64 MiB executable limits. Hash/identity/version/platform
checks precede publication. Temporary downloads are private and cleaned on normal
failure/cancel/shutdown. Abrupt Host SIGKILL can leave an ignored `.download-*`
directory for manual cleanup. This does not add an OS sandbox or a signing authority.
