package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"fitbit-export/internal/config"
	"fitbit-export/internal/exporter"
	"fitbit-export/internal/healthapi"
	"fitbit-export/internal/httpclient"
	"fitbit-export/internal/oauth"
	"fitbit-export/internal/output"
)

func main() {
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	cfg := config.Load()
	
	oauthConfig := oauth.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RefreshToken: cfg.RefreshToken,
	}

	http := httpclient.New(oauthConfig)
	api := healthapi.New(http)
	service := exporter.New(api)

	end := time.Now().UTC()
	start := end.Add(-24 * time.Hour)

	log.Printf("Exporting data from %s to %s", start, end)

	result, err := service.Run(ctx, start, end)
	if err != nil {
		log.Fatal(err)
	}

	path, err := output.SaveJSON(cfg.OutputDir, result)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("Report saved: %s", path)
	log.Printf("Steps data points: %d", len(result.Steps))
}