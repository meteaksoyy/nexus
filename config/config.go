package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	Port string
	Env  string

	JWTSecret        string
	JWTExpiryMinutes int
	JWTRefreshDays   int

	DatabaseURL string

	RedisURL string

	GithubToken string

	IBKRGatewayURL string
	IBKRUsername   string
	IBKRPassword   string

	RateLimitAuthed int
	RateLimitAPIKey int
	RateLimitWindow time.Duration

	OTELEndpoint string
}

func Load() *Config {
	return &Config{
		Port:             getEnv("PORT", "8080"),
		Env:              getEnv("ENV", "development"),
		JWTSecret:        mustEnv("JWT_SECRET"),
		JWTExpiryMinutes: getEnvInt("JWT_EXPIRY_MINUTES", 60),
		JWTRefreshDays:   getEnvInt("JWT_REFRESH_DAYS", 7),
		DatabaseURL:      mustEnv("DATABASE_URL"),
		RedisURL:         getEnv("REDIS_URL", "redis://localhost:6379"),
		GithubToken:      getEnv("GITHUB_TOKEN", ""),
		IBKRGatewayURL:   getEnv("IBKR_GATEWAY_URL", "https://localhost:5000"),
		IBKRUsername:     getEnv("IBKR_USERNAME", ""),
		IBKRPassword:     getEnv("IBKR_PASSWORD", ""),
		RateLimitAuthed:  getEnvInt("RATE_LIMIT_AUTHED", 100),
		RateLimitAPIKey:  getEnvInt("RATE_LIMIT_APIKEY", 500),
		RateLimitWindow:  time.Duration(getEnvInt("RATE_LIMIT_WINDOW_SECONDS", 60)) * time.Second,
		OTELEndpoint:     getEnv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4318"),
	}
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		panic("required env var not set: " + key)
	}
	return v
}

func getEnvInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}
