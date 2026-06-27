package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:            "gCompare",
		Width:            1280,
		Height:           820,
		MinWidth:         860,
		MinHeight:        560,
		DisableResize:    false,
		WindowStartState: options.Normal,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 248, G: 250, B: 252, A: 1},
		OnStartup:        app.startup,
		Mac: &mac.Options{
			DisableZoom: false,
			Preferences: &mac.Preferences{
				FullscreenEnabled: mac.Enabled,
			},
		},
		Bind: []interface{}{
			app,
		},
	})
	if err != nil {
		println("Error:", err.Error())
	}
}
