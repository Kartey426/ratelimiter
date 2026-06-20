package config

import(
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct{
	Algorithm string
	Port string
	RedisAddr string
	//sliding window
	Limit int
	WindowSeconds int
	//tocken bucket
	Capacity float64
	RefillRate float64
}

func Load() (Config, error) {
	cfg := Config{
		Port:          getEnv("PORT", "8081"),
		RedisAddr:     getEnv("REDIS_ADDR", "localhost:6379"),
		Algorithm:     getEnv("ALGORITHM", "sliding_window"),
		Limit:         10,
		WindowSeconds: 60,
		Capacity:      10,
		RefillRate:    1.0,
	}

	var err error
	if cfg.Limit, err = getEnvInt("LIMIT", cfg.Limit); err != nil {
		return Config{}, err
	}
	if cfg.WindowSeconds, err = getEnvInt("WINDOW_SECONDS", cfg.WindowSeconds); err != nil {
		return Config{}, err
	}
	if cfg.Capacity, err = getEnvFloat("CAPACITY", cfg.Capacity); err != nil {
		return Config{}, err
	}
	if cfg.RefillRate, err = getEnvFloat("REFILL_RATE", cfg.RefillRate); err != nil {
		return Config{}, err
	}

	if cfg.Algorithm != "token_bucket" && cfg.Algorithm != "sliding_window" {
		return Config{}, fmt.Errorf("config: ALGORITHM must be 'token_bucket' or 'sliding_window', got %q", cfg.Algorithm)
	}

	return cfg, nil
}

// WindowDuration converts WindowSeconds to a time.Duration for
// limiter.SlidingWindowConfig.WindowSize.
func (c Config) WindowDuration() time.Duration {
	return time.Duration(c.WindowSeconds) * time.Second
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("config: %s must be an integer, got %q: %w", key, v, err)
	}
	return n, nil
}

func getEnvFloat(key string, fallback float64) (float64, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, fmt.Errorf("config: %s must be a number, got %q: %w", key, v, err)
	}
	return f, nil
}