package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

const (
	envFile         = ".env"
	refreshTokenKey = "GOOGLE_HEALTH_REFRESH_TOKEN"
)

type Config struct {
	ClientID     string
	ClientSecret string
	RefreshToken string
}

func Load() Config {
	_ = godotenv.Load()

	return Config{
		ClientID:     os.Getenv("GOOGLE_HEALTH_CLIENT_ID"),
		ClientSecret: os.Getenv("GOOGLE_HEALTH_CLIENT_SECRET"),
		RefreshToken: os.Getenv(refreshTokenKey),
	}
}

func SaveRefreshToken(token string) error {
	return saveEnvValue(envFile, refreshTokenKey, token)
}

func saveEnvValue(path string, key string, value string) error {
	content, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read %s: %w", path, err)
	}

	var lines []string
	if existing := strings.TrimRight(string(content), "\n"); existing != "" {
		lines = strings.Split(existing, "\n")
	}

	entry := key + "=" + value
	replaced := false
	for i, line := range lines {
		if strings.HasPrefix(line, key+"=") {
			lines[i] = entry
			replaced = true
		}
	}
	if !replaced {
		lines = append(lines, entry)
	}

	temp := path + ".tmp"
	if err := os.WriteFile(temp, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", temp, err)
	}
	if err := os.Rename(temp, path); err != nil {
		os.Remove(temp)
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}
