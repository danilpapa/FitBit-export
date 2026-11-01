package main

import (
	"context"
	"encoding/json"
	"errors"
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

	if err := run(ctx, os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, args []string) error {
	command := ""
	if len(args) > 0 {
		command = args[0]
	}

	cfg := config.Load()
	if command == "auth" {
		return runAuth(ctx, cfg)
	}

	client := googlehealth.New(httpclient.New(cfg))
	clock := report.DeviceClock(ctx, client, time.Now())
	if command == "note" {
		return runNote(clock.Now, args[1:])
	}
	if command == "strength" {
		log, err := report.BuildStrengthLog(ctx, client, clock, report.StrengthHistoryDays)
		if err != nil {
			return err
		}
		return printJSON(log)
	}

	daily, err := report.Build(ctx, client, clock)
	if err != nil {
		return err
	}
	return printJSON(daily)
}

func printJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
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
		return errors.New(`usage: fitbit-export note "how you feel today"`)
	}
	return report.SaveNote(now, text)
}
