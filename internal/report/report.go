package report

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
)

const (
	weekDays  = 7
	monthDays = 28

	zoneLookback    = 48 * time.Hour
	civilTimeLayout = "2006-01-02T15:04:05"

	secondsToHours  = 1.0 / 3600
	minutesToHours  = 1.0 / 60
	millimetersToKm = 1.0 / 1_000_000
)

type Source interface {
	List(ctx context.Context, dataType string, filter string) ([]json.RawMessage, error)
	DailyRollUp(ctx context.Context, dataType string, start time.Time, end time.Time) ([]json.RawMessage, error)
}

type Report struct {
	Day         time.Time
	GeneratedAt time.Time
	Recovery    []Row
	SleepStart  time.Time
	SleepEnd    time.Time
	Sleep       []Row
	Activity    []Row
	Workouts    []Workout
	WeekTotal   WorkoutTotal
	MonthTotal  WorkoutTotal
	Note        string
	Unavailable []string
}

type Row struct {
	Label  string
	Digits int
	Value  *float64
	Week   Average
	Month  Average
}

type Average struct {
	Value  float64
	Days   int
	Window int
}

type Workout struct {
	Name       string
	Minutes    float64
	Kilometers float64
	HeartRate  int64
}

type WorkoutTotal struct {
	Count   int
	Minutes float64
}

type metric struct {
	label       string
	digits      int
	dataType    string
	group       string
	fields      []string
	scale       float64
	rollup      bool
	fillMissing bool
}

var recoveryMetrics = []metric{
	{label: "Пульс покоя, уд/мин", dataType: "daily-resting-heart-rate", group: "dailyRestingHeartRate", fields: []string{"beatsPerMinute"}},
	{label: "HRV, мс", digits: 1, dataType: "daily-heart-rate-variability", group: "dailyHeartRateVariability", fields: []string{"averageHeartRateVariabilityMilliseconds"}},
	{label: "Дыхание, вд/мин", digits: 1, dataType: "daily-respiratory-rate", group: "dailyRespiratoryRate", fields: []string{"breathsPerMinute"}},
	{label: "Температура сна, °C", digits: 2, dataType: "daily-sleep-temperature-derivations", group: "dailySleepTemperatureDerivations", fields: []string{"nightlyTemperatureCelsius"}},
	{label: "SpO2, %", digits: 1, dataType: "daily-oxygen-saturation", group: "dailyOxygenSaturation", fields: []string{"averagePercentage"}},
}

var activityMetrics = []metric{
	{label: "Шаги", dataType: "steps", group: "steps", fields: []string{"countSum"}, rollup: true},
	{label: "Дистанция, км", digits: 1, dataType: "distance", group: "distance", fields: []string{"millimetersSum"}, scale: millimetersToKm, rollup: true},
	{label: "Зоны нагрузки, мин", dataType: "active-zone-minutes", group: "activeZoneMinutes", fields: []string{"sumInFatBurnHeartZone", "sumInCardioHeartZone", "sumInPeakHeartZone"}, rollup: true, fillMissing: true},
	{label: "Без движения, ч", digits: 1, dataType: "sedentary-period", group: "sedentaryPeriod", fields: []string{"durationSum"}, scale: secondsToHours, rollup: true},
	{label: "VO2 max", digits: 1, dataType: "daily-vo2-max", group: "dailyVo2Max", fields: []string{"vo2Max"}},
}

var sleepMetrics = []struct {
	label  string
	digits int
	key    string
	scale  float64
}{
	{label: "Сон всего, ч", digits: 1, key: minutesAsleep, scale: minutesToHours},
	{label: "Глубокий, мин", key: "DEEP", scale: 1},
	{label: "REM, мин", key: "REM", scale: 1},
	{label: "Пробуждения, мин", key: minutesAwake, scale: 1},
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

func DeviceNow(ctx context.Context, source Source, now time.Time) time.Time {
	points, err := source.List(ctx, "steps", fmt.Sprintf(
		`steps.interval.start_time >= "%s"`,
		now.Add(-zoneLookback).UTC().Format(time.RFC3339),
	))
	if err != nil {
		log.Printf("device time zone: unavailable: %v", err)
		return now
	}

	latest, err := latestStepStart(points)
	if err != nil {
		log.Printf("device time zone: %v", err)
		return now
	}
	if latest.IsZero() {
		return now
	}
	return now.In(latest.Location())
}

func Build(ctx context.Context, source Source, now time.Time) (Report, error) {
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	b := &builder{
		source: source,
		day:    day,
		start:  day.AddDate(0, 0, -monthDays),
		end:    day.AddDate(0, 0, 1),
		report: Report{Day: day, GeneratedAt: now},
	}

	for _, m := range recoveryMetrics {
		b.report.Recovery = append(b.report.Recovery, b.row(ctx, m))
	}
	b.addSleep(ctx)
	for _, m := range activityMetrics {
		b.report.Activity = append(b.report.Activity, b.row(ctx, m))
	}
	b.addWorkouts(ctx)

	if !b.loaded {
		return Report{}, fmt.Errorf("no metric available: %w", b.err)
	}

	note, err := LoadNote(day)
	if err != nil {
		log.Printf("note: unavailable: %v", err)
	}
	b.report.Note = note

	return b.report, nil
}

func (b *builder) attempt(label string, load func() error) {
	if err := load(); err != nil {
		log.Printf("%s: unavailable: %v", label, err)
		b.report.Unavailable = append(b.report.Unavailable, label)
		b.err = err
		return
	}
	b.loaded = true
}

func (b *builder) row(ctx context.Context, m metric) Row {
	var series map[string]float64
	b.attempt(m.label, func() (err error) {
		series, err = b.series(ctx, m)
		return err
	})
	return newRow(m.label, m.digits, series, b.day)
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
	b.attempt("Сон", func() error {
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

	if last, ok := nights[dateKey(b.day)]; ok {
		b.report.SleepStart = last.start
		b.report.SleepEnd = last.end
	}

	for _, m := range sleepMetrics {
		series := map[string]float64{}
		for date, n := range nights {
			if minutes, ok := n.minutes[m.key]; ok {
				series[date] = minutes * m.scale
			}
		}
		b.report.Sleep = append(b.report.Sleep, newRow(m.label, m.digits, series, b.day))
	}
}

func (b *builder) addWorkouts(ctx context.Context) {
	var byDay map[string][]Workout
	b.attempt("Тренировки", func() error {
		points, err := b.source.List(ctx, "exercise", fmt.Sprintf(
			`exercise.interval.civil_start_time >= "%s" AND exercise.interval.civil_start_time < "%s"`,
			b.start.Format(civilTimeLayout), b.end.Format(civilTimeLayout),
		))
		if err != nil {
			return err
		}
		byDay, err = decodeWorkouts(points)
		return err
	})

	b.report.Workouts = byDay[dateKey(b.day)]
	b.report.WeekTotal = workoutTotal(byDay, b.day, weekDays)
	b.report.MonthTotal = workoutTotal(byDay, b.day, monthDays)
}

func workoutTotal(byDay map[string][]Workout, day time.Time, window int) WorkoutTotal {
	var total WorkoutTotal
	for _, date := range daysBefore(day, window) {
		for _, workout := range byDay[date] {
			total.Count++
			total.Minutes += workout.Minutes
		}
	}
	return total
}

func newRow(label string, digits int, series map[string]float64, day time.Time) Row {
	row := Row{
		Label:  label,
		Digits: digits,
		Week:   average(series, day, weekDays),
		Month:  average(series, day, monthDays),
	}
	if value, ok := series[dateKey(day)]; ok {
		row.Value = &value
	}
	return row
}

func average(series map[string]float64, day time.Time, window int) Average {
	result := Average{Window: window}
	var sum float64
	for _, date := range daysBefore(day, window) {
		if value, ok := series[date]; ok {
			sum += value
			result.Days++
		}
	}
	if result.Days > 0 {
		result.Value = sum / float64(result.Days)
	}
	return result
}

func daysBefore(day time.Time, count int) []string {
	dates := make([]string, 0, count)
	for i := 1; i <= count; i++ {
		dates = append(dates, dateKey(day.AddDate(0, 0, -i)))
	}
	return dates
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
