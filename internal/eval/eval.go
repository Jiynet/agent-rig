// Package eval 提供可复现的 Agent 运行时评测：
// 用 scripted LLM 驱动确定性任务，检查任务是否完成、工具调用是否符合预期、
// 工具调用配对是否完整，并对比“全量历史”与“预算化上下文”的 token 消耗。
package eval

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"ai-cli-agent/internal/agent"
	ctxasm "ai-cli-agent/internal/context"
	"ai-cli-agent/internal/eventbus"
	"ai-cli-agent/internal/execution"
	"ai-cli-agent/internal/memory"
	"ai-cli-agent/internal/planning"
	"ai-cli-agent/internal/tools"
	"ai-cli-agent/internal/tracing"
	"ai-cli-agent/pkg/types"
)

// Mode 选择上下文策略。
type Mode string

const (
	ModeFull      Mode = "full"      // 全量历史透传
	ModeAssembled Mode = "assembled" // 预算化上下文组装
)

// Task 是一条确定性评测任务。
type Task struct {
	Name                  string
	Input                 string
	Responses             []*types.GenerateResponse
	ExpectContains        string   // 最终摘要必须包含的文本
	ExpectTools           []string // 必须出现的工具（支持重复，按次数匹配）
	ExpectRequestContains string   // 任意一次 LLM 请求中必须出现的文本
	MaxIterations         int      // 0 表示使用默认 10
	SeedMemories          []string // 预置的记忆内容
	RejectMedium          bool     // 中高风险动作一律拒绝（测试 HITL 回退）
}

// Result 是单次评测结果。
type Result struct {
	Task                  string
	Mode                  Mode
	Success               bool
	Reason                string
	Steps                 int
	LLMCalls              int
	ToolCalls             int
	PromptTokens          int
	TotalTokens           int
	ContextTokens         int
	ContextOriginalTokens int
	SummarizedMessages    int
	TruncatedTools        int
	OverBudget            bool
	Duration              time.Duration
}

// Config 是评测运行配置。
type Config struct {
	Context ctxasm.Config
}

// DefaultConfig 返回用于对比的评测配置。
// 预算刻意调小，以便在少量轮次内触发截断与摘要。
func DefaultConfig() Config {
	c := ctxasm.DefaultConfig()
	c.MaxPromptTokens = 500
	c.RecentMessages = 4
	c.MaxToolOutputTokens = 80
	c.SummaryMaxTokens = 100
	return Config{Context: c}
}

// Run 在指定模式下运行一组任务。
func Run(tasks []Task, mode Mode, cfg Config) []Result {
	results := make([]Result, 0, len(tasks))
	for _, task := range tasks {
		results = append(results, runOne(task, mode, cfg))
	}
	return results
}

// RunAll 依次运行多个模式。
func RunAll(tasks []Task, modes []Mode, cfg Config) []Result {
	var out []Result
	for _, mode := range modes {
		out = append(out, Run(tasks, mode, cfg)...)
	}
	return out
}

func runOne(task Task, mode Mode, cfg Config) Result {
	res := Result{Task: task.Name, Mode: mode}

	perce := &evalPerception{rejectMedium: task.RejectMedium}
	exec := execution.NewConfirmExecution(perce)

	registry := newRegistry()
	defs, err := registry.List(context.Background())
	if err != nil {
		res.Reason = err.Error()
		return res
	}

	client := &scriptedLLM{responses: task.Responses}
	maxIter := task.MaxIterations
	if maxIter <= 0 {
		maxIter = 10
	}
	planner := planning.NewReActPlanner(client, exec, "eval-model", maxIter)
	planner.SetTools(defs)

	asmCfg := cfg.Context
	if mode == ModeFull {
		asmCfg = ctxasm.Config{MaxPromptTokens: 0}
	}
	planner.SetAssembler(ctxasm.NewAssembler(asmCfg))

	tracer := tracing.NewConsoleTracer(io.Discard)
	planner.SetTracer(tracer)

	var mem memory.Memory = memory.NewNoop()
	if len(task.SeedMemories) > 0 {
		vm := memory.NewVectorMemory()
		for i, content := range task.SeedMemories {
			_ = vm.Store(context.Background(), &types.MemoryEntry{
				Key:     fmt.Sprintf("seed-%d", i),
				Kind:    "long_term",
				Content: content,
			})
		}
		mem = vm
	}

	agt := agent.New(agent.Config{
		Perception: perce,
		Planner:    planner,
		Execution:  exec,
		LLM:        client,
		Tools:      registry,
		Memory:     mem,
		Bus:        eventbus.NewSimpleBus(),
		Tracer:     tracer,
	})

	start := time.Now()
	agentResult, err := agt.Run(context.Background(), task.Input)
	res.Duration = time.Since(start)
	if err != nil {
		res.Reason = "run: " + err.Error()
		return res
	}

	metrics := agt.Metrics()
	res.Steps = metrics.LoopIterations
	res.LLMCalls = metrics.LLMCalls
	res.ToolCalls = metrics.ToolCalls
	res.PromptTokens = metrics.PromptTokens
	res.TotalTokens = metrics.TotalTokens
	res.ContextTokens = metrics.ContextTokens
	res.ContextOriginalTokens = metrics.ContextOriginalTokens
	res.SummarizedMessages = metrics.ContextSummarized
	res.TruncatedTools = metrics.ContextTruncatedTools
	res.OverBudget = planner.ContextStats().OverBudget

	var reasons []string
	if task.ExpectContains != "" {
		if agentResult == nil || !strings.Contains(agentResult.Summary, task.ExpectContains) {
			reasons = append(reasons, fmt.Sprintf("summary missing %q", task.ExpectContains))
		}
	}

	if len(task.ExpectTools) > 0 && agentResult != nil {
		actual := make(map[string]int)
		for _, action := range agentResult.Actions {
			if action != nil && action.Type == types.ActionToolCall {
				actual[action.ToolName]++
			}
		}
		for _, name := range task.ExpectTools {
			if actual[name] <= 0 {
				reasons = append(reasons, fmt.Sprintf("missing tool %q", name))
				break
			}
			actual[name]--
		}
	}

	if task.ExpectRequestContains != "" {
		found := false
		for _, req := range client.Requests() {
			for _, m := range req.Messages {
				if m != nil && strings.Contains(m.Content, task.ExpectRequestContains) {
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			reasons = append(reasons, fmt.Sprintf("request missing %q", task.ExpectRequestContains))
		}
	}

	for i, req := range client.Requests() {
		if err := checkPairing(req.Messages); err != nil {
			reasons = append(reasons, fmt.Sprintf("request %d: %v", i, err))
			break
		}
	}

	if len(reasons) == 0 {
		res.Success = true
	} else {
		res.Reason = strings.Join(reasons, "; ")
	}
	return res
}

// DefaultTasks 返回内置的确定性评测任务。
func DefaultTasks() []Task {
	big := func(id string, n int) *types.GenerateResponse {
		return &types.GenerateResponse{
			Content: "读取数据中",
			ToolCalls: []*types.ToolCall{{
				ID:    id,
				Name:  "big_output",
				Input: map[string]any{"n": n},
			}},
		}
	}

	return []Task{
		{
			Name:           "direct_answer",
			Input:          "计算 6*7",
			Responses:      []*types.GenerateResponse{{Content: "42"}},
			ExpectContains: "42",
		},
		{
			Name:  "single_tool",
			Input: "回显 hello",
			Responses: []*types.GenerateResponse{
				{Content: "调用 echo", ToolCalls: []*types.ToolCall{{
					ID: "c1", Name: "echo", Input: map[string]any{"text": "hello"},
				}}},
				{Content: "结果是 hello"},
			},
			ExpectContains: "hello",
			ExpectTools:    []string{"echo"},
		},
		{
			Name:  "multi_tool_batch",
			Input: "并行回显 a 和 b",
			Responses: []*types.GenerateResponse{
				{Content: "批量调用", ToolCalls: []*types.ToolCall{
					{ID: "c1", Name: "echo", Input: map[string]any{"text": "a"}},
					{ID: "c2", Name: "echo", Input: map[string]any{"text": "b"}},
				}},
				{Content: "两个都完成了"},
			},
			ExpectContains: "都完成了",
			ExpectTools:    []string{"echo", "echo"},
		},
		{
			Name:  "long_tool_output",
			Input: "分析长日志",
			Responses: []*types.GenerateResponse{
				big("c1", 200),
				{Content: "分析完成"},
			},
			ExpectContains: "分析完成",
			ExpectTools:    []string{"big_output"},
		},
		{
			Name:  "multi_round_context",
			Input: "连续分析多轮日志",
			Responses: []*types.GenerateResponse{
				big("c1", 120),
				big("c2", 120),
				big("c3", 120),
				big("c4", 120),
				big("c5", 120),
				{Content: "最终汇总完成"},
			},
			ExpectContains: "最终汇总完成",
			ExpectTools:    []string{"big_output", "big_output", "big_output", "big_output", "big_output"},
		},
		{
			Name:          "max_iterations",
			Input:         "一直循环",
			MaxIterations: 3,
			Responses: []*types.GenerateResponse{
				{Content: "r1", ToolCalls: []*types.ToolCall{{ID: "c1", Name: "echo", Input: map[string]any{"text": "1"}}}},
				{Content: "r2", ToolCalls: []*types.ToolCall{{ID: "c2", Name: "echo", Input: map[string]any{"text": "2"}}}},
				{Content: "r3", ToolCalls: []*types.ToolCall{{ID: "c3", Name: "echo", Input: map[string]any{"text": "3"}}}},
				{Content: "r4", ToolCalls: []*types.ToolCall{{ID: "c4", Name: "echo", Input: map[string]any{"text": "4"}}}},
			},
			ExpectContains: "3",
			ExpectTools:    []string{"echo", "echo", "echo"},
		},
		{
			Name:         "rejection",
			Input:        "执行高风险命令",
			RejectMedium: true,
			Responses: []*types.GenerateResponse{
				{Content: "调用 shell", ToolCalls: []*types.ToolCall{{
					ID: "c1", Name: "shell", Input: map[string]any{"command": "rm -rf /"},
				}}},
				{Content: "用户拒绝了操作，已调整方案"},
			},
			ExpectContains: "用户拒绝了操作",
			ExpectTools:    []string{"shell"},
		},
		{
			Name:                  "memory_injection",
			Input:                 "这个项目用什么语言开发？",
			SeedMemories:          []string{"项目使用 Go 语言开发"},
			Responses:             []*types.GenerateResponse{{Content: "Go"}},
			ExpectContains:        "Go",
			ExpectRequestContains: "项目使用 Go 语言开发",
		},
	}
}

// ---- 测试用实现 ----

type evalPerception struct {
	rejectMedium bool
}

func (p *evalPerception) ParseInput(_ context.Context, raw string) (*types.Intent, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("empty input")
	}
	return &types.Intent{RawInput: raw, Goal: raw}, nil
}

func (p *evalPerception) PromptForJudgment(_ context.Context, _ string, action *types.Action) (*types.JudgmentResult, error) {
	if p.rejectMedium && action.Risk >= types.RiskMedium {
		return &types.JudgmentResult{Decision: types.DecisionReject, Feedback: "eval reject"}, nil
	}
	return &types.JudgmentResult{Decision: types.DecisionApprove}, nil
}

func (p *evalPerception) FormatOutput(_ context.Context, result *types.AgentResult) (string, error) {
	return result.Summary, nil
}

func (p *evalPerception) RenderChunk(_ context.Context, _ types.StreamChunk) error { return nil }

type scriptedLLM struct {
	mu        sync.Mutex
	responses []*types.GenerateResponse
	idx       int
	requests  []*types.GenerateRequest
}

func (s *scriptedLLM) Generate(_ context.Context, req *types.GenerateRequest) (*types.GenerateResponse, error) {
	s.mu.Lock()
	s.requests = append(s.requests, req)
	var resp types.GenerateResponse
	if s.idx < len(s.responses) {
		resp = *s.responses[s.idx]
		s.idx++
	} else {
		resp = types.GenerateResponse{Content: "done"}
	}
	s.mu.Unlock()

	promptTokens := ctxasm.EstimateMessages(req.Messages)
	completionTokens := ctxasm.EstimateMessageTokens(&types.Message{
		Role:      types.RoleAssistant,
		Content:   resp.Content,
		ToolCalls: resp.ToolCalls,
	})
	resp.Usage = types.Usage{
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      promptTokens + completionTokens,
	}
	return &resp, nil
}

func (s *scriptedLLM) GenerateStream(_ context.Context, _ *types.GenerateRequest, _ func(types.StreamChunk) error) error {
	return nil
}

func (s *scriptedLLM) Requests() []*types.GenerateRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*types.GenerateRequest, len(s.requests))
	copy(out, s.requests)
	return out
}

func newRegistry() *tools.Registry {
	reg := tools.NewRegistry()

	reg.Register(&tools.ToolSpec{
		Definition: &types.ToolDefinition{
			Name:        "echo",
			Description: "回显输入文本",
			Parameters: objectSchema(map[string]any{
				"text": map[string]any{"type": "string"},
			}, "text"),
		},
		Handler: func(_ context.Context, args map[string]any) (*types.ToolResult, error) {
			text, _ := args["text"].(string)
			return &types.ToolResult{OK: true, Output: text}, nil
		},
	})

	reg.Register(&tools.ToolSpec{
		Definition: &types.ToolDefinition{
			Name:        "big_output",
			Description: "生成指定行数的模拟日志",
			Parameters: objectSchema(map[string]any{
				"n": map[string]any{"type": "integer"},
			}),
		},
		Handler: func(_ context.Context, args map[string]any) (*types.ToolResult, error) {
			n := intArg(args, "n", 100)
			line := "模拟采集日志：设备状态正常，温度=42.0，批次号=B20260101。\n"
			return &types.ToolResult{OK: true, Output: strings.Repeat(line, n)}, nil
		},
	})

	// 名称故意叫 shell，让执行层按 Medium 风险处理；实现是安全的假命令。
	reg.Register(&tools.ToolSpec{
		Definition: &types.ToolDefinition{
			Name:        "shell",
			Description: "评测用安全 shell 替身",
			Parameters: objectSchema(map[string]any{
				"command": map[string]any{"type": "string"},
			}, "command"),
		},
		Handler: func(_ context.Context, _ map[string]any) (*types.ToolResult, error) {
			return &types.ToolResult{OK: true, Output: "fake shell output"}, nil
		},
	})

	return reg
}

func objectSchema(props map[string]any, required ...string) map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": props,
		"required":   required,
	}
}

func intArg(args map[string]any, key string, def int) int {
	switch v := args[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case string:
		n, err := strconv.Atoi(v)
		if err == nil {
			return n
		}
	}
	return def
}

// checkPairing 检查 assistant(tool_calls) 与 tool(tool_call_id) 的配对完整性。
func checkPairing(msgs []*types.Message) error {
	calls := make(map[string]bool)
	answered := make(map[string]bool)
	for _, m := range msgs {
		if m == nil {
			continue
		}
		if m.Role == types.RoleAssistant {
			for _, tc := range m.ToolCalls {
				if tc == nil {
					continue
				}
				if tc.ID == "" {
					return fmt.Errorf("tool call with empty id")
				}
				calls[tc.ID] = true
			}
		}
		if m.Role == types.RoleTool {
			if m.ToolCallID == "" {
				return fmt.Errorf("tool message with empty tool_call_id")
			}
			if !calls[m.ToolCallID] {
				return fmt.Errorf("orphan tool message %q", m.ToolCallID)
			}
			answered[m.ToolCallID] = true
		}
	}
	for id := range calls {
		if !answered[id] {
			return fmt.Errorf("unanswered tool call %q", id)
		}
	}
	return nil
}

// ---- 输出 ----

// Summary 是按模式聚合的结果。
type Summary struct {
	Mode           Mode
	Passed         int
	Total          int
	PromptTokens   int
	ContextTokens  int
	OriginalTokens int
}

// Summarize 按模式聚合。
func Summarize(results []Result) []Summary {
	order := make([]Mode, 0, 2)
	byMode := make(map[Mode]*Summary)
	for _, r := range results {
		s, ok := byMode[r.Mode]
		if !ok {
			s = &Summary{Mode: r.Mode}
			byMode[r.Mode] = s
			order = append(order, r.Mode)
		}
		s.Total++
		if r.Success {
			s.Passed++
		}
		s.PromptTokens += r.PromptTokens
		s.ContextTokens += r.ContextTokens
		s.OriginalTokens += r.ContextOriginalTokens
	}
	out := make([]Summary, 0, len(order))
	for _, m := range order {
		out = append(out, *byMode[m])
	}
	return out
}

// PrintTable 输出可读的评测表。
func PrintTable(w io.Writer, results []Result) {
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "TASK\tMODE\tOK\tSTEPS\tLLM\tTOOL\tPROMPT\tCTX/ORIG\tSUMM\tTRUNC\tTIME")
	for _, r := range results {
		ok := "✓"
		if !r.Success {
			ok = "✗"
		}
		note := ""
		if r.OverBudget {
			note = "!"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s%s\t%d\t%d\t%d\t%d\t%d/%d\t%d\t%d\t%s\n",
			r.Task, r.Mode, ok, note, r.Steps, r.LLMCalls, r.ToolCalls,
			r.PromptTokens, r.ContextTokens, r.ContextOriginalTokens,
			r.SummarizedMessages, r.TruncatedTools, r.Duration.Round(time.Millisecond))
	}
	tw.Flush()

	sums := Summarize(results)
	if len(sums) == 0 {
		return
	}
	fmt.Fprintln(w)
	for _, s := range sums {
		fmt.Fprintf(w, "[%s] passed %d/%d, prompt tokens=%d, context=%d, original=%d\n",
			s.Mode, s.Passed, s.Total, s.PromptTokens, s.ContextTokens, s.OriginalTokens)
	}
	if len(sums) == 2 {
		full, asm := sums[0], sums[1]
		if full.Mode != ModeFull {
			full, asm = asm, full
		}
		if full.PromptTokens > 0 {
			saved := 100 * (1 - float64(asm.PromptTokens)/float64(full.PromptTokens))
			fmt.Fprintf(w, "assembled vs full: prompt token saved %.1f%% (%d -> %d)\n",
				saved, full.PromptTokens, asm.PromptTokens)
		}
	}
}
