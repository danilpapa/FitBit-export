package report

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"
)

const (
	dateKey    = time.DateOnly
	weekDays   = 7
	monthDays  = 28
	secondsToH = 1.0 / 3600
	minutesToH = 1.0 / 60
	mmToKm     = 1.0 / 1_000_000

	zoneLookback = 48 * time.Hour
)

// Source is the part of the Google Health client the report reads from.
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

// Row compares one day's value with the averages of the days before it.
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
	fillMissing bool
}

var recoveryMetrics = []metric{
	{label: "Пульс покоя, уд/мин", dataType: "daily-resting-heart-rate", group: "dailyRestingHeartRate", fields: []string{"beatsPerMinute"}, scale: 1},
	{label: "HRV, мс", digits: 1, dataType: "daily-heart-rate-variability", group: "dailyHeartRateVariability", fields: []string{"averageHeartRateVariabilityMilliseconds"}, scale: 1},
	{label: "Дыхание, вд/мин", digits: 1, dataType: "daily-respiratory-rate", group: "dailyRespiratoryRate", fields: []string{"breathsPerMinute"}, scale: 1},
	{label: "Температура сна, °C", digits: 2, dataType: "daily-sleep-temperature-derivations", group: "dailySleepTemperatureDerivations", fields: []string{"nightlyTemperatureCelsius"}, scale: 1},
	{label: "SpO2, %", digits: 1, dataType: "daily-oxygen-saturation", group: "dailyOxygenSaturation", fields: []string{"averagePercentage"}, scale: 1},
}

var activityMetrics = []metric{
	{label: "Шаги", dataType: "steps", group: "steps", fields: []string{"countSum"}, scale: 1},
	{label: "Дистанция, км", digits: 1, dataType: "distance", group: "distance", fields: []string{"millimetersSum"}, scale: mmToKm},
	{label: "Зоны нагрузки, мин", dataType: "active-zone-minutes", group: "activeZoneMinutes", fields: []string{"sumInFatBurnHeartZone", "sumInCardioHeartZone", "sumInPeakHeartZone"}, scale: 1, fillMissing: true},
	{label: "Без движения, ч", digits: 1, dataType: "sedentary-period", group: "sedentaryPeriod", fields: []string{"durationSum"}, scale: secondsToH},
}

var fitnessMetric = metric{label: "VO2 max", digits: 1, dataType: "daily-vo2-max", group: "dailyVo2Max", fields: []string{"vo2Max"}, scale: 1}

type builder struct {
	source   Source
	ctx      context.Context
	day      time.Time
	report   Report
	requests int
	lastErr  error
}

// Build assembles the report for now's calendar date: activity covers midnight
// until now, sleep and recovery cover the night that ended on that date. A metric
// that fails is listed as unavailable; Build fails only when every request fails.
func Build(ctx context.Context, source Source, now time.Time) (Report, error) {
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	b := &builder{
		source: source,
		ctx:    ctx,
		day:    day,
		report: Report{Day: day, GeneratedAt: now},
	}

	for _, m := range recoveryMetrics {
		b.report.Recovery = append(b.report.Recovery, b.dailyRow(m))
	}
	b.addSleep()
	for _, m := range activityMetrics {
		b.report.Activity = append(b.report.Activity, b.rollupRow(m))
	}
	b.report.Activity = append(b.report.Activity, b.dailyRow(fitnessMetric))
	b.addWorkouts()

	if b.lastErr != nil && len(b.report.Unavailable) == b.requests {
		return Report{}, fmt.Errorf("no metric available: %w", b.lastErr)
	}

	note, err := LoadNote(day)
	if err != nil {
		log.Printf("note: unavailable: %v", err)
	}
	b.report.Note = note

	return b.report, nil
}

// DeviceNow converts now to the time zone of the newest steps point, because
// the API groups days by the band's zone, which can differ from this machine's.
func DeviceNow(ctx context.Context, source Source, now time.Time) time.Time {
	points, err := source.List(ctx, "steps", fmt.Sprintf(
		`steps.interval.start_time >= "%s"`,
		now.Add(-zoneLookback).UTC().Format(time.RFC3339),
	))
	if err != nil {
		log.Printf("device time zone: unavailable: %v", err)
		return now
	}

	var latest time.Time
	for _, raw := range points {
		var point struct {
			Steps struct {
				Interval struct {
					StartTime      string `json:"startTime"`
					StartUTCOffset string `json:"startUtcOffset"`
				} `json:"interval"`
			} `json:"steps"`
		}
		if err := json.Unmarshal(raw, &point); err != nil {
			log.Printf("device time zone: decode steps point: %v", err)
			return now
		}

		start, err := localTime(point.Steps.Interval.StartTime, point.Steps.Interval.StartUTCOffset)
		if err != nil {
			log.Printf("device time zone: decode steps point: %v", err)
			return now
		}
		if start.After(latest) {
			latest = start
		}
	}

	if latest.IsZero() {
		return now
	}
	return now.In(latest.Location())
}

func (b *builder) fail(label string, err error) {
	log.Printf("%s: unavailable: %v", label, err)
	b.report.Unavailable = append(b.report.Unavailable, label)
	b.lastErr = err
}

func (b *builder) dailyRow(m metric) Row {
	b.requests++
	points, err := b.source.List(b.ctx, m.dataType, dateFilter(m.dataType, b.day.AddDate(0, 0, -monthDays), b.day))
	if err != nil {
		b.fail(m.label, err)
		return Row{Label: m.label, Digits: m.digits}
	}

	series, err := collect(points, []string{m.group, "date"}, m)
	if err != nil {
		b.fail(m.label, err)
		return Row{Label: m.label, Digits: m.digits}
	}
	return newRow(m.label, m.digits, series, b.day)
}

func (b *builder) rollupRow(m metric) Row {
	b.requests++
	start := b.day.AddDate(0, 0, -monthDays)
	points, err := b.source.DailyRollUp(b.ctx, m.dataType, start, b.day.AddDate(0, 0, 1))
	if err != nil {
		b.fail(m.label, err)
		return Row{Label: m.label, Digits: m.digits}
	}

	series, err := collect(points, []string{"civilStartTime", "date"}, m)
	if err != nil {
		b.fail(m.label, err)
		return Row{Label: m.label, Digits: m.digits}
	}
	if m.fillMissing {
		for day := start; !day.After(b.day); day = day.AddDate(0, 0, 1) {
			if _, ok := series[day.Format(dateKey)]; !ok {
				series[day.Format(dateKey)] = 0
			}
		}
	}
	return newRow(m.label, m.digits, series, b.day)
}

func (b *builder) addSleep() {
	const label = "Сон"
	b.requests++

	start := b.day.AddDate(0, 0, -monthDays)
	points, err := b.source.List(b.ctx, "sleep", fmt.Sprintf(
		`sleep.interval.end_time >= "%s" AND sleep.interval.end_time < "%s"`,
		start.UTC().Format(time.RFC3339), b.day.AddDate(0, 0, 1).UTC().Format(time.RFC3339),
	))
	if err == nil {
		var nights map[string]night
		if nights, err = decodeNights(points); err == nil {
			b.addSleepRows(nights)
			return
		}
	}

	b.fail(label, err)
	for _, m := range sleepLabels {
		b.report.Sleep = append(b.report.Sleep, Row{Label: m.label, Digits: m.digits})
	}
}

var sleepLabels = []struct {
	label  string
	digits int
	value  func(night) (float64, bool)
}{
	{"Сон всего, ч", 1, func(n night) (float64, bool) { return n.asleep * minutesToH, true }},
	{"Глубокий, мин", 0, func(n night) (float64, bool) { return stage(n, "DEEP") }},
	{"REM, мин", 0, func(n night) (float64, bool) { return stage(n, "REM") }},
	{"Пробуждения, мин", 0, func(n night) (float64, bool) { return n.awake, true }},
}

func stage(n night, name string) (float64, bool) {
	minutes, ok := n.stages[name]
	return minutes, ok
}

func (b *builder) addSleepRows(nights map[string]night) {
	if last, ok := nights[b.day.Format(dateKey)]; ok {
		b.report.SleepStart = last.start
		b.report.SleepEnd = last.end
	}

	for _, row := range sleepLabels {
		series := map[string]float64{}
		for date, n := range nights {
			if value, ok := row.value(n); ok {
				series[date] = value
			}
		}
		b.report.Sleep = append(b.report.Sleep, newRow(row.label, row.digits, series, b.day))
	}
}

func (b *builder) addWorkouts() {
	const label = "Тренировки"
	b.requests++

	start := b.day.AddDate(0, 0, -monthDays)
	points, err := b.source.List(b.ctx, "exercise", fmt.Sprintf(
		`exercise.interval.civil_start_time >= "%s" AND exercise.interval.civil_start_time < "%s"`,
		start.Format("2006-01-02T15:04:05"), b.day.AddDate(0, 0, 1).Format("2006-01-02T15:04:05"),
	))
	if err != nil {
		b.fail(label, err)
		return
	}

	byDay, err := decodeWorkouts(points)
	if err != nil {
		b.fail(label, err)
		return
	}

	b.report.Workouts = byDay[b.day.Format(dateKey)]
	b.report.WeekTotal = workoutTotal(byDay, b.day, weekDays)
	b.report.MonthTotal = workoutTotal(byDay, b.day, monthDays)
}

func workoutTotal(byDay map[string][]Workout, day time.Time, window int) WorkoutTotal {
	var total WorkoutTotal
	for i := 1; i <= window; i++ {
		for _, workout := range byDay[day.AddDate(0, 0, -i).Format(dateKey)] {
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
	if value, ok := series[day.Format(dateKey)]; ok {
		row.Value = &value
	}
	return row
}

// average covers the window days before day, skipping days without data.
func average(series map[string]float64, day time.Time, window int) Average {
	result := Average{Window: window}
	var sum float64
	for i := 1; i <= window; i++ {
		if value, ok := series[day.AddDate(0, 0, -i).Format(dateKey)]; ok {
			sum += value
			result.Days++
		}
	}
	if result.Days > 0 {
		result.Value = sum / float64(result.Days)
	}
	return result
}

func dateFilter(dataType string, start time.Time, last time.Time) string {
	field := strings.ReplaceAll(dataType, "-", "_")
	return fmt.Sprintf(
		`%s.date >= "%s" AND %s.date < "%s"`,
		field, start.Format(dateKey), field, last.AddDate(0, 0, 1).Format(dateKey),
	)
}

func collect(points []json.RawMessage, datePath []string, m metric) (map[string]float64, error) {
	series := map[string]float64{}

	for _, raw := range points {
		var point map[string]any
		if err := json.Unmarshal(raw, &point); err != nil {
			return nil, fmt.Errorf("decode %s point: %w", m.dataType, err)
		}

		date, err := pointDate(point, datePath)
		if err != nil {
			return nil, fmt.Errorf("decode %s point: %w", m.dataType, err)
		}

		group, _ := point[m.group].(map[string]any)
		var total float64
		found := false
		for _, field := range m.fields {
			if value, ok := number(group[field]); ok {
				total += value
				found = true
			}
		}
		if found {
			series[date] = total * m.scale
		}
	}

	return series, nil
}

func pointDate(point map[string]any, path []string) (string, error) {
	current := point
	for _, key := range path {
		next, ok := current[key].(map[string]any)
		if !ok {
			return "", fmt.Errorf("missing %s", strings.Join(path, "."))
		}
		current = next
	}

	year, _ := current["year"].(float64)
	month, _ := current["month"].(float64)
	day, _ := current["day"].(float64)
	return fmt.Sprintf("%04d-%02d-%02d", int(year), int(month), int(day)), nil
}

// number reads API numerics, which arrive as numbers, strings or "34560s" durations.
func number(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSuffix(typed, "s"), 64)
		return parsed, err == nil
	}
	return 0, false
}

type night struct {
	start  time.Time
	end    time.Time
	asleep float64
	awake  float64
	stages map[string]float64
}

func decodeNights(points []json.RawMessage) (map[string]night, error) {
	nights := map[string]night{}

	for _, raw := range points {
		var point struct {
			Sleep struct {
				Interval struct {
					StartTime      string `json:"startTime"`
					StartUTCOffset string `json:"startUtcOffset"`
					EndTime        string `json:"endTime"`
					EndUTCOffset   string `json:"endUtcOffset"`
				} `json:"interval"`
				Metadata struct {
					MainSleep bool `json:"mainSleep"`
				} `json:"metadata"`
				Summary struct {
					MinutesAsleep float64 `json:"minutesAsleep,string"`
					MinutesAwake  float64 `json:"minutesAwake,string"`
					StagesSummary []struct {
						Type    string  `json:"type"`
						Minutes float64 `json:"minutes,string"`
					} `json:"stagesSummary"`
				} `json:"summary"`
			} `json:"sleep"`
		}
		if err := json.Unmarshal(raw, &point); err != nil {
			return nil, fmt.Errorf("decode sleep point: %w", err)
		}
		if !point.Sleep.Metadata.MainSleep {
			continue
		}

		start, err := localTime(point.Sleep.Interval.StartTime, point.Sleep.Interval.StartUTCOffset)
		if err != nil {
			return nil, fmt.Errorf("decode sleep start: %w", err)
		}
		end, err := localTime(point.Sleep.Interval.EndTime, point.Sleep.Interval.EndUTCOffset)
		if err != nil {
			return nil, fmt.Errorf("decode sleep end: %w", err)
		}

		current := night{
			start:  start,
			end:    end,
			asleep: point.Sleep.Summary.MinutesAsleep,
			awake:  point.Sleep.Summary.MinutesAwake,
			stages: map[string]float64{},
		}
		for _, stage := range point.Sleep.Summary.StagesSummary {
			current.stages[stage.Type] = stage.Minutes
		}

		date := end.Format(dateKey)
		if existing, ok := nights[date]; !ok || current.asleep > existing.asleep {
			nights[date] = current
		}
	}

	return nights, nil
}

func decodeWorkouts(points []json.RawMessage) (map[string][]Workout, error) {
	byDay := map[string][]Workout{}

	for _, raw := range points {
		var point struct {
			Exercise struct {
				Interval struct {
					StartTime      string `json:"startTime"`
					StartUTCOffset string `json:"startUtcOffset"`
				} `json:"interval"`
				ExerciseType   string `json:"exerciseType"`
				DisplayName    string `json:"displayName"`
				ActiveDuration string `json:"activeDuration"`
				MetricsSummary struct {
					DistanceMillimeters float64 `json:"distanceMillimeters"`
					AverageHeartRate    int64   `json:"averageHeartRateBeatsPerMinute,string"`
				} `json:"metricsSummary"`
			} `json:"exercise"`
		}
		if err := json.Unmarshal(raw, &point); err != nil {
			return nil, fmt.Errorf("decode exercise point: %w", err)
		}

		start, err := localTime(point.Exercise.Interval.StartTime, point.Exercise.Interval.StartUTCOffset)
		if err != nil {
			return nil, fmt.Errorf("decode exercise start: %w", err)
		}

		duration := time.Duration(0)
		if point.Exercise.ActiveDuration != "" {
			if duration, err = time.ParseDuration(point.Exercise.ActiveDuration); err != nil {
				return nil, fmt.Errorf("decode exercise duration: %w", err)
			}
		}

		name := point.Exercise.DisplayName
		if name == "" {
			name = point.Exercise.ExerciseType
		}

		date := start.Format(dateKey)
		byDay[date] = append(byDay[date], Workout{
			Name:       name,
			Minutes:    duration.Minutes(),
			Kilometers: point.Exercise.MetricsSummary.DistanceMillimeters * mmToKm,
			HeartRate:  point.Exercise.MetricsSummary.AverageHeartRate,
		})
	}

	return byDay, nil
}

func localTime(value string, offset string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, err
	}

	shift, err := time.ParseDuration(offset)
	if err != nil {
		return time.Time{}, errors.Join(errors.New("parse UTC offset"), err)
	}

	return parsed.In(time.FixedZone("", int(shift.Seconds()))), nil
}
