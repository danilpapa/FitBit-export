package report

import (
	"context"
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	"fitbit-export/internal/fatsecret"
	"fitbit-export/internal/hevy"
)

const (
	StrengthHistoryDays = 90

	maxStrengthWorkouts = 6
	weightLookbackDays  = 30
)

var (
	strengthTypes = []string{"POWERLIFTING", "STRENGTH_TRAINING", "WEIGHTLIFTING"}
	nutritionKeys = []string{caloriesKey, proteinKey, carbsKey, fatKey}
)

type StrengthLog struct {
	GeneratedAt time.Time         `json:"generated_at"`
	Days        int               `json:"days"`
	Unavailable []string          `json:"unavailable"`
	Workouts    []Workout         `json:"workouts"`
	Body        Body              `json:"body"`
	Nutrition   map[string]Metric `json:"nutrition_today"`
}

type Body struct {
	WeightKg  Metric     `json:"weight_kg"`
	WeighedAt *time.Time `json:"weighed_at"`
}

// NutritionSource replaces Google Health as the source of daily nutrition totals.
type NutritionSource interface {
	DailyTotals(ctx context.Context, start time.Time, end time.Time) ([]fatsecret.Day, error)
}

// WorkoutPages reads the sets of a workout from a Hevy workout link.
type WorkoutPages interface {
	Workout(ctx context.Context, link string) (string, error)
}

// BuildStrengthLog reads nutrition from Google Health when nutrition is nil,
// and leaves Hevy links in notes as they are when pages is nil.
func BuildStrengthLog(
	ctx context.Context,
	source Source,
	nutrition NutritionSource,
	pages WorkoutPages,
	clock Clock,
	days int,
) (StrengthLog, error) {
	day := startOfDay(clock.Now)
	end := day.AddDate(0, 0, 1)

	points, err := source.List(ctx, "exercise", workoutFilter(end.AddDate(0, 0, -days), end))
	if err != nil {
		return StrengthLog{}, err
	}
	workouts, err := decodeWorkouts(points)
	if err != nil {
		return StrengthLog{}, err
	}

	result := StrengthLog{
		GeneratedAt: clock.Now.Truncate(time.Second),
		Days:        days,
		Unavailable: []string{},
		Workouts:    recentStrengthWorkouts(workouts),
	}
	result.readHevyPages(ctx, pages)

	body, err := loadBody(ctx, source, day, end)
	if err != nil {
		result.markUnavailable("weight", err)
	}
	result.Body = body

	series, err := loadNutrition(ctx, source, nutrition, day, end)
	if err != nil {
		result.markUnavailable("nutrition", err)
	}
	result.Nutrition = make(map[string]Metric, len(nutritionKeys))
	for _, key := range nutritionKeys {
		result.Nutrition[key] = newMetric(series[key], day, 0)
	}

	return result, nil
}

func (s *StrengthLog) markUnavailable(key string, err error) {
	log.Printf("%s: unavailable: %v", key, err)
	s.Unavailable = append(s.Unavailable, key)
}

// readHevyPages puts the sets from the Hevy page in place of the link,
// for workouts whose notes have a link and no sets of their own.
func (s *StrengthLog) readHevyPages(ctx context.Context, pages WorkoutPages) {
	if pages == nil {
		return
	}

	var failed error
	for i := range s.Workouts {
		notes := s.Workouts[i].Notes
		link := hevy.FindLink(notes)
		if link == "" || hevy.HasSets(notes) {
			continue
		}

		sets, err := pages.Workout(ctx, link)
		if err != nil {
			failed = err
			log.Printf("hevy: %v", err)
			continue
		}
		s.Workouts[i].Notes = strings.Replace(notes, link, sets, 1)
	}
	if failed != nil {
		s.Unavailable = append(s.Unavailable, "hevy")
	}
}

func recentStrengthWorkouts(workouts []Workout) []Workout {
	logged := slices.DeleteFunc(slices.Clone(workouts), func(workout Workout) bool {
		return workout.Notes == "" || !slices.Contains(strengthTypes, workout.Type)
	})
	slices.SortFunc(logged, func(a, b Workout) int {
		return a.StartedAt.Compare(b.StartedAt)
	})
	return logged[max(0, len(logged)-maxStrengthWorkouts):]
}

func loadBody(ctx context.Context, source Source, day time.Time, end time.Time) (Body, error) {
	points, err := source.List(ctx, "weight", weightFilter(day.AddDate(0, 0, -weightLookbackDays), end))
	if err != nil {
		return Body{}, err
	}
	weighIns, err := decodeWeighIns(points)
	if err != nil {
		return Body{}, err
	}
	if len(weighIns) == 0 {
		return Body{}, nil
	}

	series := map[string]float64{}
	for _, entry := range weighIns {
		series[dateKey(entry.at)] = entry.kilograms
	}

	latest := weighIns[len(weighIns)-1]
	value := round(latest.kilograms, 1)
	return Body{
		WeightKg:  Metric{Value: &value, Avg7d: average(series, day, 1)},
		WeighedAt: &latest.at,
	}, nil
}

func loadNutrition(
	ctx context.Context,
	source Source,
	nutrition NutritionSource,
	day time.Time,
	end time.Time,
) (map[string]map[string]float64, error) {
	start := day.AddDate(0, 0, -weekDays)
	if nutrition == nil {
		points, err := source.DailyRollUp(ctx, "nutrition-log", start, end)
		if err != nil {
			return nil, err
		}
		return decodeNutrition(points)
	}

	totals, err := nutrition.DailyTotals(ctx, start, end)
	if err != nil {
		return nil, err
	}
	series := map[string]map[string]float64{}
	for _, total := range totals {
		for key, value := range map[string]float64{
			caloriesKey: total.CaloriesKcal,
			proteinKey:  total.ProteinG,
			carbsKey:    total.CarbsG,
			fatKey:      total.FatG,
		} {
			if series[key] == nil {
				series[key] = map[string]float64{}
			}
			series[key][total.Date] = value
		}
	}
	return series, nil
}

func workoutFilter(start time.Time, end time.Time) string {
	return fmt.Sprintf(
		`exercise.interval.civil_start_time >= "%s" AND exercise.interval.civil_start_time < "%s"`,
		start.Format(civilTimeLayout), end.Format(civilTimeLayout),
	)
}

func weightFilter(start time.Time, end time.Time) string {
	return fmt.Sprintf(
		`weight.sample_time.civil_time >= "%s" AND weight.sample_time.civil_time < "%s"`,
		start.Format(civilTimeLayout), end.Format(civilTimeLayout),
	)
}
