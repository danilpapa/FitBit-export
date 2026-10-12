package output

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"fitbit-export/internal/report"
)

func SaveJSON(dir string, data report.Report) (string, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("create output directory: %w", err)
	}

	filename := "report-" + time.Now().UTC().Format("20060102-150405") + ".json"
	path := filepath.Join(dir, filename)

	content, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode report: %w", err)
	}

	if err := os.WriteFile(path, content, 0600); err != nil {
		return "", fmt.Errorf("write report: %w", err)
	}

	return path, nil
}