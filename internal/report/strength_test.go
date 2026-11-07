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

	"fitbit-export/internal/fatsecret"
)

func strengthPoint(start string, exerciseType string, notes string) string {
	return fmt.Sprintf(`{"exercise":{"interval":{"startTime":"%s","startUtcOffset":"10800s","endTime":"%s","endUtcOffset":"10800s"},
		"exerciseType":"%s","displayName":"Силовая","notes":%q,"activeDuration":"3600s","metricsSummary":{}}}`,
		start, start, exerciseType, notes)
}

func weightPoint(physicalTime string, grams int) string {
	return fmt.Sprintf(`{"weight":{"sampleTime":{"physicalTime":"%s","utcOffset":"10800s"},"weightGrams":%d}}`, physicalTime, grams)
}

func nutritionPoint(day int, kcal float64, protein float64) string {
	return fmt.Sprintf(`{"civilStartTime":{"date":%s,"time":{}},"nutritionLog":{"energy":{"kcalSum":%v},
		"totalCarbohydrate":{"gramsSum":250.4},"totalFat":{"gramsSum":70},
		"nutrients":[{"nutrient":"SUGAR","quantity":{"gramsSum":40}},{"nutrient":"PROTEIN","quantity":{"gramsSum":%v}}]}}`,
		date(day), kcal, protein)
}

type fakeNutrition struct {
	days  []fatsecret.Day
	err   error
	start *time.Time
	end   *time.Time
}

func (f fakeNutrition) DailyTotals(_ context.Context, start time.Time, end time.Time) ([]fatsecret.Day, error) {
	if f.start != nil {
		*f.start, *f.end = start, end
	}
	return f.days, f.err
}

func buildStrength(t *testing.T, source fakeSource) StrengthLog {
	t.Helper()
	return buildStrengthWith(t, source, nil)
}

func buildStrengthWith(t *testing.T, source fakeSource, nutrition NutritionSource) StrengthLog {
	t.Helper()
	return buildStrengthFull(t, source, nutrition, nil)
}

func buildStrengthFull(t *testing.T, source fakeSource, nutrition NutritionSource, pages WorkoutPages) StrengthLog {
	t.Helper()

	log, err := BuildStrengthLog(context.Background(), source, nutrition, pages, Clock{Now: testNow}, StrengthHistoryDays)
	if err != nil {
		t.Fatalf("BuildStrengthLog() error = %v", err)
	}
	return log
}

func TestBuildStrengthLogKeepsLoggedStrengthWorkouts(t *testing.T) {
	filters := map[string]string{}
	log := buildStrength(t, fakeSource{
		lists: map[string][]string{"exercise": {
			strengthPoint("2026-10-30T15:00:00Z", "POWERLIFTING", "Присед\nСет 1: 100 kg x 5"),
			strengthPoint("2026-10-20T15:00:00Z", "STRENGTH_TRAINING", "жим 80 на 8"),
			strengthPoint("2026-10-25T15:00:00Z", "POWERLIFTING", ""),
			strengthPoint("2026-10-26T15:00:00Z", "WALKING", "тест"),
		}},
		filters: filters,
	})

	if len(log.Workouts) != 2 {
		t.Fatalf("Workouts = %+v, want only strength workouts with text", log.Workouts)
	}
	if log.Workouts[0].Notes != "жим 80 на 8" || log.Workouts[1].Notes != "Присед\nСет 1: 100 kg x 5" {
		t.Fatalf("Workouts = %+v, want the oldest first", log.Workouts)
	}

	want := `exercise.interval.civil_start_time >= "2026-08-03T00:00:00" AND ` +
		`exercise.interval.civil_start_time < "2026-11-01T00:00:00"`
	if filters["exercise"] != want {
		t.Fatalf("filter = %s, want %s", filters["exercise"], want)
	}
}

func TestBuildStrengthLogKeepsRecentWorkouts(t *testing.T) {
	var points []string
	for day := 1; day <= maxStrengthWorkouts+3; day++ {
		start := fmt.Sprintf("2026-10-%02dT15:00:00Z", day)
		points = append(points, strengthPoint(start, "POWERLIFTING", fmt.Sprintf("день %d", day)))
	}

	log := buildStrength(t, fakeSource{lists: map[string][]string{"exercise": points}})

	if len(log.Workouts) != maxStrengthWorkouts {
		t.Fatalf("Workouts = %d, want %d", len(log.Workouts), maxStrengthWorkouts)
	}
	if got := log.Workouts[0].Notes; got != "день 4" {
		t.Fatalf("first workout = %s, want the oldest kept one", got)
	}
}

func TestBuildStrengthLogUsesLatestWeighIn(t *testing.T) {
	filters := map[string]string{}
	log := buildStrength(t, fakeSource{
		lists: map[string][]string{"weight": {
			weightPoint("2026-10-25T06:00:00Z", 78000),
			weightPoint("2026-10-30T06:00:00.5Z", 77040),
			weightPoint("2026-10-28T06:00:00Z", 77000),
		}},
		filters: filters,
	})

	body := log.Body
	if *body.WeightKg.Value != 77 {
		t.Fatalf("weight = %v, want the newest weigh-in", *body.WeightKg.Value)
	}
	if *body.WeightKg.Avg7d != 77.3 {
		t.Fatalf("weight week = %v, want 77.3", *body.WeightKg.Avg7d)
	}
	if got := body.WeighedAt.Format(time.RFC3339); got != "2026-10-30T09:00:00+03:00" {
		t.Fatalf("WeighedAt = %s", got)
	}

	want := `weight.sample_time.civil_time >= "2026-10-01T00:00:00" AND ` +
		`weight.sample_time.civil_time < "2026-11-01T00:00:00"`
	if filters["weight"] != want {
		t.Fatalf("filter = %s, want %s", filters["weight"], want)
	}
}

func TestBuildStrengthLogLoadsNutrition(t *testing.T) {
	log := buildStrength(t, fakeSource{
		rollups: map[string][]string{"nutrition-log": {
			nutritionPoint(31, 2400.4, 150.6),
			nutritionPoint(30, 2000, 120),
			nutritionPoint(29, 2200, 140),
		}},
	})

	calories := log.Nutrition["calories_kcal"]
	if *calories.Value != 2400 || *calories.Avg7d != 2100 {
		t.Fatalf("calories = %v / %v", *calories.Value, *calories.Avg7d)
	}
	if got := *log.Nutrition["protein_g"].Value; got != 151 {
		t.Fatalf("protein = %v, want 151", got)
	}
	if got := *log.Nutrition["carbs_g"].Value; got != 250 {
		t.Fatalf("carbs = %v", got)
	}
	if got := *log.Nutrition["fat_g"].Value; got != 70 {
		t.Fatalf("fat = %v", got)
	}
}

func TestBuildStrengthLogLeavesEmptyNutritionDayUnset(t *testing.T) {
	log := buildStrength(t, fakeSource{
		rollups: map[string][]string{"nutrition-log": {
			fmt.Sprintf(`{"civilStartTime":{"date":%s,"time":{}}}`, date(31)),
			nutritionPoint(30, 2000, 120),
		}},
	})

	calories := log.Nutrition["calories_kcal"]
	if calories.Value != nil {
		t.Fatalf("calories = %v, want no food logged today", *calories.Value)
	}
	if *calories.Avg7d != 2000 {
		t.Fatalf("calories week = %v", *calories.Avg7d)
	}
}

func TestBuildStrengthLogUsesFatSecretNutrition(t *testing.T) {
	var start, end time.Time
	log := buildStrengthWith(t, fakeSource{
		failing: map[string]bool{"nutrition-log": true},
	}, fakeNutrition{
		days: []fatsecret.Day{
			{Date: "2026-10-31", CaloriesKcal: 2400.4, ProteinG: 150.6, CarbsG: 250.4, FatG: 70},
			{Date: "2026-10-30", CaloriesKcal: 2000, ProteinG: 120, CarbsG: 200, FatG: 60},
			{Date: "2026-10-29", CaloriesKcal: 2200, ProteinG: 140, CarbsG: 220, FatG: 80},
		},
		start: &start,
		end:   &end,
	})

	if len(log.Unavailable) != 0 {
		t.Fatalf("Unavailable = %v, want Google nutrition left unused", log.Unavailable)
	}
	calories := log.Nutrition["calories_kcal"]
	if *calories.Value != 2400 || *calories.Avg7d != 2100 {
		t.Fatalf("calories = %v / %v", *calories.Value, *calories.Avg7d)
	}
	if got := *log.Nutrition["protein_g"].Value; got != 151 {
		t.Fatalf("protein = %v, want 151", got)
	}
	if got := *log.Nutrition["fat_g"].Avg7d; got != 70 {
		t.Fatalf("fat week = %v, want 70", got)
	}
	if start.Format(time.DateOnly) != "2026-10-24" || end.Format(time.DateOnly) != "2026-11-01" {
		t.Fatalf("range = %s..%s, want the past week and today", start, end)
	}
}

func TestBuildStrengthLogMarksFatSecretFailure(t *testing.T) {
	log := buildStrengthWith(t, fakeSource{}, fakeNutrition{err: errors.New("FatSecret error 21")})

	if want := []string{"nutrition"}; !slices.Equal(log.Unavailable, want) {
		t.Fatalf("Unavailable = %v, want %v", log.Unavailable, want)
	}
}

type fakePages struct {
	sets   map[string]string
	opened *[]string
}

func (f fakePages) Workout(_ context.Context, link string) (string, error) {
	*f.opened = append(*f.opened, link)
	if sets, ok := f.sets[link]; ok {
		return sets, nil
	}
	return "", errors.New("chrome timed out")
}

func TestBuildStrengthLogReadsHevyPagesForLinksWithoutSets(t *testing.T) {
	const (
		bare   = "https://hevy.com/workout/bare"
		filled = "https://hevy.com/workout/filled"
	)
	var opened []string
	log := buildStrengthFull(t, fakeSource{
		lists: map[string][]string{"exercise": {
			strengthPoint("2026-10-30T15:00:00Z", "POWERLIFTING", "спина\n@hevyapp\n"+bare),
			strengthPoint("2026-10-31T15:00:00Z", "POWERLIFTING", "Жим лежа\nСет 1: 80 kg x 12\n"+filled),
		}},
	}, nil, fakePages{
		sets:   map[string]string{bare: "Подтягивания\nСет 1: 25 kg x 11"},
		opened: &opened,
	})

	if !slices.Equal(opened, []string{bare}) {
		t.Fatalf("opened %v, want only the link without sets", opened)
	}
	if got, want := log.Workouts[0].Notes, "спина\n@hevyapp\nПодтягивания\nСет 1: 25 kg x 11"; got != want {
		t.Fatalf("notes = %q, want %q", got, want)
	}
	if got := log.Workouts[1].Notes; got != "Жим лежа\nСет 1: 80 kg x 12\n"+filled {
		t.Fatalf("notes with sets changed: %q", got)
	}
	if len(log.Unavailable) != 0 {
		t.Fatalf("Unavailable = %v", log.Unavailable)
	}
}

func TestBuildStrengthLogKeepsLinkWhenHevyFails(t *testing.T) {
	const link = "https://hevy.com/workout/broken"
	var opened []string
	log := buildStrengthFull(t, fakeSource{
		lists: map[string][]string{"exercise": {
			strengthPoint("2026-10-30T15:00:00Z", "POWERLIFTING", link),
			strengthPoint("2026-10-31T15:00:00Z", "POWERLIFTING", "https://hevy.com/workout/broken2"),
		}},
	}, nil, fakePages{opened: &opened})

	if want := []string{"hevy"}; !slices.Equal(log.Unavailable, want) {
		t.Fatalf("Unavailable = %v, want %v once", log.Unavailable, want)
	}
	if log.Workouts[0].Notes != link {
		t.Fatalf("notes = %q, want the link kept", log.Workouts[0].Notes)
	}
}

func TestBuildStrengthLogKeepsWorkoutsWithoutBodyData(t *testing.T) {
	log := buildStrength(t, fakeSource{
		failing: map[string]bool{"weight": true, "nutrition-log": true},
		lists: map[string][]string{"exercise": {
			strengthPoint("2026-10-30T15:00:00Z", "POWERLIFTING", "Присед"),
		}},
	})

	if want := []string{"weight", "nutrition"}; !slices.Equal(log.Unavailable, want) {
		t.Fatalf("Unavailable = %v, want %v", log.Unavailable, want)
	}
	if len(log.Workouts) != 1 {
		t.Fatalf("Workouts = %+v", log.Workouts)
	}
	if metric, ok := log.Nutrition["protein_g"]; !ok || metric.Value != nil {
		t.Fatalf("protein = %+v, want a null metric", metric)
	}
}

func TestBuildStrengthLogReturnsWorkoutError(t *testing.T) {
	_, err := BuildStrengthLog(context.Background(), fakeSource{failAll: true}, nil, nil, Clock{Now: testNow}, StrengthHistoryDays)
	if err == nil {
		t.Fatal("BuildStrengthLog() error = nil")
	}
}

func TestStrengthLogJSON(t *testing.T) {
	encoded, err := json.Marshal(buildStrength(t, fakeSource{}))
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	text := string(encoded)

	for _, want := range []string{
		`"unavailable":[]`,
		`"workouts":[]`,
		`"body":{"weight_kg":{"value":null,"avg_7d":null},"weighed_at":null}`,
		`"calories_kcal":{"value":null,"avg_7d":null}`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("JSON has no %s:\n%s", want, text)
		}
	}
}
