// Package agent 是编排层：组装感知、规划、执行、LLM、工具与记忆模块，
// 并驱动主控制循环。
package agent

import (
	"ai-cli-agent/internal/eventbus"
	"ai-cli-agent/internal/execution"
	"ai-cli-agent/internal/llm"
	"ai-cli-agent/internal/memory"
	"ai-cli-agent/internal/perception"
	"ai-cli-agent/internal/planning"
	"ai-cli-agent/internal/tools"
	"ai-cli-agent/internal/tracing"
)

// Config 是 Agent 的运行时配置，各模块由组装方注入。
type Config struct {
	Perception perception.Perception
	Planner    planning.Planner
	Execution  execution.Execution
	LLM        llm.LLM
	Tools      *tools.Registry
	Memory     memory.Memory
	Bus        eventbus.Bus
	Tracer     tracing.Tracer // 可选：为 nil 时使用 noop
}

// Agent 组合各模块，是系统的门面对象。
type Agent struct {
	perception perception.Perception
	planner    planning.Planner
	execution  execution.Execution
	tools      *tools.Registry
	memory     memory.Memory
	bus        eventbus.Bus
	llm        llm.LLM
	tracer     tracing.Tracer
}

// New 构造 Agent，bus 为空时自动创建内存态事件总线。
func New(cfg Config) *Agent {
	bus := cfg.Bus
	if bus == nil {
		bus = eventbus.NewSimpleBus()
	}
	tracer := cfg.Tracer
	if tracer == nil {
		tracer = tracing.NewNoopTracer()
	}
	return &Agent{
		perception: cfg.Perception,
		planner:    cfg.Planner,
		execution:  cfg.Execution,
		llm:        cfg.LLM,
		tools:      cfg.Tools,
		memory:     cfg.Memory,
		bus:        bus,
		tracer:     tracer,
	}
}
