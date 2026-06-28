package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"nanocode/internal/agent"
	"nanocode/internal/config"
	"nanocode/internal/permission"
	"nanocode/internal/ui"
)

func main() {
	var prompt string
	var modeRaw string

	flag.StringVar(&prompt, "prompt", "", "single user prompt to send")
	flag.StringVar(&modeRaw, "mode", string(permission.ModeDefault), "execution mode: default, plan, or accept-edits")
	flag.Parse()

	// 启动时先完成模型配置装配，避免后续流程在运行中途才暴露缺参问题。
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	mode, err := permission.ParseMode(modeRaw)
	if err != nil {
		log.Fatal(err)
	}

	runnerFactory := func(ctx context.Context, mode permission.Mode) (ui.PromptRunner, error) {
		// 统一用一个工厂创建 runner，既服务单轮入口，也服务 REPL 中的模式切换。
		return agent.NewSingleTurnRunner(ctx, cfg, mode)
	}

	// Runner 封装了当前阶段最小可运行能力：组装 Agent 并执行一轮流式回复。
	runner, err := runnerFactory(context.Background(), mode)
	if err != nil {
		log.Fatal(err)
	}

	// 命令行显式传 prompt 时，保留单轮调用体验，便于脚本和快速验证。
	if prompt != "" {
		if err := runner.Run(context.Background(), prompt, os.Stdout); err != nil {
			log.Fatal(err)
		}

		fmt.Fprintln(os.Stdout)
		return
	}

	// 未传 prompt 时进入 REPL，便于连续验证当前阶段的 Agent 行为。
	modelLabel := cfg.Model
	if cfg.BaseURL != "" {
		modelLabel = fmt.Sprintf("%s (%s)", cfg.Model, cfg.BaseURL)
	}

	repl := ui.NewREPL(os.Stdin, os.Stdout, mode, modelLabel, runner, runnerFactory)
	if err := repl.Run(context.Background()); err != nil {
		log.Fatal(err)
	}
}
