package telegram

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"fitbit-export/internal/config"
)

func testClient(server *httptest.Server) *Client {
	client := New(config.Telegram{BotToken: "secret-token", ChatID: "42"})
	client.baseURL = server.URL
	return client
}

func TestMessagesKeepsOwnChatAndAppliesEdits(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/botsecret-token/getUpdates" {
			t.Errorf("path = %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"ok":true,"result":[
			{"update_id":1,"message":{"message_id":10,"date":1794070000,"chat":{"id":42},"text":"/note плохо спал"}},
			{"update_id":2,"message":{"message_id":11,"date":1794060000,"chat":{"id":7},"text":"/nutrition спам"}},
			{"update_id":3,"message":{"message_id":12,"date":1794065000,"chat":{"id":42},"text":"/strength болит плечо"}},
			{"update_id":4,"edited_message":{"message_id":10,"date":1794070000,"chat":{"id":42},"text":"/note плохо спал, 5 ч"}},
			{"update_id":5,"message":{"message_id":13,"date":1794066000,"chat":{"id":42}}}
		]}`)
	}))
	defer server.Close()

	messages, err := testClient(server).Messages(context.Background())
	if err != nil {
		t.Fatalf("Messages() error = %v", err)
	}

	var texts []string
	for _, m := range messages {
		texts = append(texts, m.Text)
	}
	if want := []string{"/strength болит плечо", "/note плохо спал, 5 ч"}; !slices.Equal(texts, want) {
		t.Fatalf("texts = %q, want %q", texts, want)
	}
}

func TestMessagesHidesTokenInErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	client := testClient(server)
	server.Close()

	_, err := client.Messages(context.Background())
	if err == nil || strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("Messages() error = %v, want an error without the token", err)
	}
}

func TestMessagesReportsTelegramError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		fmt.Fprint(w, `{"ok":false,"error_code":409,"description":"Conflict: can't use getUpdates method while webhook is active"}`)
	}))
	defer server.Close()

	_, err := testClient(server).Messages(context.Background())
	if err == nil || !strings.Contains(err.Error(), "HTTP 409: Conflict") {
		t.Fatalf("Messages() error = %v", err)
	}
}
