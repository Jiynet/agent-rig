// demo 演示 AgentRig 的端到端能力：
// 用户提问 → LLM 推理 → 调用工具（读取文件）→ 综合回答。
//
// 使用 scripted LLM 模拟多轮 ReAct 循环，无需真实 API Key。
//
// 运行: go run ./cmd/demo
package main

import (
	"context"
	"fmt"
	"os"

	"ai-cli-agent/internal/agent"
	"ai-cli-agent/internal/eventbus"
	"ai-cli-agent/internal/execution"
	"ai-cli-agent/internal/memory"
	"ai-cli-agent/internal/perception"
	"ai-cli-agent/internal/planning"
	"ai-cli-agent/internal/tools"
	"ai-cli-agent/internal/tools/builtin"
	"ai-cli-agent/internal/tracing"
	"ai-cli-agent/pkg/types"
)

// scriptedLLM 按调用顺序返回预设响应，模拟 LLM 的多轮推理。
type scriptedLLM struct {
	responses []*types.GenerateResponse
	idx       int
}

func (s *scriptedLLM) Generate(_ context.Context, _ *types.GenerateRequest) (*types.GenerateResponse, error) {
	if s.idx >= len(s.responses) {
		return &types.GenerateResponse{Content: "任务已完成。"}, nil
	}
	r := s.responses[s.idx]
	s.idx++
	return r, nil
}

func (s *scriptedLLM) GenerateStream(_ context.Context, _ *types.GenerateRequest, _ func(chunk types.StreamChunk) error) error {
	return nil
}

func main() {
	fmt.Println("╔══════════════════════════════════════════════════════════╗")
	fmt.Println("║          AgentRig 端到端演示                            ║")
	fmt.Println("║  场景：Agent 读取项目文件并生成结构分析                     ║")
	fmt.Println("╚══════════════════════════════════════════════════════════╝")
	fmt.Println()

	// 组装模块
	perc := perception.NewTerminalPerception()
	exec := execution.NewConfirmExecution(perc)
	bus := eventbus.NewSimpleBus()

	// Scripted LLM：模拟 Agent 的多轮推理过程
	// 第1轮：LLM 决定读取 go.mod 了解项目
	// 第2轮：LLM 决定读取 README.md 了解项目描述
	// 第3轮：LLM 给出最终分析
	llm := &scriptedLLM{
		responses: []*types.GenerateResponse{
			{
				Content: "我需要先了解这个项目的基本信息，让我读取 go.mod 文件。",
				ToolCalls: []*types.ToolCall{
					{ID: "call_1", Name: "file_read", Input: map[string]any{"path": "go.mod"}},
				},
			},
			{
				Content: "这是一个 Go 项目。让我再看看 README 了解更多细节。",
				ToolCalls: []*types.ToolCall{
					{ID: "call_2", Name: "file_read", Input: map[string]any{"path": "README.md"}},
				},
			},
			{
				Content: `## 项目结构分析

**项目名称**: AgentRig
**语言**: Go (1.26)
**架构**: 四层分离（感知 → 规划 → 执行 → 工具）

### 核心特性
- ReAct 推理循环：LLM 推理 → 工具调用 → 观察 → 再推理
- MCP 协议支持：通过 stdio 暴露工具给外部客户端
- 并发工具执行：多个工具调用可并行执行
- 向量记忆系统：基于 n-gram 的语义检索
- 全链路追踪：每个执行步骤都有结构化 Span

### 工具集
- shell: 执行终端命令
- file_read/file_write: 文件读写
- web_search: 网络搜索（DuckDuckGo API）
- http_request: HTTP 请求`,
			},
		},
	}

	planner := planning.NewReActPlanner(llm, exec, "demo-model", 10)

	// 注册工具
	registry := tools.NewRegistry()
	for _, spec := range builtin.All() {
		registry.Register(spec)
	}
	defs, _ := registry.List(context.Background())
	planner.SetTools(defs)

	// 创建追踪器（输出到 stderr）
	tracer := tracing.NewConsoleTracer(os.Stderr)
	planner.SetTracer(tracer)

	// 组装 Agent
	agt := agent.New(agent.Config{
		Perception: perc,
		Planner:    planner,
		Execution:  exec,
		LLM:        llm,
		Tools:      registry,
		Memory:     memory.NewNoop(),
		Bus:        bus,
		Tracer:     tracer,
	})

	// 执行演示
	input := "分析一下当前项目的结构和技术栈"
	fmt.Printf("> %s\n\n", input)

	result, err := agt.Run(context.Background(), input)
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\n================ Agent Result ================")
	fmt.Println(result.Summary)
	fmt.Println("==============================================")
}
