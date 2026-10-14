package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"fitbit-export/internal/config"
	"fitbit-export/internal/googlehealth"
	"fitbit-export/internal/httpclient"
)

func main() {
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	client := googlehealth.New(httpclient.New(config.Load()))

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

	result, err := client.Steps(ctx, start, end)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("Steps total: %d", result.Total)
}
