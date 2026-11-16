package hevy

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestParseDate(t *testing.T) {
	moscow := time.FixedZone("UTC+3", 3*3600)
	now := time.Date(2026, 11, 16, 23, 52, 0, 0, moscow)
	tests := map[string]string{
		"13 нояб. 2026 г., 18:14":    "2026-11-13T18:14:00+03:00",
		"1 мая 2026 г., 07:05":       "2026-05-01T07:05:00+03:00",
		"5 сент. 2026\u00a0г., 9:30": "2026-09-05T09:30:00+03:00",
		"28 февр. 2027 г., 23:59":    "2027-02-28T23:59:00+03:00",
		"Вчера в 18:49":              "2026-11-15T18:49:00+03:00",
		"Вчера в 0:05":               "2026-11-15T00:05:00+03:00",
	}
	for line, want := range tests {
		got, today, err := ParseDate(line, now)
		if err != nil {
			t.Errorf("ParseDate(%q): %v", line, err)
			continue
		}
		if today || got.Format(time.RFC3339) != want {
			t.Errorf("ParseDate(%q) = %s, %v, want %s", line, got.Format(time.RFC3339), today, want)
		}
	}

	for _, line := range []string{"3 часа назад", "несколько секунд назад", "час назад", "21 минуту назад", "через минуту"} {
		got, today, err := ParseDate(line, now)
		if err != nil || !today || !got.IsZero() {
			t.Errorf("ParseDate(%q) = %v, %v, %v, want a workout that ended today", line, got, today, err)
		}
	}

	for _, line := range []string{"Загрузка...", "13 foo 2026 г., 18:14", "", "Жим лежа (Штанга)"} {
		if _, _, err := ParseDate(line, now); err == nil {
			t.Errorf("ParseDate(%q) should fail", line)
		}
	}
}

func TestWorkoutDateFindsTheDateLine(t *testing.T) {
	now := time.Date(2026, 11, 16, 23, 52, 0, 0, time.UTC)
	started, today, err := WorkoutDate("danilpapa\nВчера в 18:49\nстей хамбл\nПродолжительность: 2 ч", now)
	if err != nil || today || started.Format("2006-01-02 15:04") != "2026-11-15 18:49" {
		t.Fatalf("WorkoutDate() = %v, %v, %v", started, today, err)
	}
	if _, today, err := WorkoutDate("danilpapa\n2 часа назад\nстей хамбл", now); err != nil || !today {
		t.Fatalf("WorkoutDate() = %v, %v, want today", today, err)
	}
	if _, _, err := WorkoutDate("danilpapa\nстей хамбл", now); err == nil {
		t.Fatal("WorkoutDate() without a date should fail")
	}
}

func TestWithDescription(t *testing.T) {
	notes := "danilpapa\n13 нояб. 2026 г., 18:14\nразрядил ган-пауэр\nПродолжительность: 2 ч 6 мин"
	card := Card{Title: "разрядил ган-пауэр", Description: "болело плечо"}

	want := "danilpapa\n13 нояб. 2026 г., 18:14\nразрядил ган-пауэр\nОписание: болело плечо\nПродолжительность: 2 ч 6 мин"
	if got := WithDescription(notes, card); got != want {
		t.Errorf("WithDescription() =\n%s\nwant\n%s", got, want)
	}
	if got := WithDescription(notes, Card{Title: "другое", Description: "болело плечо"}); got != "Описание: болело плечо\n"+notes {
		t.Errorf("WithDescription() without the title =\n%s", got)
	}
	if got := WithDescription(notes, Card{Title: card.Title}); got != notes {
		t.Errorf("WithDescription() without a description changed the notes")
	}
}

// HEVY_USER=danilpapa go test ./internal/hevy -run TestLatestLive -v
func TestLatestLive(t *testing.T) {
	user := os.Getenv("HEVY_USER")
	if user == "" {
		t.Skip("set HEVY_USER to a Hevy username")
	}

	p := New(time.Local)
	defer p.Close()

	card, err := p.Latest(context.Background(), user)
	if err != nil {
		t.Fatal(err)
	}
	notes, err := p.Workout(context.Background(), card.Link)
	if err != nil {
		t.Fatal(err)
	}
	if !HasSets(notes) {
		t.Fatalf("no sets on %s:\n%s", card.Link, notes)
	}
	t.Logf("%s, date %q, ended today %v\n%s", card.Link, card.Date, card.EndedToday, WithDescription(notes, card))
}
