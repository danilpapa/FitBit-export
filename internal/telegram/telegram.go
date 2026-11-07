// Package telegram reads the messages that the user sent to the bot.
package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"

	"fitbit-export/internal/config"
)

const (
	apiURL       = "https://api.telegram.org"
	maxBodyBytes = 1 << 20
)

type Message struct {
	ID     int64
	SentAt time.Time
	Text   string
}

type Client struct {
	httpClient *http.Client
	config     config.Telegram
	baseURL    string
}

func New(cfg config.Telegram) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		config:     cfg,
		baseURL:    apiURL,
	}
}

// Messages returns the text messages from the configured chat that Telegram still
// keeps, oldest first. Telegram keeps updates for about 24 hours. It does not mark
// them as read, so a second run on the same day sees the same messages.
func (c *Client) Messages(ctx context.Context) ([]Message, error) {
	if c.config.BotToken == "" || c.config.ChatID == "" {
		return nil, errors.New("TELEGRAM_BOT_TOKEN and TELEGRAM_CHAT_ID are required")
	}
	chatID, err := strconv.ParseInt(c.config.ChatID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("TELEGRAM_CHAT_ID is not a number")
	}

	query := url.Values{"allowed_updates": {`["message","edited_message"]`}}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/bot"+c.config.BotToken+"/getUpdates?"+query.Encode(), nil)
	if err != nil {
		return nil, errors.New("build Telegram request")
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		// The URL holds the bot token, so keep it out of the error.
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return nil, fmt.Errorf("Telegram getUpdates: %w", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, maxBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("read Telegram response: %w", err)
	}

	var updates struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
		Result      []struct {
			Message       *message `json:"message"`
			EditedMessage *message `json:"edited_message"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &updates); err != nil {
		return nil, fmt.Errorf("decode Telegram response with HTTP %d: %w", response.StatusCode, err)
	}
	if !updates.OK {
		return nil, fmt.Errorf("Telegram getUpdates returned HTTP %d: %s", response.StatusCode, updates.Description)
	}

	// An edit arrives as a new update with the same message ID and replaces the text.
	byID := map[int64]Message{}
	for _, update := range updates.Result {
		for _, m := range []*message{update.Message, update.EditedMessage} {
			if m == nil || m.Chat.ID != chatID || m.Text == "" {
				continue
			}
			byID[m.MessageID] = Message{ID: m.MessageID, SentAt: time.Unix(m.Date, 0), Text: m.Text}
		}
	}

	messages := make([]Message, 0, len(byID))
	for _, m := range byID {
		messages = append(messages, m)
	}
	slices.SortFunc(messages, func(a, b Message) int {
		if c := a.SentAt.Compare(b.SentAt); c != 0 {
			return c
		}
		return int(a.ID - b.ID)
	})
	return messages, nil
}

type message struct {
	MessageID int64  `json:"message_id"`
	Date      int64  `json:"date"`
	Text      string `json:"text"`
	Chat      struct {
		ID int64 `json:"id"`
	} `json:"chat"`
}
