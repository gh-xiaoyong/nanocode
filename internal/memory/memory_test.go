package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadReturnsEmptySourcesWhenFilesAreMissing(t *testing.T) {
	workspace := t.TempDir()
	home := t.TempDir()
	loader := NewLoaderWithHome(workspace, home, DefaultMaxRunes)

	mem, err := loader.Load()

	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(mem.LoadedSources()) != 0 {
		t.Fatalf("expected no loaded memory, got %#v", mem.LoadedSources())
	}
}

func TestLoadReadsProjectMemory(t *testing.T) {
	workspace := t.TempDir()
	home := t.TempDir()
	writeFile(t, filepath.Join(workspace, ProjectFileName), "project rule")
	loader := NewLoaderWithHome(workspace, home, DefaultMaxRunes)

	mem, err := loader.Load()

	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	loaded := mem.LoadedSources()
	if len(loaded) != 1 || loaded[0].Kind != SourceProject || loaded[0].Content != "project rule" {
		t.Fatalf("expected project memory, got %#v", loaded)
	}
}

func TestLoadReadsUserMemory(t *testing.T) {
	workspace := t.TempDir()
	home := t.TempDir()
	writeFile(t, filepath.Join(home, userMemoryDirName, UserFileName), "user rule")
	loader := NewLoaderWithHome(workspace, home, DefaultMaxRunes)

	mem, err := loader.Load()

	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	loaded := mem.LoadedSources()
	if len(loaded) != 1 || loaded[0].Kind != SourceUser || loaded[0].Content != "user rule" {
		t.Fatalf("expected user memory, got %#v", loaded)
	}
}

func TestLoadKeepsUserThenProjectOrder(t *testing.T) {
	workspace := t.TempDir()
	home := t.TempDir()
	writeFile(t, filepath.Join(home, userMemoryDirName, UserFileName), "user rule")
	writeFile(t, filepath.Join(workspace, ProjectFileName), "project rule")
	loader := NewLoaderWithHome(workspace, home, DefaultMaxRunes)

	mem, err := loader.Load()

	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	loaded := mem.LoadedSources()
	if len(loaded) != 2 || loaded[0].Kind != SourceUser || loaded[1].Kind != SourceProject {
		t.Fatalf("expected user then project memory, got %#v", loaded)
	}
}

func TestLoadTruncatesLongMemory(t *testing.T) {
	workspace := t.TempDir()
	home := t.TempDir()
	writeFile(t, filepath.Join(workspace, ProjectFileName), strings.Repeat("好", 10))
	loader := NewLoaderWithHome(workspace, home, 4)

	mem, err := loader.Load()

	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	loaded := mem.LoadedSources()
	if len(loaded) != 1 || !loaded[0].Truncated {
		t.Fatalf("expected truncated memory, got %#v", loaded)
	}
	if !strings.Contains(loaded[0].Content, "记忆内容已截断") {
		t.Fatalf("expected truncation notice, got %q", loaded[0].Content)
	}
}

func TestLoadReturnsReadError(t *testing.T) {
	workspace := t.TempDir()
	home := t.TempDir()
	path := filepath.Join(workspace, ProjectFileName)
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("create directory at memory path: %v", err)
	}
	loader := NewLoaderWithHome(workspace, home, DefaultMaxRunes)

	_, err := loader.Load()

	if err == nil {
		t.Fatal("expected read error")
	}
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}
