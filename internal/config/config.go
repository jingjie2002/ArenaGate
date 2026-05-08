package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	Addr               string
	CoreRankHTTP       string
	AuthTokenPrefix    string
	ServerNotice       string
	MaintenanceEnabled bool
	MaintenanceMessage string
	IdleTimeout        time.Duration
	MatchPollInterval  time.Duration
	MaxMessageBytes    int64
	SessionRateLimit   int
}

func FromEnv() Config {
	return Config{
		Addr:               getenv("GATEWAY_ADDR", "127.0.0.1:18082"),
		CoreRankHTTP:       trimRightSlash(getenv("CORE_RANK_HTTP", "http://127.0.0.1:8081")),
		AuthTokenPrefix:    getenv("AUTH_TOKEN_PREFIX", "dev-token:"),
		ServerNotice:       getenv("SERVER_NOTICE", ""),
		MaintenanceEnabled: getenvBool("MAINTENANCE_ENABLED", false),
		MaintenanceMessage: getenv("MAINTENANCE_MESSAGE", "server is under maintenance"),
		IdleTimeout:        time.Duration(getenvInt("IDLE_TIMEOUT_MS", 30000)) * time.Millisecond,
		MatchPollInterval:  time.Duration(getenvInt("MATCH_POLL_INTERVAL_MS", 500)) * time.Millisecond,
		MaxMessageBytes:    int64(getenvInt("MAX_MESSAGE_BYTES", 32768)),
		SessionRateLimit:   getenvInt("SESSION_RATE_LIMIT", 20),
	}
}

func getenv(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func getenvInt(key string, fallback int) int {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func getenvBool(key string, fallback bool) bool {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return value
}

func trimRightSlash(value string) string {
	for len(value) > 0 && value[len(value)-1] == '/' {
		value = value[:len(value)-1]
	}
	return value
}
