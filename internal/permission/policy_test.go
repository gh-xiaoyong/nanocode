package permission

import "testing"

func TestEvaluateModePolicy(t *testing.T) {
	tests := []struct {
		name     string
		mode     Mode
		kind     ToolKind
		expected Decision
	}{
		{name: "plan denies write", mode: ModePlan, kind: ToolKindWrite, expected: DecisionDeny},
		{name: "plan denies shell", mode: ModePlan, kind: ToolKindShell, expected: DecisionDeny},
		{name: "default asks write", mode: ModeDefault, kind: ToolKindWrite, expected: DecisionAsk},
		{name: "default asks shell", mode: ModeDefault, kind: ToolKindShell, expected: DecisionAsk},
		{name: "accept edits allows write", mode: ModeAcceptEdits, kind: ToolKindWrite, expected: DecisionAllow},
		{name: "accept edits asks shell", mode: ModeAcceptEdits, kind: ToolKindShell, expected: DecisionAsk},
		{name: "read is always allowed", mode: ModePlan, kind: ToolKindRead, expected: DecisionAllow},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision, err := Evaluate(tt.mode, Request{ToolName: "tool", Kind: tt.kind})
			if err != nil {
				t.Fatalf("Evaluate() error = %v", err)
			}
			if decision != tt.expected {
				t.Fatalf("expected %q, got %q", tt.expected, decision)
			}
		})
	}
}
