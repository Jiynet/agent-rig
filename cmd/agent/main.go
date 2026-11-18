// Package main 是 AgentRig 的可执行入口：
// 加载配置、组装各模块，并启动交互循环。
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"ai-cli-agent/internal/agent"
	ctxasm "ai-cli-agent/internal/context"
	"ai-cli-agent/internal/eventbus"
	"ai-cli-agent/internal/execution"
	"ai-cli-agent/internal/llm"
	"ai-cli-agent/internal/memory"
	"ai-cli-agent/internal/perception"
	"ai-cli-agent/internal/planning"
	"ai-cli-agent/internal/tools"
	"ai-cli-agent/internal/tools/builtin"
	"ai-cli-agent/internal/tracing"
)

func main() {
	configPath := flag.String("config", "configs/config.yaml", "配置文件路径")
	flag.Parse()

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// ---- 组装各模块 ----
	bus := eventbus.NewSimpleBus()
	perc := perception.NewTerminalPerception()
	exec := execution.NewConfirmExecution(perc)

	apiKey := os.Getenv(cfg.LLM.APIKeyEnv)
	client := llm.NewOpenAILLM(apiKey, cfg.LLM.BaseURL)
	planner := planning.NewReActPlanner(client, exec, cfg.LLM.Model, cfg.Agent.MaxIterations)

	// 上下文组装：缺省使用默认预算；max_prompt_tokens < 0 时关闭。
	ctxCfg := ctxasm.DefaultConfig()
	if cfg.Context.MaxPromptTokens < 0 {
		ctxCfg.MaxPromptTokens = 0
	} else if cfg.Context.MaxPromptTokens > 0 {
		ctxCfg.MaxPromptTokens = cfg.Context.MaxPromptTokens
	}
	if cfg.Context.RecentMessages > 0 {
		ctxCfg.RecentMessages = cfg.Context.RecentMessages
	}
	if cfg.Context.MaxToolOutputTokens > 0 {
		ctxCfg.MaxToolOutputTokens = cfg.Context.MaxToolOutputTokens
	}
	if cfg.Context.SummaryMaxTokens > 0 {
		ctxCfg.SummaryMaxTokens = cfg.Context.SummaryMaxTokens
	}
	planner.SetAssembler(ctxasm.NewAssembler(ctxCfg))

	// 创建追踪器并注入规划器
	tracer := tracing.NewConsoleTracer(os.Stderr)
	planner.SetTracer(tracer)

	registry := tools.NewRegistry()
	enabled := make(map[string]bool, len(cfg.Tools.Enabled))
	for _, name := range cfg.Tools.Enabled {
		enabled[name] = true
	}
	for _, spec := range builtin.All() {
		if enabled[spec.Definition.Name] {
			registry.Register(spec)
		}
	}

	// 将可用工具定义注入规划器，使 LLM 在 ReAct 循环中能够感知并选择调用。
	toolDefs, err := registry.List(context.Background())
	if err != nil {
		log.Fatalf("列出工具失败: %v", err)
	}
	planner.SetTools(toolDefs)

	var mem memory.Memory
	if cfg.Memory.Enabled {
		mem = memory.NewVectorMemory()
	} else {
		mem = memory.NewNoop()
	}

	agt := agent.New(agent.Config{
		Perception: perc,
		Planner:    planner,
		Execution:  exec,
		LLM:        client,
		Tools:      registry,
		Memory:     mem,
		Bus:        bus,
		Tracer:     tracer,
	})

	// ---- 交互循环 ----
	ctx := context.Background()
	fmt.Println("AgentRig 交互模式（输入 exit 退出）")
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("> ")
		line, err := reader.ReadString('\n')
		if err != nil {
			break // EOF 或读入错误
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if line == "exit" || line == "quit" {
			break
		}
		if err := runOnce(ctx, agt, perc, line); err != nil {
			fmt.Fprintf(os.Stderr, "错误: %v\n", err)
		}
	}
}

// runOnce 执行一次 Agent 调用并格式化输出结果。
func runOnce(ctx context.Context, agt *agent.Agent, perc perception.Perception, input string) error {
	result, err := agt.Run(ctx, input)
	if err != nil {
		return err
	}
	out, err := perc.FormatOutput(ctx, result)
	if err != nil {
		return err
	}
	fmt.Print(out)
	return nil
}
