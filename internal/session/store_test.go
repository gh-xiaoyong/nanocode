package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"
)

func TestStoreCreateSaveLoad(t *testing.T) {
	store := NewStore(t.TempDir())

	created, err := store.Create("default", "test-model", "workspace")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	created.Messages = []*schema.Message{schema.UserMessage("hello")}
	if err := store.Save(created); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := store.Load(created.ID)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.ID != created.ID {
		t.Fatalf("expected id %q, got %q", created.ID, loaded.ID)
	}
	if len(loaded.Messages) != 1 || loaded.Messages[0].Content != "hello" {
		t.Fatalf("expected saved message, got %#v", loaded.Messages)
	}
}

func TestStoreLoadLatest(t *testing.T) {
	store := NewStore(t.TempDir())

	older, err := store.Create("default", "test-model", "workspace")
	if err != nil {
		t.Fatalf("Create older: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	newer, err := store.Create("default", "test-model", "workspace")
	if err != nil {
		t.Fatalf("Create newer: %v", err)
	}

	latest, err := store.LoadLatest()
	if err != nil {
		t.Fatalf("LoadLatest() error = %v", err)
	}
	if latest.ID != newer.ID {
		t.Fatalf("expected latest %q, got %q; older was %q", newer.ID, latest.ID, older.ID)
	}
}

func TestStoreLoadMissingSessionFails(t *testing.T) {
	store := NewStore(t.TempDir())

	_, err := store.Load("missing")
	if err == nil {
		t.Fatal("expected missing session to fail")
	}
}

func TestStoreUsesNanocodeSessionDirectory(t *testing.T) {
	root := t.TempDir()
	store := NewStore(root)

	created, err := store.Create("default", "test-model", root)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	expected := filepath.Join(root, ".nanocode", "sessions", created.ID+".json")
	if _, err := os.Stat(expected); err != nil {
		t.Fatalf("expected session file at %s: %v", expected, err)
	}
}
