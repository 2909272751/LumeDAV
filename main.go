package main

import (
	"embed"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// Create an instance of the app structure
	app := NewApp()

	// Create application with options
	err := wails.Run(&options.App{
		Title:     "LumeDAV v" + appVersion,
		Width:     1024,
		Height:    720,
		MinWidth:  860,
		MinHeight: 620,
		StartHidden: hasLaunchArg(os.Args[1:], "--autostart") ||
			hasLaunchArg(os.Args[1:], "--shutdown-for-update"),
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 7, G: 11, B: 24, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		OnBeforeClose:    app.beforeClose,
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId:               "f62e20de-12ba-4b52-b139-0fb0748b16ad",
			OnSecondInstanceLaunch: app.secondInstanceLaunch,
		},
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
