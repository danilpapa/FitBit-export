package report

import (
	"context"
	"fmt"
	"log"
	"slices"
	"time"
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

func BuildStrengthLog(ctx context.Context, source Source, clock Clock, days int) (StrengthLog, error) {
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

	body, err := loadBody(ctx, source, day, end)
	if err != nil {
		result.markUnavailable("weight", err)
	}
	result.Body = body

	series, err := loadNutrition(ctx, source, day, end)
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

func loadNutrition(ctx context.Context, source Source, day time.Time, end time.Time) (map[string]map[string]float64, error) {
	points, err := source.DailyRollUp(ctx, "nutrition-log", day.AddDate(0, 0, -weekDays), end)
	if err != nil {
		return nil, err
	}
	return decodeNutrition(points)
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
