package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	defaultBaseURL       = "https://api.openai.com/v1"
	defaultTemperature   = 0.2
	defaultMaxTokens     = 4096
	defaultContextWindow = 32000
	settingsFileName     = "settings.json"
)

type ConfigErrorKind string

const (
	ConfigErrorMissingAPIKey ConfigErrorKind = "missing_api_key"
	ConfigErrorMissingModel  ConfigErrorKind = "missing_model"
	ConfigErrorSettingsParse ConfigErrorKind = "settings_parse"
	ConfigErrorEnvParse      ConfigErrorKind = "env_parse"
)

type ConfigError struct {
	Kind  ConfigErrorKind
	Path  string
	Field string
	Cause error
}

func (e *ConfigError) Error() string {
	switch e.Kind {
	case ConfigErrorMissingAPIKey:
		return "missing API key"
	case ConfigErrorMissingModel:
		return "missing model"
	case ConfigErrorSettingsParse:
		return fmt.Sprintf("parse settings file %s: %v", e.Path, e.Cause)
	case ConfigErrorEnvParse:
		return fmt.Sprintf("parse %s: %v", e.Field, e.Cause)
	default:
		if e.Cause != nil {
			return e.Cause.Error()
		}
		return "config error"
	}
}

func (e *ConfigError) Unwrap() error {
	return e.Cause
}

type ModelConfig struct {
	BaseURL       string
	APIKey        string
	Model         string
	Temperature   float32
	MaxTokens     int
	ContextWindow int
}

type fileConfig struct {
	Model modelSettings `json:"model"`
}

type modelSettings struct {
	BaseURL       string   `json:"base_url"`
	APIKey        string   `json:"api_key"`
	APIKeyEnv     string   `json:"api_key_env"`
	Model         string   `json:"model"`
	Temperature   *float32 `json:"temperature"`
	MaxTokens     *int     `json:"max_tokens"`
	ContextWindow *int     `json:"context_window"`
}

func Load() (ModelConfig, error) {
	// 默认从当前工作目录读取项目级配置，符合 nanocode 在仓库根目录运行的使用方式。
	workspaceRoot, err := os.Getwd()
	if err != nil {
		return ModelConfig{}, fmt.Errorf("get working directory: %w", err)
	}

	return LoadFromWorkspace(workspaceRoot)
}

func LoadFromWorkspace(workspaceRoot string) (ModelConfig, error) {
	fileSettings, err := loadSettingsFile(workspaceRoot)
	if err != nil {
		return ModelConfig{}, err
	}

	// 配置优先级：环境变量 > settings.json > 默认值。
	// 这样既保留项目级可复制配置，也允许本机环境做临时覆盖。
	cfg := ModelConfig{
		BaseURL:       firstNonEmpty(envValue("NANOCODE_BASE_URL", "OPENAI_BASE_URL"), fileSettings.BaseURL, defaultBaseURL),
		APIKey:        resolveAPIKey(fileSettings),
		Model:         firstNonEmpty(envValue("NANOCODE_MODEL", "OPENAI_MODEL"), fileSettings.Model),
		Temperature:   defaultTemperature,
		MaxTokens:     defaultMaxTokens,
		ContextWindow: defaultContextWindow,
	}

	if fileSettings.Temperature != nil {
		cfg.Temperature = *fileSettings.Temperature
	}
	if fileSettings.MaxTokens != nil {
		cfg.MaxTokens = *fileSettings.MaxTokens
	}
	if fileSettings.ContextWindow != nil {
		cfg.ContextWindow = *fileSettings.ContextWindow
	}

	if raw := envValue("NANOCODE_TEMPERATURE", "OPENAI_TEMPERATURE"); raw != "" {
		value, parseErr := strconv.ParseFloat(raw, 32)
		if parseErr != nil {
			return ModelConfig{}, &ConfigError{Kind: ConfigErrorEnvParse, Field: "temperature", Cause: parseErr}
		}
		cfg.Temperature = float32(value)
	}

	if raw := envValue("NANOCODE_MAX_TOKENS", "OPENAI_MAX_TOKENS"); raw != "" {
		value, parseErr := strconv.Atoi(raw)
		if parseErr != nil {
			return ModelConfig{}, &ConfigError{Kind: ConfigErrorEnvParse, Field: "max tokens", Cause: parseErr}
		}
		cfg.MaxTokens = value
	}

	if raw := envValue("NANOCODE_CONTEXT_WINDOW", "OPENAI_CONTEXT_WINDOW"); raw != "" {
		value, parseErr := strconv.Atoi(raw)
		if parseErr != nil {
			return ModelConfig{}, &ConfigError{Kind: ConfigErrorEnvParse, Field: "context window", Cause: parseErr}
		}
		cfg.ContextWindow = value
	}

	if strings.TrimSpace(cfg.APIKey) == "" {
		return ModelConfig{}, &ConfigError{Kind: ConfigErrorMissingAPIKey}
	}

	// model 必须显式指定，避免静默调用到错误模型。
	if strings.TrimSpace(cfg.Model) == "" {
		return ModelConfig{}, &ConfigError{Kind: ConfigErrorMissingModel}
	}

	return cfg, nil
}

func loadSettingsFile(workspaceRoot string) (modelSettings, error) {
	// settings.json 是本地真实配置文件；不存在时返回空配置，由环境变量或默认值补齐。
	settingsPath := filepath.Join(workspaceRoot, settingsFileName)
	content, err := os.ReadFile(settingsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return modelSettings{}, nil
		}
		return modelSettings{}, fmt.Errorf("read settings file: %w", err)
	}

	var cfg fileConfig
	if err := json.Unmarshal(content, &cfg); err != nil {
		return modelSettings{}, &ConfigError{Kind: ConfigErrorSettingsParse, Path: settingsPath, Cause: err}
	}

	return cfg.Model, nil
}

func resolveAPIKey(fileSettings modelSettings) string {
	// API key 优先从环境变量读取；文件里也可以只声明 api_key_env，避免明文写 key。
	if value := envValue("NANOCODE_API_KEY", "OPENAI_API_KEY"); value != "" {
		return value
	}

	if fileSettings.APIKeyEnv != "" {
		if value := strings.TrimSpace(os.Getenv(fileSettings.APIKeyEnv)); value != "" {
			return value
		}
	}

	return strings.TrimSpace(fileSettings.APIKey)
}

func envValue(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}

	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}

	return ""
}
