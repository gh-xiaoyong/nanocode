package prompt

import (
	"strings"
	"testing"

	"nanocode/internal/memory"
	"nanocode/internal/permission"
)

func TestBuildSystemPromptOmitsMemoryBlockWhenEmpty(t *testing.T) {
	systemPrompt := BuildSystemPrompt(permission.ModeDefault, "D:/workspace", memory.Memory{})

	if strings.Contains(systemPrompt, "Memory:") {
		t.Fatalf("expected no memory block, got %q", systemPrompt)
	}
}

func TestBuildSystemPromptIncludesMemoryInStableOrder(t *testing.T) {
	mem := memory.Memory{Sources: []memory.Source{
		{Kind: memory.SourceUser, Path: "C:/Users/me/.nanocode/NANO.md", Exists: true, Content: "user rule"},
		{Kind: memory.SourceProject, Path: "D:/repo/NANO.md", Exists: true, Content: "project rule"},
	}}

	systemPrompt := BuildSystemPrompt(permission.ModeDefault, "D:/workspace", mem)

	userIndex := strings.Index(systemPrompt, "user rule")
	projectIndex := strings.Index(systemPrompt, "project rule")
	if userIndex < 0 || projectIndex < 0 {
		t.Fatalf("expected both memory contents, got %q", systemPrompt)
	}
	if projectIndex <= userIndex {
		t.Fatalf("expected project memory after user memory, got %q", systemPrompt)
	}
}
