package report

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

var testNow = time.Date(2026, time.October, 31, 22, 30, 15, 500, time.UTC)

type fakeSource struct {
	lists     map[string][]string
	latest    map[string]string
	rollups   map[string][]string
	failing   map[string]bool
	failAll   bool
	rollupEnd *time.Time
	filters   map[string]string
}

func (f fakeSource) List(_ context.Context, dataType string, filter string) ([]json.RawMessage, error) {
	if f.failAll || f.failing[dataType] {
		return nil, errors.New("HTTP 503")
	}
	if f.filters != nil {
		f.filters[dataType] = filter
	}
	return raw(f.lists[dataType]), nil
}

func (f fakeSource) Latest(_ context.Context, dataType string, _ string) (json.RawMessage, error) {
	if f.failAll || f.failing[dataType] {
		return nil, errors.New("HTTP 503")
	}
	if point, ok := f.latest[dataType]; ok {
		return json.RawMessage(point), nil
	}
	return nil, nil
}

func (f fakeSource) DailyRollUp(_ context.Context, dataType string, _ time.Time, end time.Time) ([]json.RawMessage, error) {
	if f.failAll || f.failing[dataType] {
		return nil, errors.New("HTTP 503")
	}
	if f.rollupEnd != nil {
		*f.rollupEnd = end
	}
	return raw(f.rollups[dataType]), nil
}

func raw(points []string) []json.RawMessage {
	messages := make([]json.RawMessage, 0, len(points))
	for _, point := range points {
		messages = append(messages, json.RawMessage(point))
	}
	return messages
}

func build(t *testing.T, source fakeSource, now time.Time) Report {
	t.Helper()
	t.Chdir(t.TempDir())

	report, err := Build(context.Background(), source, Clock{Now: now})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	return report
}

func date(day int) string {
	return fmt.Sprintf(`{"year":2026,"month":10,"day":%d}`, day)
}

func restingPoint(day int, bpm int) string {
	return fmt.Sprintf(`{"dailyRestingHeartRate":{"date":%s,"beatsPerMinute":"%d"}}`, date(day), bpm)
}

func stepsPoint(day int, steps int) string {
	return fmt.Sprintf(`{"civilStartTime":{"date":%s,"time":{}},"steps":{"countSum":"%d"}}`, date(day), steps)
}

func sleepPoint(start string, end string, asleep int, main bool) string {
	return fmt.Sprintf(`{"sleep":{"interval":{"startTime":"%s","startUtcOffset":"10800s","endTime":"%s","endUtcOffset":"10800s"},
		"metadata":{"mainSleep":%t},"summary":{"minutesAsleep":"%d","minutesAwake":"16",
		"stagesSummary":[{"type":"DEEP","minutes":"58","count":"5"},{"type":"REM","minutes":"95","count":"8"}]}}}`,
		start, end, main, asleep)
}

func heartRatePoint(physicalTime string, offset string) string {
	return fmt.Sprintf(`{"heartRate":{"sampleTime":{"physicalTime":"%s","utcOffset":"%s"},"beatsPerMinute":"65"}}`, physicalTime, offset)
}

func TestBuildComparesDayWithPreviousDays(t *testing.T) {
	var rollupEnd time.Time
	report := build(t, fakeSource{
		lists: map[string][]string{
			"daily-resting-heart-rate": {restingPoint(31, 60), restingPoint(30, 50), restingPoint(29, 54)},
		},
		rollups: map[string][]string{
			"steps": {stepsPoint(31, 800), stepsPoint(30, 3000), stepsPoint(29, 5000), stepsPoint(10, 9000)},
		},
		rollupEnd: &rollupEnd,
	}, testNow)

	resting := report.Recovery["resting_hr_bpm"]
	if *resting.Value != 60 {
		t.Fatalf("resting value = %v, want today's 60", *resting.Value)
	}
	if *resting.Avg7d != 52 {
		t.Fatalf("resting week = %v, want earlier days only", *resting.Avg7d)
	}

	steps := report.Activity["steps"]
	if *steps.Value != 800 {
		t.Fatalf("steps value = %v, want today's 800", *steps.Value)
	}
	if *steps.Avg7d != 4000 {
		t.Fatalf("steps week = %v, want 4000 without days outside the week", *steps.Avg7d)
	}
	if want := time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC); !rollupEnd.Equal(want) {
		t.Fatalf("rollup end = %v, want %v to include today", rollupEnd, want)
	}
}

func TestBuildJustAfterMidnightUsesNewDay(t *testing.T) {
	report := build(t, fakeSource{
		rollups: map[string][]string{"steps": {stepsPoint(31, 6000)}},
	}, time.Date(2026, time.November, 1, 0, 1, 0, 0, time.UTC))

	if report.Date != "2026-11-01" {
		t.Fatalf("Date = %s", report.Date)
	}
	if steps := report.Activity["steps"]; steps.Value != nil {
		t.Fatalf("steps value = %v, want no data yet", *steps.Value)
	}
}

func TestBuildRoundsValues(t *testing.T) {
	point := fmt.Sprintf(`{"dailyHeartRateVariability":{"date":%s,"averageHeartRateVariabilityMilliseconds":198.7512}}`, date(31))
	report := build(t, fakeSource{
		lists: map[string][]string{"daily-heart-rate-variability": {point}},
	}, testNow)

	if got := *report.Recovery["hrv_ms"].Value; got != 198.8 {
		t.Fatalf("hrv = %v, want 198.8", got)
	}
}

func TestBuildCountsMissingZoneDaysAsZero(t *testing.T) {
	point := fmt.Sprintf(
		`{"civilStartTime":{"date":%s,"time":{}},"activeZoneMinutes":{"sumInFatBurnHeartZone":"14","sumInCardioHeartZone":"7"}}`,
		date(29),
	)
	report := build(t, fakeSource{
		rollups: map[string][]string{"active-zone-minutes": {point}},
	}, testNow)

	zones := report.Activity["active_zone_minutes"]
	if *zones.Value != 0 {
		t.Fatalf("zones value = %v, want 0", *zones.Value)
	}
	if *zones.Avg7d != 3 {
		t.Fatalf("zones week = %v, want 21 minutes over 7 days", *zones.Avg7d)
	}
}

func TestBuildUsesMainSleepOfLastNight(t *testing.T) {
	report := build(t, fakeSource{
		lists: map[string][]string{"sleep": {
			sleepPoint("2026-10-30T21:52:00Z", "2026-10-31T06:00:00Z", 472, true),
			sleepPoint("2026-10-31T08:00:00Z", "2026-10-31T08:30:00Z", 30, false),
			sleepPoint("2026-10-29T21:50:00Z", "2026-10-30T05:52:00Z", 480, true),
		}},
	}, testNow)

	night := report.LastNight
	if got := night.StartedAt.Format(time.RFC3339); got != "2026-10-31T00:52:00+03:00" {
		t.Fatalf("StartedAt = %s, want the local time", got)
	}
	if *night.AsleepHours.Value != 7.9 {
		t.Fatalf("asleep = %v hours, want 7.9 without the nap", *night.AsleepHours.Value)
	}
	if *night.DeepMinutes.Value != 58 || *night.RemMinutes.Value != 95 {
		t.Fatalf("stages = %v / %v", *night.DeepMinutes.Value, *night.RemMinutes.Value)
	}
}

func TestBuildLoadsTodaysWorkouts(t *testing.T) {
	workout := `{"exercise":{"interval":{"startTime":"%s","startUtcOffset":"10800s","endTime":"2026-10-31T08:45:00.600Z","endUtcOffset":"10800s"},
		"exerciseType":"WALKING","displayName":"Ходьба","notes":"присед 3x5","activeDuration":"2820s",
		"metricsSummary":{"caloriesKcal":358.4,"steps":"4468","distanceMillimeters":3227800,"averagePaceSecondsPerMeter":0.8696,
		"averageHeartRateBeatsPerMinute":"95","activeZoneMinutes":"12",
		"heartRateZoneDurations":{"lightTime":"2820s","moderateTime":"90s","vigorousTime":"0s","peakTime":"0s"}}}}`
	filters := map[string]string{}
	report := build(t, fakeSource{
		lists: map[string][]string{"exercise": {
			fmt.Sprintf(workout, "2026-10-31T07:57:47.600Z"),
			fmt.Sprintf(workout, "2026-10-30T22:30:00Z"),
		}},
		filters: filters,
	}, testNow)

	wantFilter := `exercise.interval.civil_start_time >= "2026-10-31T00:00:00" AND ` +
		`exercise.interval.civil_start_time < "2026-11-01T00:00:00"`
	if filters["exercise"] != wantFilter {
		t.Fatalf("filter = %s, want %s", filters["exercise"], wantFilter)
	}
	if len(report.WorkoutsToday) != 2 {
		t.Fatalf("WorkoutsToday = %+v, want 2", report.WorkoutsToday)
	}
	first := report.WorkoutsToday[0]
	if first.Name != "Ходьба" || first.Type != "WALKING" || first.Notes != "присед 3x5" {
		t.Fatalf("workout = %+v", first)
	}
	if got := first.EndedAt.Format(time.RFC3339); got != "2026-10-31T11:45:00+03:00" {
		t.Fatalf("EndedAt = %s", got)
	}
	if first.Minutes != 47 || first.CaloriesKcal != 358 || first.Steps != 4468 || first.DistanceKm != 3.2 {
		t.Fatalf("workout = %+v", first)
	}
	if first.AvgPaceMinPerKm != 14.5 || first.AvgHRBpm != 95 || first.ActiveZoneMinutes != 12 {
		t.Fatalf("workout = %+v", first)
	}
	if want := (HRZoneMinutes{Light: 47, Moderate: 2}); *first.HRZoneMinutes != want {
		t.Fatalf("HRZoneMinutes = %+v, want %+v", *first.HRZoneMinutes, want)
	}
}

func TestWorkoutJSONOmitsMissingFields(t *testing.T) {
	workout, err := decodeWorkout(json.RawMessage(`{"exercise":{
		"interval":{"startTime":"2026-10-31T07:00:00Z","startUtcOffset":"10800s","endTime":"2026-10-31T08:00:00Z","endUtcOffset":"10800s"},
		"exerciseType":"WEIGHTLIFTING","displayName":"Силовая","activeDuration":"3600s","metricsSummary":{}}}`))
	if err != nil {
		t.Fatalf("decodeWorkout() error = %v", err)
	}

	encoded, err := json.Marshal(workout)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	want := `{"name":"Силовая","type":"WEIGHTLIFTING","started_at":"2026-10-31T10:00:00+03:00",` +
		`"ended_at":"2026-10-31T11:00:00+03:00","minutes":60}`
	if string(encoded) != want {
		t.Fatalf("JSON = %s, want %s", encoded, want)
	}
}

func TestDecodeWorkoutRejectsBadZoneDuration(t *testing.T) {
	_, err := decodeWorkout(json.RawMessage(`{"exercise":{
		"interval":{"startTime":"2026-10-31T07:00:00Z","startUtcOffset":"10800s","endTime":"2026-10-31T08:00:00Z","endUtcOffset":"10800s"},
		"metricsSummary":{"heartRateZoneDurations":{"lightTime":"oops"}}}}`))
	if err == nil {
		t.Fatal("decodeWorkout() error = nil, want a duration error")
	}
}

func TestBuildListsUnavailableMetrics(t *testing.T) {
	report := build(t, fakeSource{
		failing: map[string]bool{"daily-respiratory-rate": true, "sleep": true, "exercise": true},
	}, testNow)

	if want := []string{"breaths_per_minute", "sleep", "workouts"}; !slices.Equal(report.Unavailable, want) {
		t.Fatalf("Unavailable = %v, want %v", report.Unavailable, want)
	}
	if _, ok := report.Recovery["breaths_per_minute"]; !ok {
		t.Fatal("failed metric must stay in the report with null values")
	}
	if report.WorkoutsToday != nil {
		t.Fatalf("failed workouts = %+v, want null rather than empty", report.WorkoutsToday)
	}
}

func TestBuildFailsWhenEverythingFails(t *testing.T) {
	t.Chdir(t.TempDir())

	if _, err := Build(context.Background(), fakeSource{failAll: true}, Clock{Now: testNow}); err == nil {
		t.Fatal("Build() error = nil, want an error so a scheduled run does not send an empty report")
	}
}

func TestReportJSON(t *testing.T) {
	report := build(t, fakeSource{
		rollups: map[string][]string{"steps": {stepsPoint(31, 800)}},
	}, testNow)

	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	text := string(encoded)

	for _, want := range []string{
		`"date":"2026-10-31"`,
		`"generated_at":"2026-10-31T22:30:15Z"`,
		`"last_data_at":null`,
		`"unavailable":[]`,
		`"steps":{"value":800,"avg_7d":null}`,
		`"started_at":null`,
		`"workouts_today":[]`,
		`"note":null`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("JSON has no %s:\n%s", want, text)
		}
	}
}

func TestDeviceClockUsesZoneOfNewestPoint(t *testing.T) {
	now := time.Date(2026, time.October, 31, 22, 1, 0, 0, time.UTC)

	clock := DeviceClock(context.Background(), fakeSource{
		latest: map[string]string{"heart-rate": heartRatePoint("2026-10-31T21:50:00Z", "10800s")},
	}, now)

	if got := clock.Now.Format("2006-01-02 15:04"); got != "2026-11-01 01:01" {
		t.Fatalf("Now = %s, want the band's local time", got)
	}
	if got := clock.LastData.Format(time.RFC3339); got != "2026-11-01T00:50:00+03:00" {
		t.Fatalf("LastData = %s", got)
	}
}

func TestDeviceClockFallsBackToGivenTime(t *testing.T) {
	tests := []struct {
		name   string
		source fakeSource
	}{
		{name: "request fails", source: fakeSource{failAll: true}},
		{name: "no recent points", source: fakeSource{}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clock := DeviceClock(context.Background(), test.source, testNow)
			if !clock.Now.Equal(testNow) || clock.Now.Location() != testNow.Location() {
				t.Fatalf("Now = %v, want %v", clock.Now, testNow)
			}
			if clock.LastData != nil {
				t.Fatalf("LastData = %v, want nil", clock.LastData)
			}
		})
	}
}

func TestBuildIncludesNote(t *testing.T) {
	t.Chdir(t.TempDir())

	if err := SaveNote(testNow, "болит колено"); err != nil {
		t.Fatalf("SaveNote() error = %v", err)
	}
	report, err := Build(context.Background(), fakeSource{}, Clock{Now: testNow})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if report.Note == nil || *report.Note != "болит колено" {
		t.Fatalf("Note = %v", report.Note)
	}
}

func TestNotes(t *testing.T) {
	t.Chdir(t.TempDir())
	day := time.Date(2026, time.October, 31, 0, 0, 0, 0, time.UTC)

	note, err := LoadNote(day)
	if err != nil || note != "" {
		t.Fatalf("LoadNote() = %q, %v, want empty", note, err)
	}

	for _, line := range []string{"болит колено", "спал плохо"} {
		if err := SaveNote(day, line); err != nil {
			t.Fatalf("SaveNote() error = %v", err)
		}
	}

	note, err = LoadNote(day)
	if err != nil {
		t.Fatalf("LoadNote() error = %v", err)
	}
	if note != "болит колено; спал плохо" {
		t.Fatalf("LoadNote() = %q", note)
	}
}
