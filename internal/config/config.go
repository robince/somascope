package config

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
)

type AuthMode string

const (
	AuthModeBYO      AuthMode = "byo"
	AuthModeBrokered AuthMode = "brokered"
)

type Config struct {
	Host       string
	Port       int
	DataDir    string
	DBPath     string
	ConfigPath string
	ExportsDir string
	RawDir     string
	LogsDir    string
	AuthMode   AuthMode
}

func Load() (Config, error) {
	defaultDir := DefaultDataDir()

	host := flag.String("host", envOrDefault("SOMASCOPE_HOST", "127.0.0.1"), "Host to bind")
	port := flag.Int("port", envOrDefaultInt("SOMASCOPE_PORT", 8080), "Port to bind")
	dataDir := flag.String("data-dir", envOrDefault("SOMASCOPE_DATA_DIR", defaultDir), "Data directory")
	authMode := flag.String("auth-mode", envOrDefault("SOMASCOPE_AUTH_MODE", string(AuthModeBYO)), "OAuth mode: byo or brokered")
	flag.Parse()

	cfg := Config{
		Host:       *host,
		Port:       *port,
		DataDir:    *dataDir,
		DBPath:     filepath.Join(*dataDir, "somascope.db"),
		ConfigPath: filepath.Join(*dataDir, "config.json"),
		ExportsDir: filepath.Join(*dataDir, "exports"),
		RawDir:     filepath.Join(*dataDir, "raw"),
		LogsDir:    filepath.Join(*dataDir, "logs"),
		AuthMode:   AuthMode(*authMode),
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func (c Config) Validate() error {
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("invalid port: %d", c.Port)
	}

	switch c.AuthMode {
	case AuthModeBYO, AuthModeBrokered:
		return nil
	default:
		return fmt.Errorf("invalid auth mode %q", c.AuthMode)
	}
}

func (c Config) EnsureLayout() error {
	if c.DataDir == "" {
		return errors.New("data dir must not be empty")
	}

	for _, dir := range []string{c.DataDir, c.ExportsDir, c.RawDir, c.LogsDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	return nil
}

func DefaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ".somascope"
	}
	return filepath.Join(home, ".somascope")
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envOrDefaultInt(key string, fallback int) int {
	if value := os.Getenv(key); value != "" {
		var parsed int
		if _, err := fmt.Sscanf(value, "%d", &parsed); err == nil {
			return parsed
		}
		log.Printf("warning: invalid integer for %s=%q, falling back to %d", key, value, fallback)
	}
	return fallback
}
