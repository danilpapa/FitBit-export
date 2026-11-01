package report

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

const hevyPage = `<!DOCTYPE html><html><head><title>athlete | Тяжёлый день</title>
<script>{"reps": 999}</script><style>.set { color: red }</style></head><body>
<div>Log in</div><div>Yesterday at 18:52</div><div>Duration</div><div>1h 20min</div><div>Volume</div><div>3&nbsp;000 kg</div>
<div>Squat (Barbell)</div><div>SETS</div><div>WEIGHT &amp; REPS</div>
<div>W</div><div>60kg x 8 reps</div>
<div>1</div><div>100kg x 5 reps</div><div>Best Volume</div>
<div>F</div><div>102,5kg x 5 reps</div>
<div>Pull Up</div><div>SETS</div><div>REPS</div>
<div>1</div><div>12 reps</div>
<div>2</div><div>1min 30s</div>
<div>Bent Over Row (Barbell)</div><div>ПОДХОДЫ</div><div>ВЕС И ПОВТОРЕНИЯ</div>
<div>1</div><div>60 кг x 15 повторений</div>
<div>2</div><div>70 кг x 8 повторений</div>
<div>To follow athlete and track your own workouts, download Hevy for free.</div>
<div>Workouts</div><div>3</div><div>Followers</div><div>1</div><div>Following</div></body></html>`

func strengthPoint(start string, notes string) string {
	return fmt.Sprintf(`{"exercise":{"interval":{"startTime":"%s","startUtcOffset":"10800s","endTime":"%s","endUtcOffset":"10800s"},
		"exerciseType":"STRENGTH_TRAINING","displayName":"Силовая","notes":%q,"activeDuration":"3600s","metricsSummary":{}}}`,
		start, start, notes)
}

func TestParseHevyPage(t *testing.T) {
	workout := parseHevyPage(hevyPage)

	if workout.Title != "Тяжёлый день" {
		t.Fatalf("Title = %q", workout.Title)
	}
	if len(workout.Exercises) != 3 {
		t.Fatalf("Exercises = %+v, want 3", workout.Exercises)
	}

	squat := workout.Exercises[0]
	if squat.Name != "Squat (Barbell)" || len(squat.Sets) != 3 {
		t.Fatalf("squat = %+v", squat)
	}
	if squat.Sets[0].Type != "warmup" {
		t.Fatalf("first set = %+v, want a warmup", squat.Sets[0])
	}
	if want := (StrengthSet{WeightKg: 102.5, Reps: 5, Type: "failure"}); squat.Sets[2] != want {
		t.Fatalf("last set = %+v, want %+v", squat.Sets[2], want)
	}
	if squat.VolumeKg != 1012.5 || squat.BestE1RMKg != 119.6 {
		t.Fatalf("squat volume = %v, e1RM = %v, want no warmup", squat.VolumeKg, squat.BestE1RMKg)
	}
}

func TestParseHevyPageKeepsUnknownSets(t *testing.T) {
	workout := parseHevyPage(hevyPage)

	pullUp := workout.Exercises[1]
	if len(pullUp.Sets) != 1 || pullUp.Sets[0].Reps != 12 || pullUp.Sets[0].WeightKg != 0 {
		t.Fatalf("pull up = %+v", pullUp)
	}
	if len(workout.UnparsedLines) != 1 || workout.UnparsedLines[0] != "1min 30s" {
		t.Fatalf("UnparsedLines = %v", workout.UnparsedLines)
	}
}

func TestEstimatedMaxSkipsLongSets(t *testing.T) {
	row := parseHevyPage(hevyPage).Exercises[2]

	if row.BestE1RMKg != 88.7 {
		t.Fatalf("row e1RM = %v, want 88.7 from the set of 8", row.BestE1RMKg)
	}
	if got := (StrengthSet{WeightKg: 90, Reps: 1}).estimatedMax(); got != 90 {
		t.Fatalf("single = %v, want the lifted weight", got)
	}
}

func TestBuildStrengthLogLoadsLinkedWorkoutsInOrder(t *testing.T) {
	var loaded []string
	load := func(_ context.Context, url string) (string, error) {
		loaded = append(loaded, url)
		return hevyPage, nil
	}
	filters := map[string]string{}
	source := fakeSource{
		lists: map[string][]string{"exercise": {
			strengthPoint("2026-10-30T15:00:00Z", "https://hevy.com/workout/newer"),
			strengthPoint("2026-10-20T15:00:00Z", "заметка https://hevy.com/workout/older"),
			strengthPoint("2026-10-25T15:00:00Z", "тест"),
		}},
		filters: filters,
	}

	log, err := BuildStrengthLog(context.Background(), source, load, Clock{Now: testNow}, StrengthHistoryDays)
	if err != nil {
		t.Fatalf("BuildStrengthLog() error = %v", err)
	}

	if len(log.Workouts) != 2 {
		t.Fatalf("Workouts = %+v, want only linked workouts", log.Workouts)
	}
	if want := "https://hevy.com/workout/older"; log.Workouts[0].URL != want || loaded[0] != want {
		t.Fatalf("first workout = %s, loaded = %v, want the oldest first", log.Workouts[0].URL, loaded)
	}
	if log.Workouts[0].Minutes != 60 {
		t.Fatalf("Minutes = %v", log.Workouts[0].Minutes)
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
		points = append(points, strengthPoint(start, fmt.Sprintf("https://hevy.com/workout/w%d", day)))
	}
	load := func(context.Context, string) (string, error) { return hevyPage, nil }

	log, err := BuildStrengthLog(context.Background(), fakeSource{lists: map[string][]string{"exercise": points}}, load, Clock{Now: testNow}, StrengthHistoryDays)
	if err != nil {
		t.Fatalf("BuildStrengthLog() error = %v", err)
	}

	if len(log.Workouts) != maxStrengthWorkouts {
		t.Fatalf("Workouts = %d, want %d", len(log.Workouts), maxStrengthWorkouts)
	}
	if got := log.Workouts[0].URL; !strings.HasSuffix(got, "/w4") {
		t.Fatalf("first workout = %s, want the oldest kept one", got)
	}
}

func TestBuildStrengthLogLoadsRepeatedLinkOnce(t *testing.T) {
	loads := 0
	load := func(context.Context, string) (string, error) {
		loads++
		return hevyPage, nil
	}
	source := fakeSource{lists: map[string][]string{"exercise": {
		strengthPoint("2026-10-03T15:00:00Z", "https://hevy.com/workout/same"),
		strengthPoint("2026-10-30T15:00:00Z", "https://hevy.com/workout/same"),
	}}}

	log, err := BuildStrengthLog(context.Background(), source, load, Clock{Now: testNow}, StrengthHistoryDays)
	if err != nil {
		t.Fatalf("BuildStrengthLog() error = %v", err)
	}

	if loads != 1 || len(log.Workouts) != 1 {
		t.Fatalf("loads = %d, Workouts = %+v, want one workout", loads, log.Workouts)
	}
	if got := log.Workouts[0].StartedAt.Format("2006-01-02"); got != "2026-10-30" {
		t.Fatalf("StartedAt = %s, want the newest record", got)
	}
}

func TestBuildStrengthLogKeepsFailedPage(t *testing.T) {
	load := func(context.Context, string) (string, error) { return "", errors.New("page is incomplete") }
	source := fakeSource{lists: map[string][]string{"exercise": {
		strengthPoint("2026-10-30T15:00:00Z", "https://hevy.com/workout/broken"),
	}}}

	log, err := BuildStrengthLog(context.Background(), source, load, Clock{Now: testNow}, StrengthHistoryDays)
	if err != nil {
		t.Fatalf("BuildStrengthLog() error = %v", err)
	}

	workout := log.Workouts[0]
	if workout.Error == "" || workout.URL == "" || workout.Exercises == nil {
		t.Fatalf("workout = %+v", workout)
	}
}

func TestBuildStrengthLogEmptyIsNotNull(t *testing.T) {
	load := func(context.Context, string) (string, error) { return "", nil }

	log, err := BuildStrengthLog(context.Background(), fakeSource{}, load, Clock{Now: testNow}, StrengthHistoryDays)
	if err != nil {
		t.Fatalf("BuildStrengthLog() error = %v", err)
	}
	if log.Workouts == nil {
		t.Fatal("Workouts = nil, want an empty list")
	}
}

func TestBuildStrengthLogReturnsSourceError(t *testing.T) {
	load := func(context.Context, string) (string, error) { return "", nil }

	if _, err := BuildStrengthLog(context.Background(), fakeSource{failAll: true}, load, Clock{Now: testNow}, StrengthHistoryDays); err == nil {
		t.Fatal("BuildStrengthLog() error = nil")
	}
}
