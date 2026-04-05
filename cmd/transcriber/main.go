package main

import (
	"log"

	"github.com/joe/radio-transcriber/internal/app"
	"github.com/joe/radio-transcriber/internal/config"
	"github.com/joe/radio-transcriber/internal/logging"
	"github.com/joe/radio-transcriber/internal/ui"
)

func main() {
	logFile, err := logging.Setup()
	if err != nil {
		log.Fatalf("setup logging: %v", err)
	}
	defer logFile.Close()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	uiApp := ui.NewApp(cfg)
	orch := app.NewOrchestrator(cfg, uiApp)
	orch.Start() // blocks via UI.Run()
}
