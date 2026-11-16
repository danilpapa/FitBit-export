package report

import (
	"cmp"
	"context"
	"fmt"
	"log"
	"slices"
	"time"

	"fitbit-export/internal/fatsecret"
	"fitbit-export/internal/hevy"
)

const weightLookbackDays = 30

var nutritionKeys = []string{caloriesKey, proteinKey, carbsKey, fatKey}

// StrengthLog has today's workouts from Hevy, body weight and today's nutrition.
// History of older workouts lives in the memory of the strength-coach skill.
type StrengthLog struct {
	GeneratedAt time.Time         `json:"generated_at"`
	Unavailable []string          `json:"unavailable"`
	Workouts    []HevyWorkout     `json:"workouts"`
	Body        Body              `json:"body"`
	Nutrition   map[string]Metric `json:"nutrition_today"`
}

// HevyWorkout is one workout of today in the format of workout notes.
type HevyWorkout struct {
	// Source is "profile" for the newest workout on the profile page and
	// "additional" for a link from the /additionalTraining command.
	Source string `json:"source"`
	Link   string `json:"link"`
	// StartedAt is null for a workout that ended today: Hevy shows it as
	// "3 часа назад", without the time.
	StartedAt *time.Time `json:"started_at"`
	Notes     string     `json:"notes"`

	// page is the text of the workout page, to find the same workout under another link.
	page string
}

type Body struct {
	WeightKg  Metric     `json:"weight_kg"`
	WeighedAt *time.Time `json:"weighed_at"`
}

// NutritionSource replaces Google Health as the source of daily nutrition totals.
type NutritionSource interface {
	DailyTotals(ctx context.Context, start time.Time, end time.Time) ([]fatsecret.Day, error)
}

// WorkoutPages reads public Hevy pages.
type WorkoutPages interface {
	Latest(ctx context.Context, username string) (hevy.Card, error)
	Workout(ctx context.Context, link string) (string, error)
}

// Hevy says where today's workouts come from. Without Pages there are none.
type Hevy struct {
	Pages WorkoutPages
	// User is the Hevy username. The newest workout on the profile counts when it started today.
	User string
	// Inbox has the /additionalTraining commands with links to more workouts of today.
	Inbox MessageSource
}

// BuildStrengthLog reads nutrition from Google Health when nutrition is nil.
func BuildStrengthLog(
	ctx context.Context,
	source Source,
	nutrition NutritionSource,
	workouts Hevy,
	clock Clock,
) StrengthLog {
	day := startOfDay(clock.Now)
	end := day.AddDate(0, 0, 1)

	result := StrengthLog{
		GeneratedAt: clock.Now.Truncate(time.Second),
		Unavailable: []string{},
		Workouts:    []HevyWorkout{},
	}
	result.readHevy(ctx, workouts, clock)

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

	return result
}

func (s *StrengthLog) markUnavailable(key string, err error) {
	log.Printf("%s: unavailable: %v", key, err)
	s.Unavailable = append(s.Unavailable, key)
}

// readHevy collects today's workouts: the newest one on the profile when it
// started today, and the ones linked with /additionalTraining. One failed page
// marks hevy unavailable and keeps the rest.
func (s *StrengthLog) readHevy(ctx context.Context, source Hevy, clock Clock) {
	if source.Pages == nil {
		return
	}
	var failed error

	if source.User != "" {
		if err := s.readProfile(ctx, source, clock); err != nil {
			failed = err
		}
	}

	for _, link := range s.additionalLinks(ctx, source.Inbox, clock) {
		if slices.ContainsFunc(s.Workouts, func(known HevyWorkout) bool { return known.Link == link }) {
			continue
		}
		notes, err := source.Pages.Workout(ctx, link)
		if err != nil {
			failed = err
			log.Printf("hevy: %v", err)
			continue
		}
		workout := HevyWorkout{Source: "additional", Link: link, Notes: notes, page: notes}
		if started, _, err := hevy.WorkoutDate(notes, clock.Now); err == nil && !started.IsZero() {
			workout.StartedAt = &started
		}
		s.addWorkout(workout)
	}

	// Workouts of today have no start time and go last. The profile one is the newest of them.
	slices.SortStableFunc(s.Workouts, func(a, b HevyWorkout) int {
		switch {
		case a.StartedAt == nil && b.StartedAt == nil:
			return cmp.Compare(rank(a), rank(b))
		case a.StartedAt == nil:
			return 1
		case b.StartedAt == nil:
			return -1
		}
		return a.StartedAt.Compare(*b.StartedAt)
	})
	if failed != nil {
		s.Unavailable = append(s.Unavailable, "hevy")
	}
}

func (s *StrengthLog) readProfile(ctx context.Context, source Hevy, clock Clock) error {
	card, err := source.Pages.Latest(ctx, source.User)
	if err != nil {
		log.Printf("hevy: %v", err)
		return err
	}
	startedToday := !card.StartedAt.IsZero() && startOfDay(card.StartedAt.In(clock.Now.Location())).Equal(startOfDay(clock.Now))
	if !card.EndedToday && !startedToday {
		log.Printf("hevy: no workout today, the newest is %q", card.Date)
		return nil
	}

	notes, err := source.Pages.Workout(ctx, card.Link)
	if err != nil {
		log.Printf("hevy: %v", err)
		return err
	}
	workout := HevyWorkout{Source: "profile", Link: card.Link, Notes: hevy.WithDescription(notes, card), page: notes}
	if !card.StartedAt.IsZero() {
		started := card.StartedAt
		workout.StartedAt = &started
	}
	s.addWorkout(workout)
	return nil
}

func rank(workout HevyWorkout) int {
	if workout.Source == "profile" {
		return 1
	}
	return 0
}

// addWorkout skips a workout that is already there: the app and the share
// sheet give the same workout different links, so the page text decides too.
func (s *StrengthLog) addWorkout(workout HevyWorkout) {
	for _, known := range s.Workouts {
		if known.Link == workout.Link || known.page == workout.page {
			return
		}
	}
	s.Workouts = append(s.Workouts, workout)
}

// additionalLinks returns the Hevy links from today's /additionalTraining commands.
func (s *StrengthLog) additionalLinks(ctx context.Context, source MessageSource, clock Clock) []string {
	if source == nil {
		return nil
	}
	inbox, err := BuildInbox(ctx, source, clock)
	if err != nil {
		s.markUnavailable("inbox", err)
		return nil
	}

	var links []string
	for _, message := range inbox.AdditionalTraining {
		link := hevy.FindLink(message.Text)
		if link == "" {
			log.Printf("hevy: /additionalTraining sent at %s has no workout link", message.SentAt.Format("15:04"))
			continue
		}
		if !slices.Contains(links, link) {
			links = append(links, link)
		}
	}
	return links
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
