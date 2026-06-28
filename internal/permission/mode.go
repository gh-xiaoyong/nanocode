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
