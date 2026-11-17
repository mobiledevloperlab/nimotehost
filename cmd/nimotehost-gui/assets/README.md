# NimoteHost brand assets

The source artwork is `assets/nimotehost.png`. The master and app icons use the newly generated cat-and-N mark on a dark background.

- `icon-master.png`: full-resolution master.
- `icon.png`: 1024 × 1024 app icon; copied to `assets/nimotehost-rounded.png`.
- `icon-256.png`, `brand-128.png`, `tray-64.png`: runtime sizes.
- `icon.icns`, `icon.ico`: macOS and Windows icon containers.
- `../logo_windows_{amd64,arm64}.syso`: Windows executable icon resources.

To regenerate Windows resources, run from the GUI directory:

```sh
go run github.com/akavel/rsrc@v0.10.2 -ico assets/icon.ico -arch amd64 -o logo_windows_amd64.syso
go run github.com/akavel/rsrc@v0.10.2 -ico assets/icon.ico -arch arm64 -o logo_windows_arm64.syso
```
