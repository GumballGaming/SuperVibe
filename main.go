package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"

	"supervibe/internal/app"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	a := app.NewApp()

	err := wails.Run(&options.App{
		Title:             "SuperVibe",
		Width:             1440,
		Height:            920,
		MinWidth:          1000,
		MinHeight:         640,
		Frameless:         true,
		HideWindowOnClose: true,
		BackgroundColour:  &options.RGBA{R: 13, G: 13, B: 14, A: 255},
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup:  a.Startup,
		OnShutdown: a.Shutdown,
		Bind: []interface{}{
			a,
		},
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
			DisableWindowIcon:    false,
			Theme:                windows.Dark,
			CustomTheme: &windows.ThemeSettings{
				DarkModeTitleBar:   0x0E0D0D,
				DarkModeTitleText:  0xF2F2F2,
				DarkModeBorder:     0x303030,
				LightModeTitleBar:  0x0E0D0D,
				LightModeTitleText: 0xF2F2F2,
				LightModeBorder:    0x303030,
			},
		},
	})
	if err != nil {
		println("Error:", err.Error())
	}
}
