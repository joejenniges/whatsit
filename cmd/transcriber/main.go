package main

import (
	"fmt"
	"log"
	"runtime/debug"

	"github.com/joe/radio-transcriber/internal/app"
	"github.com/joe/radio-transcriber/internal/config"
	"github.com/joe/radio-transcriber/internal/logging"
	"github.com/joe/radio-transcriber/internal/ui"
)

func main() {
	logResult, err := logging.Setup()
	if err != nil {
		// Last resort: try to show the error somewhere visible.
		log.Fatalf("setup logging: %v", err)
	}
	defer logResult.File.Close()

	// Catch panics on the main goroutine, write a crash report, and re-panic
	// so the runtime still produces its output (which goes to the log file
	// thanks to stderr redirection).
	defer func() {
		if r := recover(); r != nil {
			stack := debug.Stack()
			crashPath := logging.WriteCrashReport(logResult.LogsDir, r, stack)
			log.Printf("FATAL PANIC: %v (crash report: %s)", r, crashPath)
			// Write to the log file directly in case log is broken.
			fmt.Fprintf(logResult.File, "\n=== PANIC ===\n%v\n\n%s\n", r, stack)
			logResult.File.Sync()
			// Re-panic so the Go runtime writes its own trace too.
			panic(r)
		}
	}()

	log.Printf("RadioTranscriber starting")

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	uiApp := ui.NewApp(cfg)
	orch := app.NewOrchestrator(cfg, uiApp)
	orch.Start() // blocks via UI.Run()
}
