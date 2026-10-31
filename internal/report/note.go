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

// SaveNote appends a self-reported line to the note for the given day.
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

// LoadNote returns the note for the given day, or "" when there is none.
func LoadNote(day time.Time) (string, error) {
	content, err := os.ReadFile(notePath(day))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read note: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	return strings.Join(lines, "; "), nil
}

func notePath(day time.Time) string {
	return filepath.Join(notesDir, day.Format(dateKey)+".txt")
}
