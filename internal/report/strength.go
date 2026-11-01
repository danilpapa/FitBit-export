package report

import (
	"context"
	"fmt"
	"html"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	StrengthHistoryDays = 90

	maxStrengthWorkouts = 12
	maxE1RMReps         = 12
	titleSeparator      = " | "
)

var (
	hevyURLPattern     = regexp.MustCompile(`https://hevy\.com/workout/[A-Za-z0-9_-]+`)
	hiddenBlockPattern = regexp.MustCompile(`(?is)<(script|style)\b.*?</(script|style)>`)
	htmlTagPattern     = regexp.MustCompile(`<[^>]+>`)
	weightedSetPattern = regexp.MustCompile(`(?i)^(\d+(?:[.,]\d+)?)\s*(?:kg|кг)\s*x\s*(\d+)\b`)
	bodyweightPattern  = regexp.MustCompile(`(?i)^(\d+)\s*(?:reps?|повтор)`)
	setsHeaderPattern  = regexp.MustCompile(`(?i)^(?:sets?|подходы)$`)
	setMarkerPattern   = regexp.MustCompile(`(?i)^(?:\d+|[wfd])$`)
	pageFooterPattern  = regexp.MustCompile(`(?i)^(?:to follow|чтобы подписаться)`)
)

type PageLoader func(ctx context.Context, url string) (string, error)

type StrengthLog struct {
	GeneratedAt time.Time         `json:"generated_at"`
	Days        int               `json:"days"`
	Workouts    []StrengthWorkout `json:"workouts"`
}

type StrengthWorkout struct {
	Title         string             `json:"title,omitempty"`
	URL           string             `json:"url"`
	StartedAt     time.Time          `json:"started_at"`
	Minutes       float64            `json:"minutes"`
	Exercises     []StrengthExercise `json:"exercises"`
	UnparsedLines []string           `json:"unparsed_lines,omitempty"`
	Error         string             `json:"error,omitempty"`
}

type StrengthExercise struct {
	Name       string        `json:"name"`
	BestE1RMKg float64       `json:"best_e1rm_kg,omitempty"`
	VolumeKg   float64       `json:"volume_kg"`
	Sets       []StrengthSet `json:"sets"`
}

type StrengthSet struct {
	WeightKg float64 `json:"weight_kg"`
	Reps     int     `json:"reps"`
	Type     string  `json:"type,omitempty"`
}

func BuildStrengthLog(ctx context.Context, source Source, load PageLoader, clock Clock, days int) (StrengthLog, error) {
	end := startOfDay(clock.Now).AddDate(0, 0, 1)
	points, err := source.List(ctx, "exercise", workoutFilter(end.AddDate(0, 0, -days), end))
	if err != nil {
		return StrengthLog{}, err
	}

	records, err := decodeWorkouts(points)
	if err != nil {
		return StrengthLog{}, err
	}

	log := StrengthLog{
		GeneratedAt: clock.Now.Truncate(time.Second),
		Days:        days,
		Workouts:    []StrengthWorkout{},
	}
	for _, record := range recentHevyWorkouts(records) {
		log.Workouts = append(log.Workouts, loadStrengthWorkout(ctx, load, record))
	}
	return log, nil
}

func recentHevyWorkouts(records []Workout) []Workout {
	linked := slices.DeleteFunc(slices.Clone(records), func(record Workout) bool {
		return !hevyURLPattern.MatchString(record.Notes)
	})
	slices.SortFunc(linked, func(a, b Workout) int {
		return a.StartedAt.Compare(b.StartedAt)
	})
	return linked[max(0, len(linked)-maxStrengthWorkouts):]
}

func loadStrengthWorkout(ctx context.Context, load PageLoader, record Workout) StrengthWorkout {
	url := hevyURLPattern.FindString(record.Notes)

	page, err := load(ctx, url)
	workout := StrengthWorkout{Exercises: []StrengthExercise{}}
	if err != nil {
		workout.Error = err.Error()
	} else {
		workout = parseHevyPage(page)
	}

	workout.URL = url
	workout.StartedAt = record.StartedAt
	workout.Minutes = record.Minutes
	return workout
}

func parseHevyPage(page string) StrengthWorkout {
	workout := StrengthWorkout{Exercises: []StrengthExercise{}}

	lines := pageLines(page)
	if len(lines) > 0 {
		_, title, found := strings.Cut(lines[0], titleSeparator)
		if found {
			workout.Title = title
		}
	}

	for i, line := range lines {
		if pageFooterPattern.MatchString(line) {
			break
		}

		switch {
		case i > 0 && setsHeaderPattern.MatchString(line):
			workout.Exercises = append(workout.Exercises, StrengthExercise{Name: lines[i-1], Sets: []StrengthSet{}})
		case len(workout.Exercises) > 0 && i > 0 && setMarkerPattern.MatchString(lines[i-1]):
			set, ok := parseHevySet(line, lines[i-1])
			if !ok {
				workout.UnparsedLines = append(workout.UnparsedLines, line)
				continue
			}
			last := &workout.Exercises[len(workout.Exercises)-1]
			last.Sets = append(last.Sets, set)
		}
	}

	for i := range workout.Exercises {
		workout.Exercises[i].summarize()
	}
	return workout
}

func pageLines(page string) []string {
	page = hiddenBlockPattern.ReplaceAllString(page, "\n")
	page = htmlTagPattern.ReplaceAllString(page, "\n")

	var lines []string
	for line := range strings.Lines(html.UnescapeString(page)) {
		line = strings.TrimSpace(strings.ReplaceAll(line, " ", " "))
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func parseHevySet(line string, marker string) (StrengthSet, bool) {
	set := StrengthSet{Type: setType(marker)}

	if match := weightedSetPattern.FindStringSubmatch(line); match != nil {
		weight, err := strconv.ParseFloat(strings.Replace(match[1], ",", ".", 1), 64)
		if err != nil {
			return StrengthSet{}, false
		}
		reps, err := strconv.Atoi(match[2])
		if err != nil {
			return StrengthSet{}, false
		}
		set.WeightKg = weight
		set.Reps = reps
		return set, true
	}

	if match := bodyweightPattern.FindStringSubmatch(line); match != nil {
		reps, err := strconv.Atoi(match[1])
		if err != nil {
			return StrengthSet{}, false
		}
		set.Reps = reps
		return set, true
	}
	return StrengthSet{}, false
}

func setType(marker string) string {
	switch strings.ToUpper(marker) {
	case "W":
		return "warmup"
	case "F":
		return "failure"
	case "D":
		return "drop"
	}
	return ""
}

func (e *StrengthExercise) summarize() {
	var volume, best float64
	for _, set := range e.Sets {
		if set.Type == "warmup" {
			continue
		}
		volume += set.WeightKg * float64(set.Reps)
		best = max(best, set.estimatedMax())
	}
	e.VolumeKg = round(volume, 1)
	e.BestE1RMKg = round(best, 1)
}

func (s StrengthSet) estimatedMax() float64 {
	if s.WeightKg == 0 || s.Reps == 0 || s.Reps > maxE1RMReps {
		return 0
	}
	if s.Reps == 1 {
		return s.WeightKg
	}
	return s.WeightKg * (1 + float64(s.Reps)/30)
}

func workoutFilter(start time.Time, end time.Time) string {
	return fmt.Sprintf(
		`exercise.interval.civil_start_time >= "%s" AND exercise.interval.civil_start_time < "%s"`,
		start.Format(civilTimeLayout), end.Format(civilTimeLayout),
	)
}
