package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const (
	ProjectFileName    = "NANO.md"
	UserFileName       = "NANO.md"
	DefaultMaxRunes    = 12000
	userMemoryDirName  = ".nanocode"
	truncationTemplate = "\n\n[记忆内容已截断：原始长度 %d 字符，保留前 %d 字符。]"
)

type SourceKind string

const (
	SourceUser    SourceKind = "user"
	SourceProject SourceKind = "project"
)

type Source struct {
	Kind      SourceKind
	Path      string
	Content   string
	Exists    bool
	Truncated bool
}

type Memory struct {
	Sources []Source
}

type Loader struct {
	workspaceRoot string
	homeDir       string
	maxRunes      int
}

func NewLoader(workspaceRoot string) (*Loader, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("get user home directory: %w", err)
	}
	return NewLoaderWithHome(workspaceRoot, homeDir, DefaultMaxRunes), nil
}

func NewLoaderWithHome(workspaceRoot string, homeDir string, maxRunes int) *Loader {
	if maxRunes <= 0 {
		maxRunes = DefaultMaxRunes
	}
	return &Loader{
		workspaceRoot: workspaceRoot,
		homeDir:       homeDir,
		maxRunes:      maxRunes,
	}
}

func (l *Loader) Load() (Memory, error) {
	// 加载顺序固定为用户级、项目级；注入 prompt 时也保持这个顺序，让项目规则更靠近当前任务。
	sources := make([]Source, 0, 2)
	for _, source := range []Source{
		{Kind: SourceUser, Path: filepath.Join(l.homeDir, userMemoryDirName, UserFileName)},
		{Kind: SourceProject, Path: filepath.Join(l.workspaceRoot, ProjectFileName)},
	} {
		loaded, err := l.loadSource(source)
		if err != nil {
			return Memory{}, err
		}
		sources = append(sources, loaded)
	}
	return Memory{Sources: sources}, nil
}

func (l *Loader) loadSource(source Source) (Source, error) {
	content, err := os.ReadFile(source.Path)
	if err != nil {
		if os.IsNotExist(err) {
			source.Exists = false
			return source, nil
		}
		return source, fmt.Errorf("read %s memory %s: %w", source.Kind, source.Path, err)
	}

	source.Exists = true
	source.Content, source.Truncated = truncateContent(strings.TrimSpace(string(content)), l.maxRunes)
	return source, nil
}

func (m Memory) LoadedSources() []Source {
	loaded := make([]Source, 0, len(m.Sources))
	for _, source := range m.Sources {
		if source.Exists && strings.TrimSpace(source.Content) != "" {
			loaded = append(loaded, source)
		}
	}
	return loaded
}

func (m Memory) IsEmpty() bool {
	return len(m.LoadedSources()) == 0
}

func truncateContent(content string, maxRunes int) (string, bool) {
	runeCount := utf8.RuneCountInString(content)
	if runeCount <= maxRunes {
		return content, false
	}

	var builder strings.Builder
	count := 0
	for _, r := range content {
		if count >= maxRunes {
			break
		}
		builder.WriteRune(r)
		count++
	}
	builder.WriteString(fmt.Sprintf(truncationTemplate, runeCount, maxRunes))
	return builder.String(), true
}
