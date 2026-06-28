package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"nanocode/internal/agent"
	"nanocode/internal/checkpoint"
	"nanocode/internal/config"
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

	// 启动时先完成模型配置装配；runner、REPL、session 都共享这份配置。
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	mode, err := permission.ParseMode(modeRaw)
	if err != nil {
		log.Fatal(err)
	}

	checkpoints := checkpoint.NewManager()
	runnerFactory := func(ctx context.Context, mode permission.Mode, confirmer permission.Confirmer) (ui.PromptRunner, error) {
		// runner 由工厂统一创建，/mode 切换时会重建工具暴露策略、system prompt 和记忆。
		return agent.NewSingleTurnRunnerWithRuntime(ctx, cfg, mode, confirmer, checkpoints)
	}

	// -prompt 走一次性脚本调用，不写入 session，但同样会加载长期记忆。
	if prompt != "" {
		runner, err := runnerFactory(context.Background(), mode, permission.DenyConfirmer{})
		if err != nil {
			log.Fatal(err)
		}
		if err := runner.Run(context.Background(), prompt, os.Stdout); err != nil {
			log.Fatal(err)
		}

		fmt.Fprintln(os.Stdout)
		return
	}

	workspaceRoot, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}

	sessionStore := session.NewStore(workspaceRoot)
	currentSession, err := loadOrCreateSession(sessionStore, string(mode), cfg.Model, workspaceRoot, continueLatest, resumeID)
	if err != nil {
		log.Fatal(err)
	}
	if currentSession.Mode != "" {
		// 恢复会话时优先尊重会话里保存的 mode，让续跑体验和上次退出时一致。
		loadedMode, parseErr := permission.ParseMode(currentSession.Mode)
		if parseErr == nil {
			mode = loadedMode
		}
	}

	modelLabel := cfg.Model
	if cfg.BaseURL != "" {
		modelLabel = fmt.Sprintf("%s (%s)", cfg.Model, cfg.BaseURL)
	}

	repl := ui.NewREPL(os.Stdin, os.Stdout, mode, modelLabel, nil, runnerFactory)
	repl.SetSession(sessionStore, currentSession)
	runner, err := runnerFactory(context.Background(), mode, repl)
	if err != nil {
		log.Fatal(err)
	}
	repl.SetRunner(runner)
	if err := repl.Run(context.Background()); err != nil {
		log.Fatal(err)
	}
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
