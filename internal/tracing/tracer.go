// Package tracing 提供 Agent 执行全链路的结构化追踪能力。
// 通过 Span 记录每个执行步骤的耗时、状态与属性，
// 通过 Metrics 汇总一次执行的累计指标。
package tracing

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"ai-cli-agent/pkg/types"
)

// Tracer 是追踪器的抽象接口。
type Tracer interface {
	// Start 开始一个新的跨度，返回可操作的 SpanHandle。
	Start(name string, attrs map[string]any) SpanHandle
	// Metrics 返回当前累计指标的快照。
	Metrics() types.AgentMetrics
	// Reset 重置所有追踪状态（用于新一轮执行）。
	Reset()
}

// SpanHandle 是一个活跃跨度的操作句柄。
type SpanHandle interface {
	// SetAttr 设置跨度属性（可多次调用）。
	SetAttr(key string, value any)
	// End 结束跨度并记录状态。
	End(status types.SpanStatus, err error)
	// Child 在当前跨度下创建子跨度。
	StartChild(name string, attrs map[string]any) SpanHandle
}

// ConsoleTracer 将追踪信息输出到 io.Writer（默认 stderr）。
type ConsoleTracer struct {
	mu      sync.Mutex
	out     io.Writer
	root    *types.Span
	stack   []*types.Span // 当前跨度栈
	metrics types.AgentMetrics
	seq     uint64
}

// NewConsoleTracer 创建控制台追踪器。
func NewConsoleTracer(out io.Writer) *ConsoleTracer {
	if out == nil {
		out = os.Stderr
	}
	return &ConsoleTracer{out: out}
}

// spanHandle 实现 SpanHandle。
type spanHandle struct {
	tracer *ConsoleTracer
	span   *types.Span
}

func (t *ConsoleTracer) nextID() string {
	t.seq++
	return fmt.Sprintf("span_%d", t.seq)
}

func (t *ConsoleTracer) Start(name string, attrs map[string]any) SpanHandle {
	t.mu.Lock()
	defer t.mu.Unlock()

	span := &types.Span{
		ID:         t.nextID(),
		Name:       name,
		StartTime:  time.Now(),
		Attributes: attrs,
		Status:     types.SpanOK,
	}

	if len(t.stack) > 0 {
		parent := t.stack[len(t.stack)-1]
		span.ParentID = parent.ID
		parent.Children = append(parent.Children, span)
	} else {
		t.root = span
	}
	t.stack = append(t.stack, span)

	return &spanHandle{tracer: t, span: span}
}

func (h *spanHandle) SetAttr(key string, value any) {
	h.tracer.mu.Lock()
	defer h.tracer.mu.Unlock()
	if h.span.Attributes == nil {
		h.span.Attributes = make(map[string]any)
	}
	h.span.Attributes[key] = value
}

func (h *spanHandle) End(status types.SpanStatus, err error) {
	h.tracer.mu.Lock()
	defer h.tracer.mu.Unlock()

	h.span.EndTime = time.Now()
	h.span.Duration = h.span.EndTime.Sub(h.span.StartTime)
	h.span.Status = status
	if err != nil {
		h.span.Error = err.Error()
		h.span.Status = types.SpanError
	}

	// 从栈中移除
	stack := h.tracer.stack
	for i := len(stack) - 1; i >= 0; i-- {
		if stack[i].ID == h.span.ID {
			h.tracer.stack = append(stack[:i], stack[i+1:]...)
			break
		}
	}

	// 更新累计指标
	h.tracer.updateMetrics(h.span)

	// 输出到控制台
	h.tracer.printSpan(h.span, 0)
}

func (h *spanHandle) StartChild(name string, attrs map[string]any) SpanHandle {
	h.tracer.mu.Lock()
	defer h.tracer.mu.Unlock()

	span := &types.Span{
		ID:         h.tracer.nextID(),
		ParentID:   h.span.ID,
		Name:       name,
		StartTime:  time.Now(),
		Attributes: attrs,
		Status:     types.SpanOK,
	}
	h.span.Children = append(h.span.Children, span)
	h.tracer.stack = append(h.tracer.stack, span)

	return &spanHandle{tracer: h.tracer, span: span}
}

func (t *ConsoleTracer) updateMetrics(span *types.Span) {
	switch span.Name {
	case "llm.generate":
		t.metrics.LLMCalls++
		if v, ok := span.Attributes["prompt_tokens"]; ok {
			if n, ok := v.(int); ok {
				t.metrics.PromptTokens += n
			}
		}
		if v, ok := span.Attributes["completion_tokens"]; ok {
			if n, ok := v.(int); ok {
				t.metrics.CompletionTokens += n
			}
		}
		if v, ok := span.Attributes["total_tokens"]; ok {
			if n, ok := v.(int); ok {
				t.metrics.TotalTokens += n
			}
		}
		if v, ok := span.Attributes["context_tokens"]; ok {
			if n, ok := v.(int); ok {
				t.metrics.ContextTokens += n
			}
		}
		if v, ok := span.Attributes["context_original_tokens"]; ok {
			if n, ok := v.(int); ok {
				t.metrics.ContextOriginalTokens += n
			}
		}
		if v, ok := span.Attributes["context_summarized"]; ok {
			if n, ok := v.(int); ok {
				t.metrics.ContextSummarized += n
			}
		}
		if v, ok := span.Attributes["context_truncated_tools"]; ok {
			if n, ok := v.(int); ok {
				t.metrics.ContextTruncatedTools += n
			}
		}
	case "tool.execute":
		t.metrics.ToolCalls++
		if span.Status == types.SpanError {
			t.metrics.ToolErrors++
		}
	case "tool.execute_batch":
		if v, ok := span.Attributes["tool_calls"]; ok {
			if n, ok := v.(int); ok {
				t.metrics.ToolCalls += n
			}
		}
		if v, ok := span.Attributes["tool_errors"]; ok {
			if n, ok := v.(int); ok {
				t.metrics.ToolErrors += n
			}
		}
	case "react.iteration":
		t.metrics.LoopIterations++
	}
}

func (t *ConsoleTracer) printSpan(span *types.Span, depth int) {
	indent := ""
	for i := 0; i < depth; i++ {
		indent += "  "
	}
	statusIcon := "✓"
	if span.Status == types.SpanError {
		statusIcon = "✗"
	} else if span.Status == types.SpanSkip {
		statusIcon = "⊘"
	}

	fmt.Fprintf(t.out, "%s%s %s [%dms]", indent, statusIcon, span.Name, span.Duration.Milliseconds())
	if span.Error != "" {
		fmt.Fprintf(t.out, " err=%s", span.Error)
	}
	// 输出关键属性
	for k, v := range span.Attributes {
		if k == "prompt_tokens" || k == "completion_tokens" || k == "total_tokens" ||
			k == "model" || k == "tool_name" || k == "iterations" || k == "max_iterations" ||
			k == "context_tokens" {
			fmt.Fprintf(t.out, " %s=%v", k, v)
		}
	}
	fmt.Fprintln(t.out)

	for _, child := range span.Children {
		t.printSpan(child, depth+1)
	}
}

func (t *ConsoleTracer) Metrics() types.AgentMetrics {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.metrics
}

func (t *ConsoleTracer) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.root = nil
	t.stack = nil
	t.metrics = types.AgentMetrics{}
	t.seq = 0
}
