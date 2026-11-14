# NimoteHost brand assets / 品牌资源

English | 简体中文

Original supplied artwork / 用户原图: [docs/nimotehost.png](../../../docs/nimotehost.png).
The original is retained unchanged. / 原图保持不变。

- `icon-master.png`: polished bitmap master, 1254 × 1254; white cat and N on charcoal.
- `icon.png`: 1024 × 1024 application and in-app logo with transparent rounded corners; a copy is in `docs/nimotehost-rounded.png`.
- `icon-256.png`, `brand-128.png`, `tray-64.png`: pre-sized runtime images to avoid repeated large-image scaling.
- `icon.icns`: macOS multi-resolution icon, 16–1024 pixels.
- `icon.ico`: Windows multi-resolution icon, 16–256 pixels.
- `mark.png`: transparent black mark for monochrome brand use.

优化重点：保留猫形和 N 形负空间，平滑锯齿、加粗并整理胡须，调整视觉居中和留白。
The accepted masters were edited with the built-in imagegen tool. Standard sizes and
native icon containers were exported with `sips`, `iconutil` and PNG-backed ICO encoding.
Windows Explorer resources are stored in `../logo_windows_{amd64,arm64}.syso`.
Regenerate each with `go run github.com/akavel/rsrc@v0.10.2 -ico assets/icon.ico -arch <arch> -o logo_windows_<arch>.syso` from the GUI module. The COFF objects contain only these icon resources; the resource tool is not a runtime dependency.

## Accepted imagegen prompts / 最终提示词

### Application master

Use case: precise-object-edit / logo-brand. Edit target: attached Nimote Host logo.
Produce ONE polished square 1024x1024 application-icon master, preserving this exact
visual identity: white seated elegant cat facing left, pointed ears, dark mask-shaped
face, slim white eyes, curved tail and large negative-space N monogram inside the
body. Retain the recognizable original silhouette and pose; do not invent a different
cat or change the N. Clean up the visibly jagged edges into smooth crisp antialiased
vector-like curves. Simplify the very thin irregular whiskers into three clean slightly
thicker tapered strokes so they survive at small sizes, soften accidental kinks and
tiny noisy features. Optically center the whole silhouette including whiskers; use
balanced generous approximately 12% safe margins on all sides, symbol height about
76% of canvas. Use pure white symbol on uniform opaque charcoal #17191C square
background. Keep all negative-space holes charcoal. Flat monochrome only, no
gradients, no shadows, no bevels, no text, no border, no rounded corners baked into
the image, no extra symbols. The output is the final usable logo asset itself, not
a presentation or mockup.

### Transparent monochrome mark

Redraw this exact cat-with-N logo as a CLEAN SOLID BLACK silhouette on actual
transparent background. This is an edited vector-style brand mark for a macOS
template menu-bar icon, not a photograph and not a texture. Preserve the original
seated cat pose facing left, pointed ears with empty triangular interior, two eyes
as empty holes inside its mask face, three whiskers extending left, curled tail,
and N-shaped negative-space body geometry. All originally WHITE artwork becomes
SOLID BLACK filled artwork. All originally dark background, dark face mask, and
internal dark holes become perfectly transparent alpha. Use smooth clean
antialiased boundaries; slightly thicken the whiskers and remove jaggies. Center
whole mark on square canvas with 12% padding. The mark must be clean solid flat
black with NO white, NO gray, NO texture, NO speckles, NO distressed surface,
NO shadow, NO background. Output one transparent PNG.
