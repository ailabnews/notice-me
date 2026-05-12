package main

import (
	"embed"
	"os"

	"github.com/wailsapp/wails/v3/pkg/application"

	"notify-me/internal/config"
	"notify-me/internal/logger"
	"notify-me/internal/singleton"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// Subcommand: "notify-me hook" — IPC client, no GUI needed.
	if len(os.Args) > 1 && os.Args[1] == "hook" {
		runHook()
		return
	}

	cfg, cfgErr := config.LoadOrInit()
	log := logger.New(cfg)

	if existing, err := singleton.AcquireOrActivate(cfg); err != nil {
		log.Fatal().Err(err).Msg("singleton lock failed")
	} else if existing {
		log.Info().Msg("another instance is running; activated and exiting")
		return
	}

	app := NewApp(cfg, log)
	if cfgErr != nil {
		app.QueueBanner("config corrupted, defaults loaded")
	}

	wailsApp := application.New(application.Options{
		Name:        "notify-me",
		Description: "HTTP-driven confirmation popup",
		Services: []application.Service{
			application.NewService(app),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
	})

	app.Boot(wailsApp)

	if err := wailsApp.Run(); err != nil {
		app.Shutdown()
		log.Fatal().Err(err).Msg("wails run failed")
	}
	// Ensure cleanup runs for any exit path (Dock quit, Cmd+Q, window close).
	app.Shutdown()
}
