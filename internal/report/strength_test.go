package report

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
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

func buildStrength(t *testing.T, source fakeSource) StrengthLog {
	t.Helper()

	log, err := BuildStrengthLog(context.Background(), source, Clock{Now: testNow}, StrengthHistoryDays)
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
	_, err := BuildStrengthLog(context.Background(), fakeSource{failAll: true}, Clock{Now: testNow}, StrengthHistoryDays)
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
