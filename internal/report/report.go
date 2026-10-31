package report

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"strings"
	"time"
)

const (
	weekDays = 7

	clockLookback   = 48 * time.Hour
	civilTimeLayout = "2006-01-02T15:04:05"

	secondsToHours  = 1.0 / 3600
	minutesToHours  = 1.0 / 60
	millimetersToKm = 1.0 / 1_000_000

	secondsPerMeterToMinPerKm = 1000.0 / 60
)

type Source interface {
	List(ctx context.Context, dataType string, filter string) ([]json.RawMessage, error)
	Latest(ctx context.Context, dataType string, filter string) (json.RawMessage, error)
	DailyRollUp(ctx context.Context, dataType string, start time.Time, end time.Time) ([]json.RawMessage, error)
}

type Clock struct {
	Now      time.Time
	LastData *time.Time
}

type Report struct {
	Date          string            `json:"date"`
	GeneratedAt   time.Time         `json:"generated_at"`
	LastDataAt    *time.Time        `json:"last_data_at"`
	Unavailable   []string          `json:"unavailable"`
	LastNight     LastNight         `json:"last_night"`
	Recovery      map[string]Metric `json:"recovery_last_night"`
	Activity      map[string]Metric `json:"activity_today"`
	WorkoutsToday []Workout         `json:"workouts_today"`
	Note          *string           `json:"note"`
}

type LastNight struct {
	StartedAt    *time.Time `json:"started_at"`
	EndedAt      *time.Time `json:"ended_at"`
	AsleepHours  Metric     `json:"asleep_hours"`
	DeepMinutes  Metric     `json:"deep_minutes"`
	RemMinutes   Metric     `json:"rem_minutes"`
	AwakeMinutes Metric     `json:"awake_minutes"`
}

type Metric struct {
	Value *float64 `json:"value"`
	Avg7d *float64 `json:"avg_7d"`
}

type Workout struct {
	Name              string         `json:"name"`
	Type              string         `json:"type"`
	StartedAt         time.Time      `json:"started_at"`
	EndedAt           time.Time      `json:"ended_at"`
	Minutes           float64        `json:"minutes"`
	CaloriesKcal      float64        `json:"calories_kcal,omitempty"`
	Steps             int64          `json:"steps,omitempty"`
	DistanceKm        float64        `json:"distance_km,omitempty"`
	AvgPaceMinPerKm   float64        `json:"avg_pace_min_per_km,omitempty"`
	AvgHRBpm          int64          `json:"avg_hr_bpm,omitempty"`
	ActiveZoneMinutes int64          `json:"active_zone_minutes,omitempty"`
	HRZoneMinutes     *HRZoneMinutes `json:"hr_zone_minutes,omitempty"`
	Notes             string         `json:"notes,omitempty"`
}

type HRZoneMinutes struct {
	Light    float64 `json:"light"`
	Moderate float64 `json:"moderate"`
	Vigorous float64 `json:"vigorous"`
	Peak     float64 `json:"peak"`
}

type metric struct {
	key         string
	digits      int
	dataType    string
	group       string
	fields      []string
	scale       float64
	rollup      bool
	fillMissing bool
}

var recoveryMetrics = []metric{
	{key: "resting_hr_bpm", dataType: "daily-resting-heart-rate", group: "dailyRestingHeartRate", fields: []string{"beatsPerMinute"}},
	{key: "hrv_ms", digits: 1, dataType: "daily-heart-rate-variability", group: "dailyHeartRateVariability", fields: []string{"averageHeartRateVariabilityMilliseconds"}},
	{key: "breaths_per_minute", digits: 1, dataType: "daily-respiratory-rate", group: "dailyRespiratoryRate", fields: []string{"breathsPerMinute"}},
	{key: "sleep_temperature_celsius", digits: 2, dataType: "daily-sleep-temperature-derivations", group: "dailySleepTemperatureDerivations", fields: []string{"nightlyTemperatureCelsius"}},
	{key: "spo2_percent", digits: 1, dataType: "daily-oxygen-saturation", group: "dailyOxygenSaturation", fields: []string{"averagePercentage"}},
}

var activityMetrics = []metric{
	{key: "steps", dataType: "steps", group: "steps", fields: []string{"countSum"}, rollup: true},
	{key: "distance_km", digits: 1, dataType: "distance", group: "distance", fields: []string{"millimetersSum"}, scale: millimetersToKm, rollup: true},
	{key: "active_zone_minutes", dataType: "active-zone-minutes", group: "activeZoneMinutes", fields: []string{"sumInFatBurnHeartZone", "sumInCardioHeartZone", "sumInPeakHeartZone"}, rollup: true, fillMissing: true},
	{key: "sedentary_hours", digits: 1, dataType: "sedentary-period", group: "sedentaryPeriod", fields: []string{"durationSum"}, scale: secondsToHours, rollup: true},
	{key: "vo2_max", digits: 1, dataType: "daily-vo2-max", group: "dailyVo2Max", fields: []string{"vo2Max"}},
}

type builder struct {
	source Source
	day    time.Time
	start  time.Time
	end    time.Time
	report Report
	loaded bool
	err    error
}

func DeviceClock(ctx context.Context, source Source, now time.Time) Clock {
	clock := Clock{Now: now}

	point, err := source.Latest(ctx, "heart-rate", fmt.Sprintf(
		`heart_rate.sample_time.physical_time >= "%s"`,
		now.Add(-clockLookback).UTC().Format(time.RFC3339),
	))
	if err != nil {
		log.Printf("device clock: unavailable: %v", err)
		return clock
	}
	if point == nil {
		return clock
	}

	latest, err := heartRateTime(point)
	if err != nil {
		log.Printf("device clock: %v", err)
		return clock
	}

	clock.Now = now.In(latest.Location())
	clock.LastData = &latest
	return clock
}

func Build(ctx context.Context, source Source, clock Clock) (Report, error) {
	now := clock.Now
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	b := &builder{
		source: source,
		day:    day,
		start:  day.AddDate(0, 0, -weekDays),
		end:    day.AddDate(0, 0, 1),
		report: Report{
			Date:        dateKey(day),
			GeneratedAt: now.Truncate(time.Second),
			LastDataAt:  clock.LastData,
			Unavailable: []string{},
		},
	}

	b.report.Recovery = b.metrics(ctx, recoveryMetrics)
	b.addSleep(ctx)
	b.report.Activity = b.metrics(ctx, activityMetrics)
	b.addWorkouts(ctx)

	if !b.loaded {
		return Report{}, fmt.Errorf("no metric available: %w", b.err)
	}

	note, err := LoadNote(day)
	if err != nil {
		log.Printf("note: unavailable: %v", err)
	}
	if note != "" {
		b.report.Note = &note
	}

	return b.report, nil
}

func (b *builder) attempt(key string, load func() error) bool {
	if err := load(); err != nil {
		log.Printf("%s: unavailable: %v", key, err)
		b.report.Unavailable = append(b.report.Unavailable, key)
		b.err = err
		return false
	}
	b.loaded = true
	return true
}

func (b *builder) metrics(ctx context.Context, metrics []metric) map[string]Metric {
	result := make(map[string]Metric, len(metrics))
	for _, m := range metrics {
		var series map[string]float64
		b.attempt(m.key, func() (err error) {
			series, err = b.series(ctx, m)
			return err
		})
		result[m.key] = newMetric(series, b.day, m.digits)
	}
	return result
}

func (b *builder) series(ctx context.Context, m metric) (map[string]float64, error) {
	if !m.rollup {
		points, err := b.source.List(ctx, m.dataType, dateFilter(m.dataType, b.start, b.end))
		if err != nil {
			return nil, err
		}
		return collect(points, m.group, m)
	}

	points, err := b.source.DailyRollUp(ctx, m.dataType, b.start, b.end)
	if err != nil {
		return nil, err
	}
	series, err := collect(points, "civilStartTime", m)
	if err != nil {
		return nil, err
	}

	if m.fillMissing {
		for day := b.start; day.Before(b.end); day = day.AddDate(0, 0, 1) {
			if _, ok := series[dateKey(day)]; !ok {
				series[dateKey(day)] = 0
			}
		}
	}
	return series, nil
}

func (b *builder) addSleep(ctx context.Context) {
	var nights map[string]night
	b.attempt("sleep", func() error {
		points, err := b.source.List(ctx, "sleep", fmt.Sprintf(
			`sleep.interval.end_time >= "%s" AND sleep.interval.end_time < "%s"`,
			b.start.UTC().Format(time.RFC3339), b.end.UTC().Format(time.RFC3339),
		))
		if err != nil {
			return err
		}
		nights, err = decodeNights(points)
		return err
	})

	b.report.LastNight = LastNight{
		AsleepHours:  b.nightMetric(nights, minutesAsleep, minutesToHours, 1),
		DeepMinutes:  b.nightMetric(nights, "DEEP", 1, 0),
		RemMinutes:   b.nightMetric(nights, "REM", 1, 0),
		AwakeMinutes: b.nightMetric(nights, minutesAwake, 1, 0),
	}
	if last, ok := nights[dateKey(b.day)]; ok {
		b.report.LastNight.StartedAt = &last.start
		b.report.LastNight.EndedAt = &last.end
	}
}

func (b *builder) nightMetric(nights map[string]night, key string, scale float64, digits int) Metric {
	series := map[string]float64{}
	for date, n := range nights {
		if minutes, ok := n.minutes[key]; ok {
			series[date] = minutes * scale
		}
	}
	return newMetric(series, b.day, digits)
}

func (b *builder) addWorkouts(ctx context.Context) {
	b.attempt("workouts", func() error {
		points, err := b.source.List(ctx, "exercise", fmt.Sprintf(
			`exercise.interval.civil_start_time >= "%s" AND exercise.interval.civil_start_time < "%s"`,
			b.day.Format(civilTimeLayout), b.end.Format(civilTimeLayout),
		))
		if err != nil {
			return err
		}
		b.report.WorkoutsToday, err = decodeWorkouts(points)
		return err
	})
}

func newMetric(series map[string]float64, day time.Time, digits int) Metric {
	metric := Metric{Avg7d: average(series, day, digits)}
	if value, ok := series[dateKey(day)]; ok {
		rounded := round(value, digits)
		metric.Value = &rounded
	}
	return metric
}

func average(series map[string]float64, day time.Time, digits int) *float64 {
	var sum float64
	days := 0
	for i := 1; i <= weekDays; i++ {
		if value, ok := series[dateKey(day.AddDate(0, 0, -i))]; ok {
			sum += value
			days++
		}
	}
	if days == 0 {
		return nil
	}

	result := round(sum/float64(days), digits)
	return &result
}

func round(value float64, digits int) float64 {
	scale := math.Pow10(digits)
	return math.Round(value*scale) / scale
}

func dateKey(day time.Time) string {
	return day.Format(time.DateOnly)
}

func dateFilter(dataType string, start time.Time, end time.Time) string {
	field := strings.ReplaceAll(dataType, "-", "_")
	return fmt.Sprintf(
		`%s.date >= "%s" AND %s.date < "%s"`,
		field, dateKey(start), field, dateKey(end),
	)
}
