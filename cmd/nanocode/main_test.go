package main

import (
	"strings"
	"testing"

	"nanocode/internal/config"
	"nanocode/internal/session"
)

func TestLoadOrCreateSessionCreatesNewSession(t *testing.T) {
	store := session.NewStore(t.TempDir())

	created, err := loadOrCreateSession(store, "default", "test-model", "workspace", false, "")
	if err != nil {
		t.Fatalf("loadOrCreateSession() error = %v", err)
	}
	if created.ID == "" {
		t.Fatal("expected created session id")
	}
}

func TestLoadOrCreateSessionResumesByID(t *testing.T) {
	store := session.NewStore(t.TempDir())
	created, err := store.Create("default", "test-model", "workspace")
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}

	loaded, err := loadOrCreateSession(store, "default", "test-model", "workspace", false, created.ID)
	if err != nil {
		t.Fatalf("loadOrCreateSession() error = %v", err)
	}
	if loaded.ID != created.ID {
		t.Fatalf("expected resumed id %q, got %q", created.ID, loaded.ID)
	}
}

func TestLoadOrCreateSessionContinuesLatest(t *testing.T) {
	store := session.NewStore(t.TempDir())
	created, err := store.Create("default", "test-model", "workspace")
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}

	loaded, err := loadOrCreateSession(store, "default", "test-model", "workspace", true, "")
	if err != nil {
		t.Fatalf("loadOrCreateSession() error = %v", err)
	}
	if loaded.ID != created.ID {
		t.Fatalf("expected latest id %q, got %q", created.ID, loaded.ID)
	}
}

func TestFormatFatalErrorUsesFriendlyMessage(t *testing.T) {
	rendered := formatFatalError(&config.ConfigError{Kind: config.ConfigErrorMissingAPIKey})

	if !strings.Contains(rendered, "配置错误：缺少 API Key") {
		t.Fatalf("expected friendly config error, got %q", rendered)
	}
	if strings.Contains(rendered, "main.go") || strings.Contains(rendered, "Fatal") {
		t.Fatalf("expected no log.Fatal prefix, got %q", rendered)
	}
}

func TestLoadOrCreateSessionResumeMissingCanBeFormatted(t *testing.T) {
	store := session.NewStore(t.TempDir())

	_, err := loadOrCreateSession(store, "default", "test-model", "workspace", false, "missing")
	if err == nil {
		t.Fatal("expected missing session error")
	}
	rendered := formatFatalError(err)
	if !strings.Contains(rendered, "错误") || !strings.Contains(rendered, "missing") {
		t.Fatalf("expected friendly fallback for missing session, got %q", rendered)
	}
}
