package report

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"fitbit-export/internal/telegram"
)

type fakeMessages struct {
	messages []telegram.Message
	err      error
}

func (f fakeMessages) Messages(context.Context) ([]telegram.Message, error) {
	return f.messages, f.err
}

func TestBuildInboxKeepsTodaysCommands(t *testing.T) {
	moscow := time.FixedZone("", 3*3600)
	now := time.Date(2026, time.November, 7, 23, 50, 0, 0, moscow)
	at := func(day, hour, minute int) time.Time {
		return time.Date(2026, time.November, day, hour, minute, 0, 0, moscow).UTC()
	}

	inbox, err := BuildInbox(context.Background(), fakeMessages{messages: []telegram.Message{
		{SentAt: at(6, 23, 59), Text: "/nutrition вчерашнее"},
		{SentAt: at(7, 0, 5), Text: "/nutrition сегодня в гостях, не записывай еду в память"},
		{SentAt: at(7, 9, 0), Text: "/note@FitBitExporterBot  голова болит "},
		{SentAt: at(7, 18, 0), Text: "/STRENGTH болело плечо"},
		{SentAt: at(7, 19, 0), Text: "просто текст"},
		{SentAt: at(7, 20, 0), Text: "/note"},
		{SentAt: at(7, 21, 0), Text: "/start"},
	}}, Clock{Now: now})
	if err != nil {
		t.Fatalf("BuildInbox() error = %v", err)
	}

	if inbox.Date != "2026-11-07" {
		t.Fatalf("Date = %s", inbox.Date)
	}
	if len(inbox.Nutrition) != 1 || inbox.Nutrition[0].Text != "сегодня в гостях, не записывай еду в память" {
		t.Fatalf("Nutrition = %+v, want only today's command", inbox.Nutrition)
	}
	if got := inbox.Nutrition[0].SentAt.Format(time.RFC3339); got != "2026-11-07T00:05:00+03:00" {
		t.Fatalf("SentAt = %s, want the device time", got)
	}
	if len(inbox.Note) != 1 || inbox.Note[0].Text != "голова болит" {
		t.Fatalf("Note = %+v", inbox.Note)
	}
	if len(inbox.Strength) != 1 || inbox.Strength[0].Text != "болело плечо" {
		t.Fatalf("Strength = %+v", inbox.Strength)
	}
}

func TestBuildInboxJSONHasEmptyLists(t *testing.T) {
	inbox, err := BuildInbox(context.Background(), fakeMessages{}, Clock{Now: testNow})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(inbox)
	for _, want := range []string{`"note":[]`, `"strength":[]`, `"nutrition":[]`} {
		if !strings.Contains(string(encoded), want) {
			t.Fatalf("JSON has no %s: %s", want, encoded)
		}
	}
}

func TestBuildInboxReturnsError(t *testing.T) {
	if _, err := BuildInbox(context.Background(), fakeMessages{err: errors.New("HTTP 401")}, Clock{Now: testNow}); err == nil {
		t.Fatal("BuildInbox() error = nil")
	}
}

func TestParseCommandTruncatesLongText(t *testing.T) {
	_, text := parseCommand("/note " + strings.Repeat("я", maxMessageRunes+50))
	if got := len([]rune(text)); got != maxMessageRunes+1 {
		t.Fatalf("length = %d, want %d", got, maxMessageRunes+1)
	}
}
