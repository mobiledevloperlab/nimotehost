// SPDX-License-Identifier: Apache-2.0
package main

import (
	"embed"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
)

//go:embed assets/icon-256.png assets/brand-128.png assets/tray-64.png
var brandingFiles embed.FS

var applicationLogo = brandingResource("icon-256.png")
var sidebarLogo = brandingResource("brand-128.png")
var trayLogo = brandingResource("tray-64.png")

func brandingResource(name string) fyne.Resource {
	data, err := brandingFiles.ReadFile("assets/" + name)
	if err != nil {
		panic(err)
	}
	return fyne.NewStaticResource(name, data)
}

func brandImage(size float32) fyne.CanvasObject {
	image := canvas.NewImageFromResource(sidebarLogo)
	image.FillMode = canvas.ImageFillContain
	image.ScaleMode = canvas.ImageScaleSmooth
	image.SetMinSize(fyne.NewSquareSize(size))
	return image
}
