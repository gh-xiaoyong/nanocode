package diffview

import (
	"strings"
	"testing"
)

func TestUnifiedDiffForExistingFile(t *testing.T) {
	diff := UnifiedDiff("hello.txt", "one\ntwo\nthree\n", "one\nTWO\nthree\n", true)

	if !strings.Contains(diff, "--- a/hello.txt") || !strings.Contains(diff, "+++ b/hello.txt") {
		t.Fatalf("expected unified diff headers, got %q", diff)
	}
	if !strings.Contains(diff, "-two") || !strings.Contains(diff, "+TWO") {
		t.Fatalf("expected changed lines, got %q", diff)
	}
}

func TestUnifiedDiffForNewFileUsesDevNull(t *testing.T) {
	diff := UnifiedDiff("demo.txt", "", "hello\nworld\n", false)

	if !strings.Contains(diff, "--- /dev/null") || !strings.Contains(diff, "+++ b/demo.txt") {
		t.Fatalf("expected new file headers, got %q", diff)
	}
	if !strings.Contains(diff, "+hello") || !strings.Contains(diff, "+world") {
		t.Fatalf("expected added lines, got %q", diff)
	}
}

func TestSummarizeCountsAddedAndRemovedLines(t *testing.T) {
	diff := UnifiedDiff("hello.txt", "one\ntwo\n", "one\nthree\nfour\n", true)

	summary := Summarize(diff)

	if summary.Added != 2 || summary.Removed != 1 {
		t.Fatalf("unexpected summary: %#v\n%s", summary, diff)
	}
	if summary.IsNewFile {
		t.Fatalf("did not expect existing file to be treated as new")
	}
}

func TestSummarizeDetectsNewFile(t *testing.T) {
	summary := Summarize(UnifiedDiff("demo.txt", "", "hello\n", false))

	if !summary.IsNewFile || summary.Added != 1 || summary.Removed != 0 {
		t.Fatalf("unexpected new file summary: %#v", summary)
	}
}

func TestUnifiedDiffReturnsEmptyForNoChange(t *testing.T) {
	diff := UnifiedDiff("hello.txt", "same\n", "same\n", true)

	if diff != "" {
		t.Fatalf("expected no diff for unchanged content, got %q", diff)
	}
}
