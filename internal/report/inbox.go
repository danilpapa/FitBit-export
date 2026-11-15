package report

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"fitbit-export/internal/telegram"
)

const maxMessageRunes = 1000

// MessageSource returns the messages that the user sent to the bot.
type MessageSource interface {
	Messages(ctx context.Context) ([]telegram.Message, error)
}

// Inbox holds today's bot commands, one list per skill. AdditionalTraining has
// links to more Hevy workouts of today: the strength command reads them itself.
type Inbox struct {
	Date               string         `json:"date"`
	GeneratedAt        time.Time      `json:"generated_at"`
	Note               []InboxMessage `json:"note"`
	Strength           []InboxMessage `json:"strength"`
	Nutrition          []InboxMessage `json:"nutrition"`
	AdditionalTraining []InboxMessage `json:"additional_training"`
}

type InboxMessage struct {
	SentAt time.Time `json:"sent_at"`
	Text   string    `json:"text"`
}

// BuildInbox keeps the commands sent today by the device clock, so each
// command applies to one day only.
func BuildInbox(ctx context.Context, source MessageSource, clock Clock) (Inbox, error) {
	day := startOfDay(clock.Now)
	end := day.AddDate(0, 0, 1)

	inbox := Inbox{
		Date:               dateKey(day),
		GeneratedAt:        clock.Now.Truncate(time.Second),
		Note:               []InboxMessage{},
		Strength:           []InboxMessage{},
		Nutrition:          []InboxMessage{},
		AdditionalTraining: []InboxMessage{},
	}

	messages, err := source.Messages(ctx)
	if err != nil {
		return Inbox{}, err
	}
	for _, message := range messages {
		sentAt := message.SentAt.In(clock.Now.Location())
		if sentAt.Before(day) || !sentAt.Before(end) {
			continue
		}

		command, text := parseCommand(message.Text)
		if text == "" {
			continue
		}
		entry := InboxMessage{SentAt: sentAt, Text: text}
		switch command {
		case "note":
			inbox.Note = append(inbox.Note, entry)
		case "strength":
			inbox.Strength = append(inbox.Strength, entry)
		case "nutrition":
			inbox.Nutrition = append(inbox.Nutrition, entry)
		case "additionaltraining", "additional_training":
			inbox.AdditionalTraining = append(inbox.AdditionalTraining, entry)
		}
	}
	return inbox, nil
}

// parseCommand splits "/note@MyBot text" into "note" and "text". Commands are case
// insensitive, so /additionalTraining works although BotFather only takes lowercase.
func parseCommand(message string) (string, string) {
	message = strings.TrimSpace(message)
	if !strings.HasPrefix(message, "/") {
		return "", ""
	}

	command, text, _ := strings.Cut(message, " ")
	command, _, _ = strings.Cut(strings.ToLower(command[1:]), "@")
	text = strings.TrimSpace(text)

	if utf8.RuneCountInString(text) > maxMessageRunes {
		text = string([]rune(text)[:maxMessageRunes]) + "…"
	}
	return command, text
}
