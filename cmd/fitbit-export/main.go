package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"fitbit-export/internal/auth"
	"fitbit-export/internal/config"
	"fitbit-export/internal/googlehealth"
	"fitbit-export/internal/httpclient"
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
	end := time.Now()
	start := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, end.Location())

	steps, err := client.Steps(ctx, start, end)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("steps: %d", steps.Total)

	for _, metric := range []string{
		"distance",
		"active-minutes",
		"total-calories",
		"heart-rate",
		"active-zone-minutes",
	} {
		printRollup(ctx, client, metric, start, end)
	}

	for _, metric := range []listedMetric{
		{name: "heart-rate-variability", filter: sampleFilter("heart_rate_variability", start, end)},
		{name: "daily-resting-heart-rate", filter: dailyFilter("dailyRestingHeartRate", start)},
		{name: "sleep", filter: sleepFilter(start, end)},
		{name: "weight", filter: sampleFilter("weight", start, end)},
		{name: "body-fat", filter: sampleFilter("body_fat", start, end)},
		{name: "exercise", filter: civilSessionFilter("exercise", start, end)},
		{name: "oxygen-saturation", filter: sampleFilter("oxygen_saturation", start, end)},
	} {
		printDataPoints(ctx, client, metric)
	}
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

type listedMetric struct {
	name   string
	filter string
}

func printRollup(ctx context.Context, client *googlehealth.Client, metric string, start time.Time, end time.Time) {
	points, err := client.RollUp(ctx, metric, start, end)
	if err != nil {
		log.Printf("%s: unavailable: %v", metric, err)
		return
	}
	printJSON(metric, points)
}

func printDataPoints(ctx context.Context, client *googlehealth.Client, metric listedMetric) {
	points, err := client.List(ctx, metric.name, metric.filter)
	if err != nil {
		log.Printf("%s: unavailable: %v", metric.name, err)
		return
	}
	printJSON(metric.name, points)
}

func printJSON(name string, value any) {
	output, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		log.Printf("%s: encode output: %v", name, err)
		return
	}
	fmt.Printf("\n%s:\n%s\n", name, output)
}

func intervalFilter(dataType string, start time.Time, end time.Time) string {
	return fmt.Sprintf(
		`%s.interval.start_time >= "%s" AND %s.interval.start_time < "%s"`,
		dataType, start.UTC().Format(time.RFC3339), dataType, end.UTC().Format(time.RFC3339),
	)
}

func sampleFilter(dataType string, start time.Time, end time.Time) string {
	return fmt.Sprintf(
		`%s.sample_time.physical_time >= "%s" AND %s.sample_time.physical_time < "%s"`,
		dataType, start.UTC().Format(time.RFC3339), dataType, end.UTC().Format(time.RFC3339),
	)
}

func dailyFilter(dataType string, day time.Time) string {
	nextDay := day.AddDate(0, 0, 1)
	return fmt.Sprintf(
		`%s.date >= "%s" AND %s.date < "%s"`,
		dataType, day.Format(time.DateOnly), dataType, nextDay.Format(time.DateOnly),
	)
}

func sleepFilter(start time.Time, end time.Time) string {
	return fmt.Sprintf(
		`sleep.interval.end_time >= "%s" AND sleep.interval.end_time < "%s"`,
		start.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339),
	)
}

func civilSessionFilter(dataType string, start time.Time, end time.Time) string {
	return fmt.Sprintf(
		`%s.interval.civil_start_time >= "%s" AND %s.interval.civil_start_time < "%s"`,
		dataType, start.Format("2006-01-02T15:04:05"), dataType, end.Format("2006-01-02T15:04:05"),
	)
}
