package checkpoint

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type Manager struct {
	// Manager 当前只保存最近一次写操作快照，支撑 MVP 版 /undo。
	mu   sync.Mutex
	last *Snapshot
}

type Snapshot struct {
	Path    string
	Content []byte
	Exists  bool
}

func NewManager() *Manager {
	return &Manager{}
}

func (m *Manager) SaveBeforeWrite(path string) error {
	if m == nil {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			// 文件原本不存在时，undo 的语义就是删除这次新建的文件。
			m.last = &Snapshot{Path: path, Exists: false}
			return nil
		}
		return fmt.Errorf("stat checkpoint target: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("checkpoint target %q is a directory", path)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read checkpoint target: %w", err)
	}

	m.last = &Snapshot{Path: path, Content: content, Exists: true}
	return nil
}

func (m *Manager) UndoLast(ctx context.Context) (string, error) {
	_ = ctx
	if m == nil {
		return "没有可撤销的修改。", nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.last == nil {
		return "没有可撤销的修改。", nil
	}

	snapshot := m.last
	m.last = nil

	if snapshot.Exists {
		// 原文件存在：恢复旧内容。
		if err := os.WriteFile(snapshot.Path, snapshot.Content, 0o644); err != nil {
			return "", fmt.Errorf("restore checkpoint: %w", err)
		}
		return fmt.Sprintf("已恢复文件：%s", filepath.Base(snapshot.Path)), nil
	}

	// 原文件不存在：撤销方式是删除本轮创建出来的文件。
	if err := os.Remove(snapshot.Path); err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("remove created file: %w", err)
	}
	return fmt.Sprintf("已撤销新建文件：%s", filepath.Base(snapshot.Path)), nil
}
