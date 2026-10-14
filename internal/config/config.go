package config

import (
	"os"

	"github.com/joho/godotenv"
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
		RefreshToken: os.Getenv("GOOGLE_HEALTH_REFRESH_TOKEN"),
	}
}
