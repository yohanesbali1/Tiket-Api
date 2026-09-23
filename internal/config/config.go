package config

import (
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	RedisAddr     string
	RedisPassword string
	RedisDB       int

	Port             int
	MaxActive        int
	Lease            time.Duration
	AverageSession   time.Duration
	PromotionInterval time.Duration
}

func Load() *Config {
	godotenv.Load()

	return &Config{
		RedisAddr:         envStr("REDIS_ADDR", "localhost:6379"),
		RedisPassword:     envStr("REDIS_PASSWORD", ""),
		RedisDB:           envInt("REDIS_DB", 0),
		Port:              envInt("PORT", 8080),
		MaxActive:         envInt("MAX_ACTIVE", 100),
		Lease:             time.Duration(envInt("LEASE_SECONDS", 60)) * time.Second,
		AverageSession:    time.Duration(envInt("AVG_SESSION_SECONDS", 300)) * time.Second,
		PromotionInterval: time.Duration(envInt("PROMOTION_INTERVAL_SECONDS", 1)) * time.Second,
	}
}

func envStr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}
