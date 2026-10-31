package report

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

var testNow = time.Date(2026, time.October, 31, 11, 0, 0, 0, time.UTC)

type fakeSource struct {
	lists     map[string][]string
	rollups   map[string][]string
	failing   map[string]bool
	failAll   bool
	rollupEnd *time.Time
}

func (f fakeSource) List(_ context.Context, dataType string, _ string) ([]json.RawMessage, error) {
	if f.failAll || f.failing[dataType] {
		return nil, errors.New("HTTP 503")
	}
	return raw(f.lists[dataType]), nil
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

func TestBuildComparesDayWithPreviousDays(t *testing.T) {
	t.Chdir(t.TempDir())

	var rollupEnd time.Time
	report, err := Build(context.Background(), fakeSource{
		lists: map[string][]string{
			"daily-resting-heart-rate": {restingPoint(31, 60), restingPoint(30, 50), restingPoint(29, 54)},
		},
		rollups: map[string][]string{
			"steps": {stepsPoint(31, 800), stepsPoint(30, 3000), stepsPoint(10, 5000)},
		},
		rollupEnd: &rollupEnd,
	}, testNow)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	resting := report.Recovery[0]
	if *resting.Value != 60 {
		t.Fatalf("resting value = %v, want today's 60", *resting.Value)
	}
	if resting.Week.Value != 52 || resting.Week.Days != 2 {
		t.Fatalf("resting week = %+v, want average of earlier days only", resting.Week)
	}

	steps := report.Activity[0]
	if *steps.Value != 800 {
		t.Fatalf("steps value = %v, want today's 800", *steps.Value)
	}
	if steps.Week.Value != 3000 || steps.Month.Value != 4000 {
		t.Fatalf("steps averages = %+v / %+v", steps.Week, steps.Month)
	}
	if want := time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC); !rollupEnd.Equal(want) {
		t.Fatalf("rollup end = %v, want %v to include today", rollupEnd, want)
	}
}

func TestBuildJustAfterMidnightUsesNewDay(t *testing.T) {
	t.Chdir(t.TempDir())

	now := time.Date(2026, time.November, 1, 0, 1, 0, 0, time.UTC)
	report, err := Build(context.Background(), fakeSource{
		rollups: map[string][]string{"steps": {stepsPoint(31, 6000)}},
	}, now)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	steps := report.Activity[0]
	if steps.Value != nil {
		t.Fatalf("steps value = %v, want no data yet", *steps.Value)
	}
	if !strings.HasPrefix(Render(report), "Отчёт за 01.11.2026 на 00:01\n") {
		t.Fatalf("Render() =\n%s", Render(report))
	}
}

func TestDeviceNowUsesZoneOfNewestPoint(t *testing.T) {
	now := time.Date(2026, time.October, 31, 22, 1, 0, 0, time.UTC)
	step := `{"steps":{"interval":{"startTime":"%s","startUtcOffset":"%s"}}}`

	device := DeviceNow(context.Background(), fakeSource{
		lists: map[string][]string{"steps": {
			fmt.Sprintf(step, "2026-10-31T21:50:00Z", "10800s"),
			fmt.Sprintf(step, "2026-10-31T02:00:00Z", "7200s"),
		}},
	}, now)

	if got := device.Format("2006-01-02 15:04"); got != "2026-11-01 01:01" {
		t.Fatalf("DeviceNow() = %s, want the band's local time", got)
	}
}

func TestDeviceNowFallsBackToGivenTime(t *testing.T) {
	tests := []struct {
		name   string
		source fakeSource
	}{
		{name: "request fails", source: fakeSource{failAll: true}},
		{name: "no recent points", source: fakeSource{}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := DeviceNow(context.Background(), test.source, testNow); !got.Equal(testNow) || got.Location() != testNow.Location() {
				t.Fatalf("DeviceNow() = %v, want %v", got, testNow)
			}
		})
	}
}

func TestBuildCountsMissingZoneDaysAsZero(t *testing.T) {
	t.Chdir(t.TempDir())

	point := fmt.Sprintf(
		`{"civilStartTime":{"date":%s,"time":{}},"activeZoneMinutes":{"sumInFatBurnHeartZone":"14","sumInCardioHeartZone":"7"}}`,
		date(29),
	)
	report, err := Build(context.Background(), fakeSource{
		rollups: map[string][]string{"active-zone-minutes": {point}},
	}, testNow)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	zones := report.Activity[2]
	if *zones.Value != 0 {
		t.Fatalf("zones value = %v, want 0", *zones.Value)
	}
	if zones.Week.Value != 3 || zones.Week.Days != 7 {
		t.Fatalf("zones week = %+v, want 21 minutes over 7 days", zones.Week)
	}
}

func TestBuildUsesMainSleepOfLastNight(t *testing.T) {
	t.Chdir(t.TempDir())

	report, err := Build(context.Background(), fakeSource{
		lists: map[string][]string{"sleep": {
			sleepPoint("2026-10-30T21:52:00Z", "2026-10-31T06:00:00Z", 472, true),
			sleepPoint("2026-10-31T08:00:00Z", "2026-10-31T08:30:00Z", 30, false),
			sleepPoint("2026-10-29T21:50:00Z", "2026-10-30T05:52:00Z", 480, true),
		}},
	}, testNow)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	if got := report.SleepStart.Format("15:04"); got != "00:52" {
		t.Fatalf("SleepStart = %s, want local 00:52", got)
	}
	asleep := report.Sleep[0]
	if got := fmt.Sprintf("%.2f", *asleep.Value); got != "7.87" {
		t.Fatalf("asleep = %s hours, want 7.87 without the nap", got)
	}
	if *report.Sleep[1].Value != 58 || *report.Sleep[2].Value != 95 {
		t.Fatalf("stages = %v / %v", *report.Sleep[1].Value, *report.Sleep[2].Value)
	}
}

func TestBuildSplitsWorkoutsByDay(t *testing.T) {
	t.Chdir(t.TempDir())

	workout := `{"exercise":{"interval":{"startTime":"%s","startUtcOffset":"10800s"},"displayName":"Ходьба",
		"activeDuration":"2820s","metricsSummary":{"distanceMillimeters":3227800,"averageHeartRateBeatsPerMinute":"95"}}}`
	report, err := Build(context.Background(), fakeSource{
		lists: map[string][]string{"exercise": {
			fmt.Sprintf(workout, "2026-10-31T07:57:47.600Z"),
			fmt.Sprintf(workout, "2026-10-30T22:30:00Z"),
			fmt.Sprintf(workout, "2026-10-21T17:00:00Z"),
		}},
	}, testNow)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	if len(report.Workouts) != 2 {
		t.Fatalf("Workouts = %+v, want 2 on the local day", report.Workouts)
	}
	if got := formatWorkout(report.Workouts[0]); got != "Ходьба: 47 мин, 3.2 км, ср. пульс 95" {
		t.Fatalf("formatWorkout() = %q", got)
	}
	if report.WeekTotal.Count != 0 || report.MonthTotal.Count != 1 {
		t.Fatalf("totals = %+v / %+v", report.WeekTotal, report.MonthTotal)
	}
}

func TestBuildListsUnavailableMetrics(t *testing.T) {
	t.Chdir(t.TempDir())

	report, err := Build(context.Background(), fakeSource{
		failing: map[string]bool{"daily-respiratory-rate": true, "sleep": true},
	}, testNow)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	text := Render(report)
	if !strings.Contains(text, "Не удалось получить: Дыхание, вд/мин, Сон") {
		t.Fatalf("Render() =\n%s", text)
	}
	if len(report.Sleep) != len(sleepMetrics) {
		t.Fatalf("Sleep rows = %d, want %d", len(report.Sleep), len(sleepMetrics))
	}
}

func TestBuildFailsWhenEverythingFails(t *testing.T) {
	t.Chdir(t.TempDir())

	if _, err := Build(context.Background(), fakeSource{failAll: true}, testNow); err == nil {
		t.Fatal("Build() error = nil, want an error so a scheduled run does not send an empty report")
	}
}

func TestRender(t *testing.T) {
	value := 54.0
	text := Render(Report{
		Day:         time.Date(2026, time.October, 31, 0, 0, 0, 0, time.UTC),
		GeneratedAt: testNow,
		Recovery: []Row{{
			Label: "Пульс покоя, уд/мин",
			Value: &value,
			Week:  Average{Value: 55.4, Days: 7, Window: 7},
			Month: Average{Value: 55.1, Days: 10, Window: 28},
		}, {
			Label: "SpO2, %", Digits: 1, Week: Average{Window: 7}, Month: Average{Window: 28},
		}},
		Note: "плохо спал",
	})

	want := "" +
		"Отчёт за 31.10.2026 на 11:00\n" +
		"Нагрузка с 00:00 до этого времени, сон и восстановление за ночь на 31.10\n" +
		"\n" +
		"ВОССТАНОВЛЕНИЕ           сегодня     ср. 7 дн.    ср. 28 дн.\n" +
		"Пульс покоя, уд/мин           54            55   55 (10 дн.)\n" +
		"SpO2, %                        —             —             —\n"
	if !strings.HasPrefix(text, want) {
		t.Fatalf("Render() =\n%s\nwant prefix\n%s", text, want)
	}
	if !strings.Contains(text, "Тренировки за 31.10: нет\n") {
		t.Fatalf("Render() =\n%s", text)
	}
	if !strings.HasSuffix(text, "Самочувствие: плохо спал\n") {
		t.Fatalf("Render() =\n%s", text)
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
