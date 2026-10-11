
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"log"
	"time"
        "github.com/joho/godotenv"
)

const identityURL = "https://health.googleapis.com/v4/users/me/identity"

func main() {
	if err := godotenv.Load(); err != nil {
		log.Fatal("не удалось загрузить .env: ", err)
	}

	token := os.Getenv("GOOGLE_HEALTH_ACCESS_TOKEN")
	if token == "" {
		log.Fatal("GOOGLE_HEALTH_ACCESS_TOKEN не задан")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		identityURL,
		nil,
	)
	if err != nil {
		fatal(err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fatal(err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		fatal(err)
	}

	fmt.Printf("HTTP status: %s\n", resp.Status)

	switch resp.StatusCode {
	case http.StatusOK:
		var result map[string]any
		if err := json.Unmarshal(body, &result); err != nil {
			fatal(fmt.Errorf("invalid JSON response: %w", err))
		}
		fmt.Println("Google Health API: accessible")
		fmt.Println("Google Health identity: linked")
		fmt.Printf("Response fields: %v\n", keys(result))

	case http.StatusUnauthorized:
		fmt.Println("Access token is invalid or expired.")
		os.Exit(1)

	case http.StatusForbidden:
		fmt.Println("Access denied: check API enablement, OAuth scopes and permissions.")
		fmt.Println(safeError(body))
		os.Exit(1)

	case http.StatusBadRequest:
		fmt.Println("Request rejected. The account may not be linked to Google Health.")
		fmt.Println(safeError(body))
		os.Exit(1)

	default:
		fmt.Println("API check failed.")
		fmt.Println(safeError(body))
		os.Exit(1)
	}
}

func keys(m map[string]any) []string {
	result := make([]string, 0, len(m))
	for k := range m {
		result = append(result, k)
	}
	return result
}

func safeError(body []byte) string {
	// Error bodies may contain account details. Print only Google's
	// structured error message, not the entire response.
	var envelope struct {
		Error struct {
			Status  string `json:"status"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &envelope) == nil {
		if envelope.Error.Status != "" || envelope.Error.Message != "" {
			return fmt.Sprintf("%s: %s",
				envelope.Error.Status, envelope.Error.Message)
		}
	}
	return "No structured error details available."
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "Error:", err)
	os.Exit(1)
}
