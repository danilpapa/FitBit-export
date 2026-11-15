package hevy

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestParseDate(t *testing.T) {
	moscow := time.FixedZone("UTC+3", 3*3600)
	tests := map[string]string{
		"danilpapa\n13 нояб. 2026 г., 18:14\nразрядил":        "2026-11-13T18:14:00+03:00",
		"1 мая 2026 г., 07:05":                                "2026-05-01T07:05:00+03:00",
		"5 сент. 2026\u00a0г., 9:30":                          "2026-09-05T09:30:00+03:00",
		"28 февр. 2027 г., 23:59":                             "2027-02-28T23:59:00+03:00",
		"Продолжительность\n2 ч 6 мин\n7 июл. 2026 г., 12:00": "2026-07-07T12:00:00+03:00",
	}
	for text, want := range tests {
		got, err := ParseDate(text, moscow)
		if err != nil {
			t.Errorf("ParseDate(%q): %v", text, err)
			continue
		}
		if got.Format(time.RFC3339) != want {
			t.Errorf("ParseDate(%q) = %s, want %s", text, got.Format(time.RFC3339), want)
		}
	}

	for _, text := range []string{"Загрузка...", "13 foo 2026 г., 18:14"} {
		if _, err := ParseDate(text, moscow); err == nil {
			t.Errorf("ParseDate(%q) should fail", text)
		}
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
	t.Logf("%s, started %s\n%s", card.Link, card.StartedAt.Format("2006-01-02 15:04"), WithDescription(notes, card))
}
