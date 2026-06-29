package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFromWorkspaceReadsSettingsFile(t *testing.T) {
	workspace := t.TempDir()
	writeFile(t, filepath.Join(workspace, settingsFileName), `{
  "model": {
    "base_url": "https://example.com/v1",
    "api_key": "file-key",
    "model": "test-model",
    "temperature": 0.4,
    "max_tokens": 1234,
    "context_window": 1000000
  }
}`)

	cfg, err := LoadFromWorkspace(workspace)
	if err != nil {
		t.Fatalf("LoadFromWorkspace() error = %v", err)
	}

	if cfg.BaseURL != "https://example.com/v1" {
		t.Fatalf("expected base url from settings file, got %q", cfg.BaseURL)
	}
	if cfg.APIKey != "file-key" {
		t.Fatalf("expected api key from settings file, got %q", cfg.APIKey)
	}
	if cfg.Model != "test-model" {
		t.Fatalf("expected model from settings file, got %q", cfg.Model)
	}
	if cfg.Temperature != 0.4 {
		t.Fatalf("expected temperature from settings file, got %v", cfg.Temperature)
	}
	if cfg.MaxTokens != 1234 {
		t.Fatalf("expected max tokens from settings file, got %d", cfg.MaxTokens)
	}
	if cfg.ContextWindow != 1000000 {
		t.Fatalf("expected context window from settings file, got %d", cfg.ContextWindow)
	}
}

func TestLoadFromWorkspaceEnvOverridesSettings(t *testing.T) {
	workspace := t.TempDir()
	writeFile(t, filepath.Join(workspace, settingsFileName), `{
  "model": {
    "base_url": "https://file.example/v1",
    "api_key": "file-key",
    "model": "file-model",
    "temperature": 0.2,
    "max_tokens": 2048,
    "context_window": 64000
  }
}`)

	t.Setenv("NANOCODE_BASE_URL", "https://env.example/v1")
	t.Setenv("NANOCODE_API_KEY", "env-key")
	t.Setenv("NANOCODE_MODEL", "env-model")
	t.Setenv("NANOCODE_TEMPERATURE", "0.9")
	t.Setenv("NANOCODE_MAX_TOKENS", "999")
	t.Setenv("NANOCODE_CONTEXT_WINDOW", "200000")

	cfg, err := LoadFromWorkspace(workspace)
	if err != nil {
		t.Fatalf("LoadFromWorkspace() error = %v", err)
	}

	if cfg.BaseURL != "https://env.example/v1" {
		t.Fatalf("expected env base url, got %q", cfg.BaseURL)
	}
	if cfg.APIKey != "env-key" {
		t.Fatalf("expected env api key, got %q", cfg.APIKey)
	}
	if cfg.Model != "env-model" {
		t.Fatalf("expected env model, got %q", cfg.Model)
	}
	if cfg.Temperature != 0.9 {
		t.Fatalf("expected env temperature, got %v", cfg.Temperature)
	}
	if cfg.MaxTokens != 999 {
		t.Fatalf("expected env max tokens, got %d", cfg.MaxTokens)
	}
	if cfg.ContextWindow != 200000 {
		t.Fatalf("expected env context window, got %d", cfg.ContextWindow)
	}
}

func TestLoadFromWorkspaceUsesAPIKeyEnvFromSettings(t *testing.T) {
	workspace := t.TempDir()
	writeFile(t, filepath.Join(workspace, settingsFileName), `{
  "model": {
    "base_url": "https://example.com/v1",
    "api_key_env": "PROJECT_MODEL_KEY",
    "model": "test-model"
  }
}`)

	t.Setenv("PROJECT_MODEL_KEY", "env-from-settings")

	cfg, err := LoadFromWorkspace(workspace)
	if err != nil {
		t.Fatalf("LoadFromWorkspace() error = %v", err)
	}

	if cfg.APIKey != "env-from-settings" {
		t.Fatalf("expected api key from api_key_env, got %q", cfg.APIKey)
	}
}

func TestLoadFromWorkspaceReturnsTypedMissingAPIKeyError(t *testing.T) {
	workspace := t.TempDir()
	writeFile(t, filepath.Join(workspace, settingsFileName), `{"model":{"model":"test-model"}}`)

	_, err := LoadFromWorkspace(workspace)
	if err == nil {
		t.Fatal("expected missing API key error")
	}
	var configErr *ConfigError
	if !errors.As(err, &configErr) || configErr.Kind != ConfigErrorMissingAPIKey {
		t.Fatalf("expected missing API key ConfigError, got %T %v", err, err)
	}
}

func TestLoadFromWorkspaceReturnsTypedMissingModelError(t *testing.T) {
	workspace := t.TempDir()
	writeFile(t, filepath.Join(workspace, settingsFileName), `{"model":{"api_key":"test-key"}}`)

	_, err := LoadFromWorkspace(workspace)
	if err == nil {
		t.Fatal("expected missing model error")
	}
	var configErr *ConfigError
	if !errors.As(err, &configErr) || configErr.Kind != ConfigErrorMissingModel {
		t.Fatalf("expected missing model ConfigError, got %T %v", err, err)
	}
}

func TestLoadFromWorkspaceReturnsTypedSettingsParseError(t *testing.T) {
	workspace := t.TempDir()
	writeFile(t, filepath.Join(workspace, settingsFileName), `{"model":`)

	_, err := LoadFromWorkspace(workspace)
	if err == nil {
		t.Fatal("expected settings parse error")
	}
	var configErr *ConfigError
	if !errors.As(err, &configErr) || configErr.Kind != ConfigErrorSettingsParse || configErr.Path == "" {
		t.Fatalf("expected settings parse ConfigError, got %#v", err)
	}
}

func TestLoadFromWorkspaceReturnsTypedEnvParseError(t *testing.T) {
	workspace := t.TempDir()
	writeFile(t, filepath.Join(workspace, settingsFileName), `{"model":{"api_key":"test-key","model":"test-model"}}`)
	t.Setenv("NANOCODE_CONTEXT_WINDOW", "not-int")

	_, err := LoadFromWorkspace(workspace)
	if err == nil {
		t.Fatal("expected env parse error")
	}
	var configErr *ConfigError
	if !errors.As(err, &configErr) || configErr.Kind != ConfigErrorEnvParse {
		t.Fatalf("expected env parse ConfigError, got %#v", err)
	}
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file %s: %v", path, err)
	}
}
