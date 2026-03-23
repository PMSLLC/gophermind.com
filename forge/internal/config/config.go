package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

type Config struct {
	RootDir        string
	Model          string
	OllamaBaseURL  string
	ApprovalMode   string
	MaxIterations  int
	LogLevel       string
	Verbose        bool
	SessionPath    string
	LogsDir        string
	SummariesDir   string
	CommandTimeout int
}

func Load() (Config, error) {
	root, err := os.Getwd()
	if err != nil {
		return Config{}, fmt.Errorf("getwd: %w", err)
	}

	cfg := Config{
		RootDir:        root,
		Model:          envOr("FORGE_MODEL", "gpt-oss:20b"),
		OllamaBaseURL:  envOr("FORGE_OLLAMA_BASE_URL", "http://localhost:11434"),
		ApprovalMode:   envOr("FORGE_APPROVAL_MODE", "suggest"),
		MaxIterations:  envIntOr("FORGE_MAX_ITERATIONS", 3),
		LogLevel:       envOr("FORGE_LOG_LEVEL", "info"),
		CommandTimeout: envIntOr("FORGE_COMMAND_TIMEOUT_SECONDS", 90),
	}

	forgeDir := filepath.Join(cfg.RootDir, ".forge")
	cfg.SessionPath = filepath.Join(forgeDir, "session.json")
	cfg.LogsDir = filepath.Join(forgeDir, "logs")
	cfg.SummariesDir = filepath.Join(forgeDir, "summaries")

	return cfg, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envIntOr(key string, fallback int) int {
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
