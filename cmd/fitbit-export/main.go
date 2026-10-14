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

	end := time.Now()
	start := time.Date(
		end.Year(),
		end.Month(),
		end.Day(),
		0,
		0,
		0,
		0,
		end.Location(),
	)

	result, err := service.Run(ctx, start, end)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("Steps total: %d", result.StepsTotal)
}
