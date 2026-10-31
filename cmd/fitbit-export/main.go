package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"fitbit-export/internal/auth"
	"fitbit-export/internal/config"
	"fitbit-export/internal/googlehealth"
	"fitbit-export/internal/httpclient"
	"fitbit-export/internal/report"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg := config.Load()
	if len(os.Args) > 1 && os.Args[1] == "auth" {
		if err := runAuth(ctx, cfg); err != nil {
			log.Fatal(err)
		}
		return
	}

	client := googlehealth.New(httpclient.New(cfg))
	now := report.DeviceNow(ctx, client, time.Now())

	if len(os.Args) > 1 && os.Args[1] == "note" {
		if err := runNote(now, os.Args[2:]); err != nil {
			log.Fatal(err)
		}
		return
	}

	daily, err := report.Build(ctx, client, now)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Print(report.Render(daily))
}

func runAuth(ctx context.Context, cfg config.Config) error {
	result, err := auth.Login(ctx, cfg)
	if err != nil {
		return err
	}
	if err := config.SaveRefreshToken(result.RefreshToken); err != nil {
		return err
	}

	for _, scope := range result.MissingScopes {
		log.Printf("warning: scope not granted: %s", scope)
	}
	log.Print("refresh token saved to .env")
	return nil
}

func runNote(now time.Time, words []string) error {
	text := strings.TrimSpace(strings.Join(words, " "))
	if text == "" {
		return fmt.Errorf(`usage: fitbit-export note "how you feel today"`)
	}
	return report.SaveNote(now, text)
}
