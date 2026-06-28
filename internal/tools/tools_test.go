package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"

	"nanocode/internal/checkpoint"
	"nanocode/internal/permission"
)

func TestResolvePathRejectsEscape(t *testing.T) {
	workspace, err := permission.NewWorkspacePolicy(t.TempDir())
	if err != nil {
		t.Fatalf("create workspace policy: %v", err)
	}

	_, err = resolvePath(workspace, "..\\outside.txt")
	if err == nil {
		t.Fatal("expected resolvePath to reject parent escape")
	}
}

func TestReadFileToolReadsWorkspaceFile(t *testing.T) {
	rootDir := t.TempDir()
	filePath := filepath.Join(rootDir, "hello.txt")
	if err := os.WriteFile(filePath, []byte("hello nanocode"), 0o644); err != nil {
		t.Fatalf("write fixture file: %v", err)
	}

	workspace, err := permission.NewWorkspacePolicy(rootDir)
	if err != nil {
		t.Fatalf("create workspace policy: %v", err)
	}

	baseTool, err := newReadFileTool(workspace)
	if err != nil {
		t.Fatalf("create read_file tool: %v", err)
	}

	invokableTool, ok := baseTool.(einotool.InvokableTool)
	if !ok {
		t.Fatal("read_file tool should implement InvokableTool")
	}

	output, err := invokableTool.InvokableRun(context.Background(), `{"file_path":"hello.txt"}`)
	if err != nil {
		t.Fatalf("invoke read_file tool: %v", err)
	}

	if !strings.Contains(output, "hello nanocode") {
		t.Fatalf("unexpected read_file output: %q", output)
	}
}

func TestReadFileToolReadsWindowWithOffsetAndLimit(t *testing.T) {
	rootDir := t.TempDir()
	filePath := filepath.Join(rootDir, "lines.txt")
	content := strings.Join([]string{"line 1", "line 2", "line 3", "line 4"}, "\n")
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture file: %v", err)
	}

	workspace, err := permission.NewWorkspacePolicy(rootDir)
	if err != nil {
		t.Fatalf("create workspace policy: %v", err)
	}

	baseTool, err := newReadFileTool(workspace)
	if err != nil {
		t.Fatalf("create read_file tool: %v", err)
	}

	invokableTool, ok := baseTool.(einotool.InvokableTool)
	if !ok {
		t.Fatal("read_file tool should implement InvokableTool")
	}

	output, err := invokableTool.InvokableRun(context.Background(), `{"file_path":"lines.txt","offset":3,"limit":2}`)
	if err != nil {
		t.Fatalf("invoke read_file tool: %v", err)
	}

	if !strings.Contains(output, "lines 3-4") {
		t.Fatalf("expected output to describe requested line window, got %q", output)
	}
	if strings.Contains(output, "line 1") || strings.Contains(output, "line 2") {
		t.Fatalf("expected output to exclude earlier lines, got %q", output)
	}
	if !strings.Contains(output, "line 3") || !strings.Contains(output, "line 4") {
		t.Fatalf("expected output to include requested lines, got %q", output)
	}
}

func TestReadFileToolDefaultsToLimitedWindow(t *testing.T) {
	rootDir := t.TempDir()
	filePath := filepath.Join(rootDir, "large.txt")
	lines := make([]string, 0, defaultReadFileLineLimit+1)
	for i := 1; i <= defaultReadFileLineLimit+1; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	if err := os.WriteFile(filePath, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("write fixture file: %v", err)
	}

	workspace, err := permission.NewWorkspacePolicy(rootDir)
	if err != nil {
		t.Fatalf("create workspace policy: %v", err)
	}

	baseTool, err := newReadFileTool(workspace)
	if err != nil {
		t.Fatalf("create read_file tool: %v", err)
	}

	invokableTool, ok := baseTool.(einotool.InvokableTool)
	if !ok {
		t.Fatal("read_file tool should implement InvokableTool")
	}

	output, err := invokableTool.InvokableRun(context.Background(), `{"file_path":"large.txt"}`)
	if err != nil {
		t.Fatalf("invoke read_file tool: %v", err)
	}

	if !strings.Contains(output, "lines 1-2000") {
		t.Fatalf("expected output to read first 2000 lines, got %q", output)
	}
	if !strings.Contains(output, "truncated; use offset=2001, limit=2000 to continue") {
		t.Fatalf("expected output to mention continuation offset, got %q", output)
	}
	if strings.Contains(output, "line 2001") {
		t.Fatalf("expected default read to exclude line 2001, got %q", output)
	}
}

func TestReadFileToolAlwaysReadsLatestDiskContent(t *testing.T) {
	rootDir := t.TempDir()
	filePath := filepath.Join(rootDir, "fresh.txt")
	if err := os.WriteFile(filePath, []byte("first version"), 0o644); err != nil {
		t.Fatalf("write fixture file: %v", err)
	}

	workspace, err := permission.NewWorkspacePolicy(rootDir)
	if err != nil {
		t.Fatalf("create workspace policy: %v", err)
	}

	baseTool, err := newReadFileTool(workspace)
	if err != nil {
		t.Fatalf("create read_file tool: %v", err)
	}

	invokableTool, ok := baseTool.(einotool.InvokableTool)
	if !ok {
		t.Fatal("read_file tool should implement InvokableTool")
	}

	if _, err := invokableTool.InvokableRun(context.Background(), `{"file_path":"fresh.txt"}`); err != nil {
		t.Fatalf("invoke read_file tool first time: %v", err)
	}

	if err := os.WriteFile(filePath, []byte("second version"), 0o644); err != nil {
		t.Fatalf("rewrite fixture file: %v", err)
	}

	output, err := invokableTool.InvokableRun(context.Background(), `{"file_path":"fresh.txt"}`)
	if err != nil {
		t.Fatalf("invoke read_file tool second time: %v", err)
	}

	if !strings.Contains(output, "second version") || strings.Contains(output, "first version") {
		t.Fatalf("expected read_file to return latest disk content, got %q", output)
	}
}

func TestListDirectoryToolListsEntries(t *testing.T) {
	rootDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(rootDir, "internal"), 0o755); err != nil {
		t.Fatalf("create test directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "README.md"), []byte("doc"), 0o644); err != nil {
		t.Fatalf("create test file: %v", err)
	}

	workspace, err := permission.NewWorkspacePolicy(rootDir)
	if err != nil {
		t.Fatalf("create workspace policy: %v", err)
	}

	baseTool, err := newListDirectoryTool(workspace)
	if err != nil {
		t.Fatalf("create ls tool: %v", err)
	}

	invokableTool, ok := baseTool.(einotool.InvokableTool)
	if !ok {
		t.Fatal("ls tool should implement InvokableTool")
	}

	output, err := invokableTool.InvokableRun(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("invoke ls tool: %v", err)
	}

	if !strings.Contains(output, "README.md") {
		t.Fatalf("expected ls output to contain README.md, got %q", output)
	}
	if !strings.Contains(output, "internal/") {
		t.Fatalf("expected ls output to contain internal/, got %q", output)
	}
}

func TestGrepToolFindsMatchesRecursively(t *testing.T) {
	rootDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(rootDir, "pkg"), 0o755); err != nil {
		t.Fatalf("create nested directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "main.go"), []byte("package main\nconst Name = \"nanocode\"\n"), 0o644); err != nil {
		t.Fatalf("write root file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "pkg", "feature.go"), []byte("package pkg\nfunc Search() string { return \"NanoCode\" }\n"), 0o644); err != nil {
		t.Fatalf("write nested file: %v", err)
	}

	workspace, err := permission.NewWorkspacePolicy(rootDir)
	if err != nil {
		t.Fatalf("create workspace policy: %v", err)
	}

	baseTool, err := newGrepTool(workspace)
	if err != nil {
		t.Fatalf("create grep tool: %v", err)
	}

	invokableTool, ok := baseTool.(einotool.InvokableTool)
	if !ok {
		t.Fatal("grep tool should implement InvokableTool")
	}

	output, err := invokableTool.InvokableRun(context.Background(), `{"pattern":"nanocode"}`)
	if err != nil {
		t.Fatalf("invoke grep tool: %v", err)
	}

	if !strings.Contains(output, "main.go:2:") {
		t.Fatalf("expected grep output to contain main.go hit, got %q", output)
	}
	if !strings.Contains(output, "pkg/feature.go:2:") {
		t.Fatalf("expected grep output to contain nested file hit, got %q", output)
	}
}

func TestGrepToolRejectsWorkspaceEscape(t *testing.T) {
	rootDir := t.TempDir()
	workspace, err := permission.NewWorkspacePolicy(rootDir)
	if err != nil {
		t.Fatalf("create workspace policy: %v", err)
	}

	baseTool, err := newGrepTool(workspace)
	if err != nil {
		t.Fatalf("create grep tool: %v", err)
	}

	invokableTool, ok := baseTool.(einotool.InvokableTool)
	if !ok {
		t.Fatal("grep tool should implement InvokableTool")
	}

	_, err = invokableTool.InvokableRun(context.Background(), `{"pattern":"nanocode","path":"../outside"}`)
	if err == nil {
		t.Fatal("expected grep to reject workspace escape")
	}
}

func TestGrepToolFallsBackWhenPrimaryBackendFails(t *testing.T) {
	rootDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootDir, "hello.txt"), []byte("hello nanocode\n"), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	workspace, err := permission.NewWorkspacePolicy(rootDir)
	if err != nil {
		t.Fatalf("create workspace policy: %v", err)
	}

	baseTool, err := newGrepToolWithBackend(workspace, func(workspace *permission.WorkspacePolicy, resolvedPath string, input grepInput) (string, error) {
		return runGoGrep(workspace, resolvedPath, input)
	})
	if err != nil {
		t.Fatalf("create grep tool with fallback backend: %v", err)
	}

	invokableTool, ok := baseTool.(einotool.InvokableTool)
	if !ok {
		t.Fatal("grep tool should implement InvokableTool")
	}

	output, err := invokableTool.InvokableRun(context.Background(), `{"pattern":"nanocode"}`)
	if err != nil {
		t.Fatalf("invoke grep fallback tool: %v", err)
	}

	if !strings.Contains(output, "hello.txt:1: hello nanocode") {
		t.Fatalf("expected fallback grep output to contain file hit, got %q", output)
	}
}

func TestParseRipgrepJSONOutput(t *testing.T) {
	workspaceRoot := filepath.Clean(`D:\projects\nanocode`)
	raw := `{"type":"begin","data":{"path":{"text":"D:\\projects\\nanocode\\internal\\tools\\grep.go"}}}
{"type":"match","data":{"path":{"text":"D:\\projects\\nanocode\\internal\\tools\\grep.go"},"lines":{"text":"type grepInput struct {\n"},"line_number":12}}
{"type":"end","data":{"path":{"text":"D:\\projects\\nanocode\\internal\\tools\\grep.go"},"binary_offset":null,"stats":{"elapsed":{"secs":0,"nanos":1,"human":"0.000001s"},"searches":1,"searches_with_match":1,"bytes_searched":42,"bytes_printed":0,"matched_lines":1,"matches":1}}}`

	results, err := parseRipgrepJSONOutput(workspaceRoot, []byte(raw))
	if err != nil {
		t.Fatalf("parseRipgrepJSONOutput() error = %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected one parsed result, got %d", len(results))
	}
	if results[0] != "internal/tools/grep.go:12: type grepInput struct {" {
		t.Fatalf("unexpected parsed result: %q", results[0])
	}
}

func TestGlobToolFindsFilesByPatternSortedByModTime(t *testing.T) {
	rootDir := t.TempDir()
	srcDir := filepath.Join(rootDir, "src")
	nestedDir := filepath.Join(srcDir, "components")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatalf("create test directories: %v", err)
	}

	oldFile := filepath.Join(srcDir, "old.tsx")
	newFile := filepath.Join(nestedDir, "new.tsx")
	if err := os.WriteFile(oldFile, []byte("old"), 0o644); err != nil {
		t.Fatalf("write old file: %v", err)
	}
	if err := os.WriteFile(newFile, []byte("new"), 0o644); err != nil {
		t.Fatalf("write new file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "note.md"), []byte("note"), 0o644); err != nil {
		t.Fatalf("write non-matching file: %v", err)
	}

	oldTime := time.Now().Add(-2 * time.Hour)
	newTime := time.Now()
	if err := os.Chtimes(oldFile, oldTime, oldTime); err != nil {
		t.Fatalf("set old file time: %v", err)
	}
	if err := os.Chtimes(newFile, newTime, newTime); err != nil {
		t.Fatalf("set new file time: %v", err)
	}

	workspace, err := permission.NewWorkspacePolicy(rootDir)
	if err != nil {
		t.Fatalf("create workspace policy: %v", err)
	}

	baseTool, err := newGlobTool(workspace)
	if err != nil {
		t.Fatalf("create glob tool: %v", err)
	}

	invokableTool, ok := baseTool.(einotool.InvokableTool)
	if !ok {
		t.Fatal("glob tool should implement InvokableTool")
	}

	output, err := invokableTool.InvokableRun(context.Background(), `{"pattern":"**/*.tsx"}`)
	if err != nil {
		t.Fatalf("invoke glob tool: %v", err)
	}

	newIndex := strings.Index(output, "src/components/new.tsx")
	oldIndex := strings.Index(output, "src/old.tsx")
	if newIndex == -1 || oldIndex == -1 {
		t.Fatalf("expected glob output to include both tsx files, got %q", output)
	}
	if newIndex > oldIndex {
		t.Fatalf("expected newer file to appear first, got %q", output)
	}
	if strings.Contains(output, "note.md") {
		t.Fatalf("expected glob output to exclude non-matching file, got %q", output)
	}
}

func TestGlobToolTruncatesAtHardLimit(t *testing.T) {
	rootDir := t.TempDir()
	for i := 0; i < globMaxResults+1; i++ {
		fileName := filepath.Join(rootDir, fmt.Sprintf("file-%03d.tsx", i))
		if err := os.WriteFile(fileName, []byte("tsx"), 0o644); err != nil {
			t.Fatalf("write test file: %v", err)
		}
	}

	workspace, err := permission.NewWorkspacePolicy(rootDir)
	if err != nil {
		t.Fatalf("create workspace policy: %v", err)
	}

	baseTool, err := newGlobTool(workspace)
	if err != nil {
		t.Fatalf("create glob tool: %v", err)
	}

	invokableTool, ok := baseTool.(einotool.InvokableTool)
	if !ok {
		t.Fatal("glob tool should implement InvokableTool")
	}

	output, err := invokableTool.InvokableRun(context.Background(), `{"pattern":"**/*.tsx"}`)
	if err != nil {
		t.Fatalf("invoke glob tool: %v", err)
	}

	if !strings.Contains(output, "truncated: showing first 100 matches") {
		t.Fatalf("expected glob output to mention truncation, got %q", output)
	}
}

func TestGlobToolRejectsWorkspaceEscape(t *testing.T) {
	rootDir := t.TempDir()
	workspace, err := permission.NewWorkspacePolicy(rootDir)
	if err != nil {
		t.Fatalf("create workspace policy: %v", err)
	}

	baseTool, err := newGlobTool(workspace)
	if err != nil {
		t.Fatalf("create glob tool: %v", err)
	}

	invokableTool, ok := baseTool.(einotool.InvokableTool)
	if !ok {
		t.Fatal("glob tool should implement InvokableTool")
	}

	_, err = invokableTool.InvokableRun(context.Background(), `{"pattern":"**/*.tsx","path":"../outside"}`)
	if err == nil {
		t.Fatal("expected glob to reject workspace escape")
	}
}

func TestGitStatusToolReportsBranchAndDiffSummary(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}

	rootDir := t.TempDir()
	runGitTestCommand(t, rootDir, "init")
	runGitTestCommand(t, rootDir, "config", "user.email", "test@example.com")
	runGitTestCommand(t, rootDir, "config", "user.name", "Test User")

	filePath := filepath.Join(rootDir, "tracked.txt")
	if err := os.WriteFile(filePath, []byte("first\n"), 0o644); err != nil {
		t.Fatalf("write tracked file: %v", err)
	}
	runGitTestCommand(t, rootDir, "add", "tracked.txt")
	runGitTestCommand(t, rootDir, "commit", "-m", "initial")

	if err := os.WriteFile(filePath, []byte("first\nsecond\n"), 0o644); err != nil {
		t.Fatalf("modify tracked file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "untracked.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatalf("write untracked file: %v", err)
	}

	workspace, err := permission.NewWorkspacePolicy(rootDir)
	if err != nil {
		t.Fatalf("create workspace policy: %v", err)
	}

	baseTool, err := newGitStatusTool(workspace)
	if err != nil {
		t.Fatalf("create git_status tool: %v", err)
	}

	invokableTool, ok := baseTool.(einotool.InvokableTool)
	if !ok {
		t.Fatal("git_status tool should implement InvokableTool")
	}

	output, err := invokableTool.InvokableRun(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("invoke git_status tool: %v", err)
	}

	if !strings.Contains(output, "##") {
		t.Fatalf("expected git status output to include branch header, got %q", output)
	}
	if !strings.Contains(output, " M tracked.txt") {
		t.Fatalf("expected git status output to include modified file, got %q", output)
	}
	if !strings.Contains(output, "?? untracked.txt") {
		t.Fatalf("expected git status output to include untracked file, got %q", output)
	}
	if !strings.Contains(output, "tracked.txt") || !strings.Contains(output, "1 insertion") {
		t.Fatalf("expected git diff stat to summarize tracked file change, got %q", output)
	}
}

func TestWriteFileToolWritesWorkspaceFile(t *testing.T) {
	rootDir := t.TempDir()
	workspace, err := permission.NewWorkspacePolicy(rootDir)
	if err != nil {
		t.Fatalf("create workspace policy: %v", err)
	}

	baseTool, err := newWriteFileTool(workspace, checkpoint.NewManager())
	if err != nil {
		t.Fatalf("create write_file tool: %v", err)
	}
	invokableTool := mustInvokableTool(t, baseTool)

	output, err := invokableTool.InvokableRun(context.Background(), `{"path":"hello.txt","content":"hello nanocode"}`)
	if err != nil {
		t.Fatalf("invoke write_file tool: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(rootDir, "hello.txt"))
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	if string(content) != "hello nanocode" {
		t.Fatalf("unexpected written content: %q", string(content))
	}
	if !strings.Contains(output, "checkpoint created") {
		t.Fatalf("expected checkpoint message, got %q", output)
	}
}

func TestWriteFileToolRejectsWorkspaceEscape(t *testing.T) {
	rootDir := t.TempDir()
	workspace, err := permission.NewWorkspacePolicy(rootDir)
	if err != nil {
		t.Fatalf("create workspace policy: %v", err)
	}

	baseTool, err := newWriteFileTool(workspace, checkpoint.NewManager())
	if err != nil {
		t.Fatalf("create write_file tool: %v", err)
	}
	invokableTool := mustInvokableTool(t, baseTool)

	_, err = invokableTool.InvokableRun(context.Background(), `{"path":"../outside.txt","content":"nope"}`)
	if err == nil {
		t.Fatal("expected write_file to reject workspace escape")
	}
}

func TestEditFileToolReplacesSingleMatch(t *testing.T) {
	rootDir := t.TempDir()
	filePath := filepath.Join(rootDir, "hello.txt")
	if err := os.WriteFile(filePath, []byte("hello old value\n"), 0o644); err != nil {
		t.Fatalf("write fixture file: %v", err)
	}

	workspace, err := permission.NewWorkspacePolicy(rootDir)
	if err != nil {
		t.Fatalf("create workspace policy: %v", err)
	}

	baseTool, err := newEditFileTool(workspace, checkpoint.NewManager())
	if err != nil {
		t.Fatalf("create edit_file tool: %v", err)
	}
	invokableTool := mustInvokableTool(t, baseTool)

	output, err := invokableTool.InvokableRun(context.Background(), `{"path":"hello.txt","old_string":"old value","new_string":"new value"}`)
	if err != nil {
		t.Fatalf("invoke edit_file tool: %v", err)
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read edited file: %v", err)
	}
	if string(content) != "hello new value\n" {
		t.Fatalf("unexpected edited content: %q", string(content))
	}
	if !strings.Contains(output, "1 replacement") {
		t.Fatalf("expected replacement count, got %q", output)
	}
}

func TestEditFileToolErrorsWhenOldStringMissing(t *testing.T) {
	rootDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootDir, "hello.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write fixture file: %v", err)
	}

	workspace, err := permission.NewWorkspacePolicy(rootDir)
	if err != nil {
		t.Fatalf("create workspace policy: %v", err)
	}

	baseTool, err := newEditFileTool(workspace, checkpoint.NewManager())
	if err != nil {
		t.Fatalf("create edit_file tool: %v", err)
	}
	invokableTool := mustInvokableTool(t, baseTool)

	_, err = invokableTool.InvokableRun(context.Background(), `{"path":"hello.txt","old_string":"missing","new_string":"new"}`)
	if err == nil {
		t.Fatal("expected edit_file to fail when old_string is missing")
	}
}

func TestEditFileToolErrorsOnMultipleMatchesByDefault(t *testing.T) {
	rootDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootDir, "hello.txt"), []byte("same same"), 0o644); err != nil {
		t.Fatalf("write fixture file: %v", err)
	}

	workspace, err := permission.NewWorkspacePolicy(rootDir)
	if err != nil {
		t.Fatalf("create workspace policy: %v", err)
	}

	baseTool, err := newEditFileTool(workspace, checkpoint.NewManager())
	if err != nil {
		t.Fatalf("create edit_file tool: %v", err)
	}
	invokableTool := mustInvokableTool(t, baseTool)

	_, err = invokableTool.InvokableRun(context.Background(), `{"path":"hello.txt","old_string":"same","new_string":"other"}`)
	if err == nil {
		t.Fatal("expected edit_file to fail on multiple matches by default")
	}
}

func TestBashToolTimesOut(t *testing.T) {
	rootDir := t.TempDir()
	workspace, err := permission.NewWorkspacePolicy(rootDir)
	if err != nil {
		t.Fatalf("create workspace policy: %v", err)
	}

	baseTool, err := newBashTool(workspace)
	if err != nil {
		t.Fatalf("create bash tool: %v", err)
	}
	invokableTool := mustInvokableTool(t, baseTool)

	command := `Start-Sleep -Seconds 2`
	if os.PathSeparator == '/' {
		command = `sleep 2`
	}
	_, err = invokableTool.InvokableRun(context.Background(), fmt.Sprintf(`{"command":%q,"timeout_seconds":1}`, command))
	if err == nil {
		t.Fatal("expected bash command to time out")
	}
}

func TestParseMode(t *testing.T) {
	mode, err := permission.ParseMode("plan")
	if err != nil {
		t.Fatalf("parse mode: %v", err)
	}
	if mode != permission.ModePlan {
		t.Fatalf("unexpected mode: %q", mode)
	}
}

func mustInvokableTool(t *testing.T, baseTool einotool.BaseTool) einotool.InvokableTool {
	t.Helper()

	invokableTool, ok := baseTool.(einotool.InvokableTool)
	if !ok {
		t.Fatal("tool should implement InvokableTool")
	}
	return invokableTool
}

func runGitTestCommand(t *testing.T, workdir string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = workdir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, string(output))
	}
}
