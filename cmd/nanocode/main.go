package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"nanocode/internal/agent"
	"nanocode/internal/checkpoint"
	"nanocode/internal/config"
	"nanocode/internal/friendlyerror"
	"nanocode/internal/permission"
	"nanocode/internal/session"
	"nanocode/internal/ui"
)

func main() {
	var prompt string
	var modeRaw string
	var continueLatest bool
	var resumeID string

	flag.StringVar(&prompt, "prompt", "", "single user prompt to send")
	flag.StringVar(&modeRaw, "mode", string(permission.ModeDefault), "execution mode: default, plan, or accept-edits")
	flag.BoolVar(&continueLatest, "continue", false, "continue the latest saved session")
	flag.StringVar(&resumeID, "resume", "", "resume a saved session by id")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		fatal(err)
	}

	mode, err := permission.ParseMode(modeRaw)
	if err != nil {
		fatal(err)
	}

	checkpoints := checkpoint.NewManager()
	currentConfig := cfg
	modelLabel := formatModelLabel(cfg)
	runnerFactory := func(ctx context.Context, mode permission.Mode, confirmer permission.Confirmer) (ui.PromptRunner, error) {
		// runner 工厂每次重新读取配置；/reload 和 /mode 重建 runner 时能拿到最新 settings.json 与 memory。
		loadedConfig, err := config.Load()
		if err != nil {
			return nil, err
		}
		currentConfig = loadedConfig
		modelLabel = formatModelLabel(loadedConfig)
		return agent.NewSingleTurnRunnerWithRuntime(ctx, loadedConfig, mode, confirmer, checkpoints)
	}

	// -prompt 走一次性脚本调用，不写入 session，但同样会加载长期记忆。
	if prompt != "" {
		runner, err := runnerFactory(context.Background(), mode, permission.DenyConfirmer{})
		if err != nil {
			fatal(err)
		}
		if err := runner.Run(context.Background(), prompt, os.Stdout); err != nil {
			fatal(err)
		}

		fmt.Fprintln(os.Stdout)
		return
	}

	workspaceRoot, err := os.Getwd()
	if err != nil {
		fatal(err)
	}

	sessionStore := session.NewStore(workspaceRoot)
	currentSession, err := loadOrCreateSession(sessionStore, string(mode), cfg.Model, workspaceRoot, continueLatest, resumeID)
	if err != nil {
		fatal(err)
	}
	if currentSession.Mode != "" {
		// 恢复会话时优先尊重会话里保存的 mode，让继续体验和上次退出时一致。
		loadedMode, parseErr := permission.ParseMode(currentSession.Mode)
		if parseErr == nil {
			mode = loadedMode
		}
	}

	lineReader, closeReader := ui.NewReadlineLineReader()
	defer func() {
		if err := closeReader(); err != nil {
			fmt.Fprintln(os.Stderr, friendlyerror.Format(err))
		}
	}()

	repl := ui.NewREPLWithLineReader(lineReader, os.Stdout, mode, modelLabel, nil, runnerFactory)
	repl.SetSession(sessionStore, currentSession)
	runner, err := runnerFactory(context.Background(), mode, repl)
	if err != nil {
		fatal(err)
	}
	repl.SetRunner(runner)
	repl.SetModelLabel(modelLabel)
	currentSession.Model = currentConfig.Model
	if err := sessionStore.Save(currentSession); err != nil {
		fatal(err)
	}
	if err := repl.Run(context.Background()); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	if err == nil {
		return
	}
	fmt.Fprintln(os.Stderr, formatFatalError(err))
	os.Exit(1)
}

func formatFatalError(err error) string {
	return friendlyerror.Format(err)
}

func formatModelLabel(cfg config.ModelConfig) string {
	if cfg.BaseURL != "" {
		return fmt.Sprintf("%s (%s)", cfg.Model, cfg.BaseURL)
	}
	return cfg.Model
}

func loadOrCreateSession(store *session.Store, mode string, model string, workspaceRoot string, continueLatest bool, resumeID string) (*session.Session, error) {
	// resume 精确恢复指定会话，continue 恢复最近会话；都没有时创建新会话。
	if resumeID != "" {
		return store.Load(resumeID)
	}
	if continueLatest {
		return store.LoadLatest()
	}
	return store.Create(mode, model, workspaceRoot)
}
