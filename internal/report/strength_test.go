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
	"fitbit-export/internal/hevy"
	"fitbit-export/internal/telegram"
)

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
	return buildStrengthFull(t, source, nutrition, Hevy{})
}

func buildStrengthFull(t *testing.T, source fakeSource, nutrition NutritionSource, workouts Hevy) StrengthLog {
	t.Helper()
	return BuildStrengthLog(context.Background(), source, nutrition, workouts, Clock{Now: testNow})
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
	card    hevy.Card
	cardErr error
	pages   map[string]string
	opened  *[]string
}

func (f fakePages) Latest(context.Context, string) (hevy.Card, error) {
	return f.card, f.cardErr
}

func (f fakePages) Workout(_ context.Context, link string) (string, error) {
	*f.opened = append(*f.opened, link)
	if notes, ok := f.pages[link]; ok {
		return notes, nil
	}
	return "", errors.New("chrome timed out")
}

func hevyPage(started string, exercise string) string {
	return "danilpapa\n" + started + "\nтренировка\nПродолжительность: 1 ч\n\n" + exercise + "\nСет 1: 80 kg x 8"
}

func command(hour int, text string) telegram.Message {
	return telegram.Message{SentAt: time.Date(2026, time.October, 31, hour, 0, 0, 0, time.UTC), Text: text}
}

func TestBuildStrengthLogReadsTodaysWorkoutFromProfile(t *testing.T) {
	const link = "https://hevy.com/workout/today"
	var opened []string
	log := buildStrengthFull(t, fakeSource{}, nil, Hevy{
		User: "danilpapa",
		Pages: fakePages{
			card:   hevy.Card{Link: link, Title: "тренировка", Description: "болело плечо", StartedAt: time.Date(2026, time.October, 31, 18, 0, 0, 0, time.UTC)},
			pages:  map[string]string{link: hevyPage("31 окт. 2026 г., 18:00", "Жим лежа (Штанга)")},
			opened: &opened,
		},
	})

	if len(log.Workouts) != 1 {
		t.Fatalf("Workouts = %+v", log.Workouts)
	}
	workout := log.Workouts[0]
	if workout.Source != "profile" || workout.Link != link || workout.StartedAt.Hour() != 18 {
		t.Fatalf("workout = %+v", workout)
	}
	if !strings.Contains(workout.Notes, "тренировка\nОписание: болело плечо\n") || !strings.Contains(workout.Notes, "Сет 1: 80 kg x 8") {
		t.Fatalf("notes = %q, want the sets and the description", workout.Notes)
	}
	if len(log.Unavailable) != 0 {
		t.Fatalf("Unavailable = %v", log.Unavailable)
	}
}

func TestBuildStrengthLogSkipsOlderProfileWorkout(t *testing.T) {
	var opened []string
	log := buildStrengthFull(t, fakeSource{}, nil, Hevy{
		User: "danilpapa",
		Pages: fakePages{
			card:   hevy.Card{Link: "https://hevy.com/workout/old", StartedAt: time.Date(2026, time.October, 30, 23, 0, 0, 0, time.UTC)},
			opened: &opened,
		},
	})

	if len(log.Workouts) != 0 || len(opened) != 0 || len(log.Unavailable) != 0 {
		t.Fatalf("Workouts = %+v, opened %v, Unavailable = %v, want no workout today and no error", log.Workouts, opened, log.Unavailable)
	}
}

func TestBuildStrengthLogReadsWorkoutThatEndedToday(t *testing.T) {
	const link = "https://hevy.com/workout/fresh"
	var opened []string
	log := buildStrengthFull(t, fakeSource{}, nil, Hevy{
		User: "danilpapa",
		Pages: fakePages{
			card:   hevy.Card{Link: link, Date: "3 часа назад", EndedToday: true},
			pages:  map[string]string{link: hevyPage("3 часа назад", "Жим лежа (Штанга)")},
			opened: &opened,
		},
	})

	if len(log.Workouts) != 1 || log.Workouts[0].StartedAt != nil || log.Unavailable == nil || len(log.Unavailable) != 0 {
		t.Fatalf("Workouts = %+v, Unavailable = %v, want today's workout without a start time", log.Workouts, log.Unavailable)
	}
}

func TestBuildStrengthLogAddsAdditionalTraining(t *testing.T) {
	const (
		profile = "https://hevy.com/workout/evening"
		morning = "https://hevy.com/workout/morning"
		copied  = "https://hevy.com/workout/d657850b-45c1-4ea8-8b89-03339df7dd38"
	)
	var opened []string
	log := buildStrengthFull(t, fakeSource{}, nil, Hevy{
		User: "danilpapa",
		Pages: fakePages{
			card: hevy.Card{Link: profile, StartedAt: time.Date(2026, time.October, 31, 19, 0, 0, 0, time.UTC)},
			pages: map[string]string{
				profile: hevyPage("31 окт. 2026 г., 19:00", "Присед (Штанга)"),
				morning: hevyPage("31 окт. 2026 г., 9:30", "Подтягивания"),
				// The same evening workout under the link copied in the app.
				copied: hevyPage("31 окт. 2026 г., 19:00", "Присед (Штанга)"),
			},
			opened: &opened,
		},
		Inbox: fakeMessages{messages: []telegram.Message{
			command(10, "/additionalTraining "+morning),
			command(11, "/additionalTraining утром ещё "+morning),
			command(12, "/additionalTraining без ссылки"),
			command(20, "/additionalTraining "+copied),
			command(21, "/additionalTraining https://hevy.com/workout/broken"),
			command(22, "/additionalTraining "+profile),
		}},
	})

	if len(log.Workouts) != 2 {
		t.Fatalf("Workouts = %+v, want the morning and the evening workout once each", log.Workouts)
	}
	if log.Workouts[0].Link != morning || log.Workouts[0].Source != "additional" || log.Workouts[0].StartedAt.Format("15:04") != "09:30" {
		t.Fatalf("first workout = %+v, want the morning one", log.Workouts[0])
	}
	if log.Workouts[1].Link != profile {
		t.Fatalf("second workout = %+v, want the profile one", log.Workouts[1])
	}
	if !slices.Equal(opened, []string{profile, morning, copied, "https://hevy.com/workout/broken"}) {
		t.Fatalf("opened %v, want each link once and not the profile one again", opened)
	}
	if want := []string{"hevy"}; !slices.Equal(log.Unavailable, want) {
		t.Fatalf("Unavailable = %v, want %v for the broken link", log.Unavailable, want)
	}
}

func TestBuildStrengthLogKeepsAdditionalTrainingWhenProfileFails(t *testing.T) {
	const link = "https://hevy.com/workout/extra"
	var opened []string
	log := buildStrengthFull(t, fakeSource{}, nil, Hevy{
		User: "danilpapa",
		Pages: fakePages{
			cardErr: errors.New("the page shows no workouts"),
			pages:   map[string]string{link: hevyPage("31 окт. 2026 г., 12:00", "Жим лежа (Штанга)")},
			opened:  &opened,
		},
		Inbox: fakeMessages{messages: []telegram.Message{command(13, "/additionalTraining "+link)}},
	})

	if len(log.Workouts) != 1 || log.Workouts[0].Link != link {
		t.Fatalf("Workouts = %+v", log.Workouts)
	}
	if want := []string{"hevy"}; !slices.Equal(log.Unavailable, want) {
		t.Fatalf("Unavailable = %v, want %v", log.Unavailable, want)
	}
}

func TestBuildStrengthLogMarksInboxFailure(t *testing.T) {
	var opened []string
	log := buildStrengthFull(t, fakeSource{}, nil, Hevy{
		Pages: fakePages{opened: &opened},
		Inbox: fakeMessages{err: errors.New("HTTP 401")},
	})

	if want := []string{"inbox"}; !slices.Equal(log.Unavailable, want) {
		t.Fatalf("Unavailable = %v, want %v", log.Unavailable, want)
	}
}

func TestBuildStrengthLogKeepsWorkoutsWithoutBodyData(t *testing.T) {
	const link = "https://hevy.com/workout/today"
	var opened []string
	log := buildStrengthFull(t, fakeSource{
		failing: map[string]bool{"weight": true, "nutrition-log": true},
	}, nil, Hevy{
		User: "danilpapa",
		Pages: fakePages{
			card:   hevy.Card{Link: link, StartedAt: time.Date(2026, time.October, 31, 18, 0, 0, 0, time.UTC)},
			pages:  map[string]string{link: hevyPage("31 окт. 2026 г., 18:00", "Присед (Штанга)")},
			opened: &opened,
		},
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

func TestStrengthLogJSON(t *testing.T) {
	encoded, err := json.Marshal(buildStrength(t, fakeSource{}))
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	text := string(encoded)

	for _, want := range []string{
		`"unavailable":[]`,
		`"workouts":[]`,
		`"generated_at":`,
		`"body":{"weight_kg":{"value":null,"avg_7d":null},"weighed_at":null}`,
		`"calories_kcal":{"value":null,"avg_7d":null}`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("JSON has no %s:\n%s", want, text)
		}
	}
}
