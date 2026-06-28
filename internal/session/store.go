package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"
)

const sessionDirName = ".nanocode"

type Session struct {
	// Session 是落盘的会话快照；messages 直接复用 Eino schema，方便恢复后继续喂给模型。
	ID            string            `json:"id"`
	CreatedAt     time.Time         `json:"created_at"`
	UpdatedAt     time.Time         `json:"updated_at"`
	Mode          string            `json:"mode"`
	Model         string            `json:"model"`
	WorkspaceRoot string            `json:"workspace_root"`
	Messages      []*schema.Message `json:"messages"`
}

type Store struct {
	rootDir string
}

func NewStore(workspaceRoot string) *Store {
	// 会话保存在项目本地 .nanocode/sessions，避免和用户全局状态混在一起。
	return &Store{rootDir: filepath.Join(workspaceRoot, sessionDirName, "sessions")}
}

func (s *Store) RootDir() string {
	return s.rootDir
}

func (s *Store) Path(id string) string {
	return filepath.Join(s.rootDir, id+".json")
}

func (s *Store) Create(mode string, model string, workspaceRoot string) (*Session, error) {
	// 新会话先立即落盘，这样 REPL 欢迎页展示的 session id 已经可恢复。
	now := time.Now().UTC()
	session := &Session{
		ID:            newID(now),
		CreatedAt:     now,
		UpdatedAt:     now,
		Mode:          mode,
		Model:         model,
		WorkspaceRoot: workspaceRoot,
		Messages:      []*schema.Message{},
	}
	if err := s.Save(session); err != nil {
		return nil, err
	}
	return session, nil
}

func (s *Store) Save(session *Session) error {
	if session == nil {
		return fmt.Errorf("session is nil")
	}
	if strings.TrimSpace(session.ID) == "" {
		return fmt.Errorf("session id is empty")
	}
	if err := os.MkdirAll(s.rootDir, 0o755); err != nil {
		return fmt.Errorf("create session directory: %w", err)
	}

	// 每次保存都刷新 UpdatedAt，--continue 就能按最近活跃会话恢复。
	session.UpdatedAt = time.Now().UTC()
	content, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}

	if err := os.WriteFile(s.Path(session.ID), content, 0o600); err != nil {
		return fmt.Errorf("write session: %w", err)
	}
	return nil
}

func (s *Store) Load(id string) (*Session, error) {
	cleanID := strings.TrimSpace(id)
	if cleanID == "" {
		return nil, fmt.Errorf("session id is empty")
	}

	content, err := os.ReadFile(s.Path(cleanID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("session %q not found", cleanID)
		}
		return nil, fmt.Errorf("read session: %w", err)
	}

	var session Session
	if err := json.Unmarshal(content, &session); err != nil {
		return nil, fmt.Errorf("parse session %q: %w", cleanID, err)
	}
	return &session, nil
}

func (s *Store) LoadLatest() (*Session, error) {
	// --continue 使用更新时间排序，而不是文件名排序，避免手动复制文件影响恢复选择。
	entries, err := os.ReadDir(s.rootDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no sessions found")
		}
		return nil, fmt.Errorf("read session directory: %w", err)
	}

	sessions := make([]*Session, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		loaded, loadErr := s.Load(id)
		if loadErr != nil {
			continue
		}
		sessions = append(sessions, loaded)
	}

	if len(sessions) == 0 {
		return nil, fmt.Errorf("no sessions found")
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})
	return sessions[0], nil
}

func newID(now time.Time) string {
	return "sess-" + now.Format("20060102-150405.000000000")
}
