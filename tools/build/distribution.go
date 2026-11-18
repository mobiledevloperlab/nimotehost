// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors
package main

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Repository documentation stays in source exports. Binary bundles have their
// own short instructions, with only bundled files or public URLs as links.
func writeDistributionReadmes(dir, targetOS, arch, version string, gui bool) error {
	install, start := "sh scripts/install.sh", "./nimotehost start"
	if targetOS == "windows" {
		install, start = `.\nimotehost.exe setup`, `.\nimotehost.exe start`
	}
	edition, editionZH := "CLI", "CLI"
	guiEN, guiZH := "", ""
	if gui {
		edition, editionZH = "Desktop (includes CLI)", "桌面版（包含 CLI）"
		launch := "./nimotehost-gui"
		if targetOS == "windows" {
			launch = `.\nimotehost-gui.exe`
		}
		if targetOS == "darwin" {
			launch = "open ./NimoteHost.app"
		}
		guiEN = fmt.Sprintf("\n## Open the desktop manager\n\nFrom the extracted bundle:\n\n```sh\n%s\n```\n\nClosing the manager keeps Host running. Stop or restart explicitly ends active tasks.\n", launch)
		guiZH = fmt.Sprintf("\n## 打开桌面管理器\n\n在解压目录执行：\n\n```sh\n%s\n```\n\n关闭管理器不会停止 Host；停止或重启 Host 会结束活动任务。\n", launch)
	}
	platformEN, platformZH := "", ""
	if targetOS == "darwin" {
		platformEN = "\nmacOS packages are unsigned and not notarized.\n"
		platformZH = "\nmacOS 包尚未签名或公证。\n"
	}
	if gui && targetOS == "linux" {
		platformEN = "\nLinux desktop requires glibc 2.35+, OpenGL and X11 (or XWayland in a Wayland session).\n"
		platformZH = "\nLinux 桌面版需要 glibc 2.35+、OpenGL 和 X11；Wayland 会话需提供 XWayland。\n"
	}
	en := fmt.Sprintf(`# NimoteHost %s

**English** | [简体中文](README.zh-CN.md)

%s · %s/%s

The host-side companion to [NimoteCode](https://github.com/mobiledevloperlab/nimotecode).
It also works with scripts and third-party clients. This is a development preview.

## Install

Extract the complete archive and run from this directory as your normal user, without sudo:

`+"```sh\n%s\n```"+`

No Go installation is needed. Setup verifies SHA256SUMS, copies the bundle to a
stable per-user directory and starts the background service. Upgrades preserve
state and credentials and refuse to interrupt active tasks. Keep all bundle files
together; do not remove license files or edit files covered by SHA256SUMS.

For foreground CLI use instead:

`+"```sh\n%s\n```"+`
%s%s
## Connect and manage

Connect NimoteCode over your existing SSH connection as the same OS user.
SSH and Agent CLIs require separate installation and authentication. Host listens
only on loopback. Client disconnects do not stop tasks; Host/OS restarts do not
restore their processes. Use nimotehost status, stop or restart to manage Host.

## Documentation and notices

- [Full documentation](https://github.com/mobiledevloperlab/nimotehost/blob/master/README.md)
- [Client integration](https://github.com/mobiledevloperlab/nimotehost/blob/master/docs/client-api.md)
- [Security and credential rotation](https://github.com/mobiledevloperlab/nimotehost/blob/master/SECURITY.md)
- [License](LICENSE) · [Notice](NOTICE) · [Third-party notices](THIRD_PARTY_NOTICES.md)

BUILD.json and MODULES.json describe the CLI build. Desktop bundles also carry
GUI-MODULES.json and licenses-gui/. SHA256SUMS covers the bundle; the adjacent
archive .sha256 covers the download. Checksums do not authenticate the publisher.
`, version, edition, targetOS, arch, install, start, guiEN, platformEN)
	zh := fmt.Sprintf(`# NimoteHost %s

[English](README.md) | **简体中文**

%s · %s/%s

[NimoteCode](https://github.com/mobiledevloperlab/nimotecode) 的主机端配套服务，
也可通过脚本或第三方客户端独立使用。当前为开发预览版。

## 安装

解压完整归档后，在该目录以普通用户执行，无需 sudo：

`+"```sh\n%s\n```"+`

无需安装 Go。安装器验证 SHA256SUMS，将完整包复制到固定用户目录并启动后台服务。
升级保留数据和凭据，有活动任务时拒绝升级。请保持分发文件完整，保留许可证文件，
不要修改 SHA256SUMS 覆盖的内容。

如需直接以前台 CLI 运行：

`+"```sh\n%s\n```"+`
%s%s
## 连接与管理

在 NimoteCode 中使用同一系统账号的既有 SSH 连接。SSH 和 Agent CLI 需单独安装、认证。
Host 只监听本机回环地址。客户端断开不会停止任务；Host 或系统重启不会恢复任务进程。
可使用 nimotehost status、stop 或 restart 管理 Host。

## 文档与声明

- [完整文档](https://github.com/mobiledevloperlab/nimotehost/blob/master/README.zh-CN.md)
- [客户端接入](https://github.com/mobiledevloperlab/nimotehost/blob/master/docs/client-api.md)
- [安全与凭据轮换](https://github.com/mobiledevloperlab/nimotehost/blob/master/SECURITY.zh-CN.md)
- [许可证](LICENSE) · [NOTICE](NOTICE) · [第三方声明](THIRD_PARTY_NOTICES.zh-CN.md)

BUILD.json 和 MODULES.json 记录 CLI 构建信息；桌面包另附 GUI-MODULES.json 和 licenses-gui/。
SHA256SUMS 覆盖包内文件，相邻 .sha256 校验下载归档；校验和不能验证发布者身份。
`, version, editionZH, targetOS, arch, install, start, guiZH, platformZH)
	for name, text := range map[string]string{"README.md": en, "README.zh-CN.md": zh} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0644); err != nil {
			return err
		}
	}
	return nil
}

func distributionInstaller(targetOS string) string {
	if targetOS == "windows" {
		return "install.ps1"
	}
	return "install.sh"
}

func copyDesktopIcon(output, targetOS string) error {
	if targetOS != "linux" {
		return nil
	}
	return copyFile(filepath.Join("cmd", "nimotehost-gui", "assets", "icon.png"), filepath.Join(output, "nimotehost.png"))
}

// Validate the actual bundle before checksumming and archiving, not merely the
// source checkout. License trees retain upstream texts and directory layouts.
func validateDistribution(dir, targetOS string, gui bool) error {
	binary := "nimotehost"
	if targetOS == "windows" {
		binary += ".exe"
	}
	required := []string{binary, "LICENSE", "NOTICE", "BUILD.json", "MODULES.json", "README.md", "README.zh-CN.md", "THIRD_PARTY_NOTICES.md", "THIRD_PARTY_NOTICES.zh-CN.md", "licenses", "scripts/" + distributionInstaller(targetOS)}
	roots := map[string]bool{"scripts": true, "licenses": true, "SHA256SUMS": true}
	for _, name := range required {
		roots[strings.Split(name, "/")[0]] = true
	}
	if gui {
		required = append(required, "GUI-MODULES.json", "licenses-gui")
		roots["GUI-MODULES.json"], roots["licenses-gui"] = true, true
		switch targetOS {
		case "darwin":
			required = append(required, "NimoteHost.app/Contents/MacOS/nimotehost-gui", "NimoteHost.app/Contents/MacOS/nimotehost", "NimoteHost.app/Contents/Info.plist", "NimoteHost.app/Contents/Resources/nimotehost.icns")
			roots["NimoteHost.app"] = true
		case "windows":
			required = append(required, "nimotehost-gui.exe")
			roots["nimotehost-gui.exe"] = true
		case "linux":
			required = append(required, "nimotehost-gui", "nimotehost.png", "nimotehost.desktop")
			roots["nimotehost-gui"], roots["nimotehost.png"], roots["nimotehost.desktop"] = true, true, true
		}
	}
	for _, name := range required {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(name))); err != nil {
			return fmt.Errorf("missing distribution file %s: %w", name, err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !roots[entry.Name()] {
			return fmt.Errorf("unexpected distribution entry: %s", entry.Name())
		}
	}
	scripts, err := os.ReadDir(filepath.Join(dir, "scripts"))
	if err != nil {
		return err
	}
	if len(scripts) != 1 || scripts[0].Name() != distributionInstaller(targetOS) {
		return fmt.Errorf("unexpected installers for %s", targetOS)
	}
	links := regexp.MustCompile(`\]\(([^)]+)\)|(?:src|href)="([^"]+)"`)
	for _, name := range []string{"README.md", "README.zh-CN.md", "THIRD_PARTY_NOTICES.md", "THIRD_PARTY_NOTICES.zh-CN.md"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		for _, match := range links.FindAllStringSubmatch(string(data), -1) {
			target := match[1]
			if target == "" {
				target = match[2]
			}
			target = strings.Trim(target, "<>")
			parsed, err := url.Parse(target)
			if err != nil {
				return err
			}
			if parsed.Scheme != "" || parsed.Path == "" {
				continue
			}
			if !filepath.IsLocal(filepath.FromSlash(parsed.Path)) {
				return fmt.Errorf("unsafe distribution link in %s: %s", name, target)
			}
			if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(parsed.Path))); err != nil {
				return fmt.Errorf("broken distribution link in %s: %s", name, target)
			}
		}
	}
	return nil
}
