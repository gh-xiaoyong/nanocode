package main

import (
	"testing"

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
