package permission

import (
	"fmt"
	"strings"
)

type Mode string

const (
	ModeDefault     Mode = "default"
	ModePlan        Mode = "plan"
	ModeAcceptEdits Mode = "accept-edits"
)

func ParseMode(raw string) (Mode, error) {
	// mode 是用户可见的执行策略入口，统一在这里解析，避免各层散落字符串判断。
	mode := Mode(strings.TrimSpace(raw))
	if mode == "" {
		return ModeDefault, nil
	}

	switch mode {
	case ModeDefault, ModePlan, ModeAcceptEdits:
		return mode, nil
	default:
		return "", fmt.Errorf("unsupported mode %q: expected default, plan, or accept-edits", raw)
	}
}
