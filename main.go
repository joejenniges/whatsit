package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"

	"github.com/joe/radio-transcriber/internal/logging"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// Set up logging first.
	logResult, err := logging.Setup()
	if err != nil {
		panic("logging setup failed: " + err.Error())
	}
	defer logResult.File.Close()

	app := NewApp()

	err = wails.Run(&options.App{
		Title:  "RadioTranscriber",
		Width:  900,
		Height: 600,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		Bind: []interface{}{
			app,
		},
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
		},
	})

	if err != nil {
		panic(err)
	}
}
