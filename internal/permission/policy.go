package permission

import (
	"context"
	"fmt"
)

type ToolKind string

const (
	ToolKindRead  ToolKind = "read"
	ToolKindWrite ToolKind = "write"
	ToolKindShell ToolKind = "shell"
)

type Decision string

const (
	DecisionAllow Decision = "allow"
	DecisionDeny  Decision = "deny"
	DecisionAsk   Decision = "ask"
)

type Request struct {
	// Request 是 Agent 执行工具前交给权限策略判断的最小上下文。
	ToolName  string
	Arguments string
	Kind      ToolKind
	Risk      string
	Preview   string
}

type Confirmer interface {
	Confirm(ctx context.Context, request Request) (Decision, error)
}

type DenyConfirmer struct{}

func (DenyConfirmer) Confirm(ctx context.Context, request Request) (Decision, error) {
	_ = ctx
	_ = request
	return DecisionDeny, nil
}

func Evaluate(mode Mode, request Request) (Decision, error) {
	// 阶段 3 先做工具级权限；后续可以在这里继续加参数级风险判断。
	switch request.Kind {
	case ToolKindRead:
		return DecisionAllow, nil
	case ToolKindWrite:
		return evaluateWrite(mode), nil
	case ToolKindShell:
		return evaluateShell(mode), nil
	default:
		return DecisionDeny, fmt.Errorf("unknown tool kind %q for tool %q", request.Kind, request.ToolName)
	}
}

func evaluateWrite(mode Mode) Decision {
	switch mode {
	case ModePlan:
		return DecisionDeny
	case ModeAcceptEdits:
		return DecisionAllow
	default:
		return DecisionAsk
	}
}

func evaluateShell(mode Mode) Decision {
	switch mode {
	case ModePlan:
		return DecisionDeny
	default:
		return DecisionAsk
	}
}
