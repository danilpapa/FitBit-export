package report

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const notesDir = "exports/notes"

func SaveNote(day time.Time, text string) error {
	if err := os.MkdirAll(notesDir, 0o700); err != nil {
		return fmt.Errorf("create notes directory: %w", err)
	}

	file, err := os.OpenFile(notePath(day), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open note: %w", err)
	}
	defer file.Close()

	if _, err := fmt.Fprintln(file, text); err != nil {
		return fmt.Errorf("write note: %w", err)
	}
	return nil
}

func LoadNote(day time.Time) (string, error) {
	content, err := os.ReadFile(notePath(day))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read note: %w", err)
	}
	return strings.ReplaceAll(strings.TrimSpace(string(content)), "\n", "; "), nil
}

func notePath(day time.Time) string {
	return filepath.Join(notesDir, dateKey(day)+".txt")
}
