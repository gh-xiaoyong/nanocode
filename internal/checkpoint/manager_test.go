package checkpoint

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckpointRestoresExistingFile(t *testing.T) {
	manager := NewManager()
	filePath := filepath.Join(t.TempDir(), "hello.txt")
	if err := os.WriteFile(filePath, []byte("before"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	if err := manager.SaveBeforeWrite(filePath); err != nil {
		t.Fatalf("SaveBeforeWrite() error = %v", err)
	}
	if err := os.WriteFile(filePath, []byte("after"), 0o644); err != nil {
		t.Fatalf("rewrite fixture: %v", err)
	}

	message, err := manager.UndoLast(context.Background())
	if err != nil {
		t.Fatalf("UndoLast() error = %v", err)
	}
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read restored file: %v", err)
	}
	if string(content) != "before" {
		t.Fatalf("expected restored content, got %q", string(content))
	}
	if !strings.Contains(message, "已恢复文件") {
		t.Fatalf("expected restore message, got %q", message)
	}
}

func TestCheckpointRemovesCreatedFile(t *testing.T) {
	manager := NewManager()
	filePath := filepath.Join(t.TempDir(), "new.txt")

	if err := manager.SaveBeforeWrite(filePath); err != nil {
		t.Fatalf("SaveBeforeWrite() error = %v", err)
	}
	if err := os.WriteFile(filePath, []byte("created"), 0o644); err != nil {
		t.Fatalf("write created file: %v", err)
	}

	message, err := manager.UndoLast(context.Background())
	if err != nil {
		t.Fatalf("UndoLast() error = %v", err)
	}
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Fatalf("expected created file to be removed, stat err = %v", err)
	}
	if !strings.Contains(message, "已撤销新建文件") {
		t.Fatalf("expected remove message, got %q", message)
	}
}
