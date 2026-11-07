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

	fatSecretTokenKey  = "FATSECRET_ACCESS_TOKEN"
	fatSecretSecretKey = "FATSECRET_ACCESS_SECRET"
)

type Config struct {
	ClientID     string
	ClientSecret string
	RefreshToken string
	FatSecret    FatSecret
	Telegram     Telegram
}

type Telegram struct {
	BotToken string
	ChatID   string
}

type FatSecret struct {
	ConsumerKey    string
	ConsumerSecret string
	AccessToken    string
	AccessSecret   string
}

func (f FatSecret) Linked() bool {
	return f.ConsumerKey != "" && f.ConsumerSecret != "" && f.AccessToken != "" && f.AccessSecret != ""
}

func Load() Config {
	_ = godotenv.Load()

	return Config{
		ClientID:     os.Getenv("GOOGLE_HEALTH_CLIENT_ID"),
		ClientSecret: os.Getenv("GOOGLE_HEALTH_CLIENT_SECRET"),
		RefreshToken: os.Getenv(refreshTokenKey),
		FatSecret: FatSecret{
			ConsumerKey:    os.Getenv("FATSECRET_CONSUMER_KEY"),
			ConsumerSecret: os.Getenv("FATSECRET_CONSUMER_SECRET"),
			AccessToken:    os.Getenv(fatSecretTokenKey),
			AccessSecret:   os.Getenv(fatSecretSecretKey),
		},
		Telegram: Telegram{
			BotToken: os.Getenv("TELEGRAM_BOT_TOKEN"),
			ChatID:   os.Getenv("TELEGRAM_CHAT_ID"),
		},
	}
}

func SaveRefreshToken(token string) error {
	return saveEnvValue(envFile, refreshTokenKey, token)
}

func SaveFatSecretToken(token string, secret string) error {
	if err := saveEnvValue(envFile, fatSecretTokenKey, token); err != nil {
		return err
	}
	return saveEnvValue(envFile, fatSecretSecretKey, secret)
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
