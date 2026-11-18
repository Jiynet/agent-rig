package tracing

import "ai-cli-agent/pkg/types"

// NoopTracer 是空操作追踪器，不输出任何信息。
// 用于 Tracer 未配置时的默认值。
type NoopTracer struct{}

// NewNoopTracer 创建空操作追踪器。
func NewNoopTracer() *NoopTracer { return &NoopTracer{} }

// Start 返回一个不做任何事的 SpanHandle。
func (n *NoopTracer) Start(_ string, _ map[string]any) SpanHandle { return &noopHandle{} }

// Metrics 返回零值指标。
func (n *NoopTracer) Metrics() types.AgentMetrics { return types.AgentMetrics{} }

// Reset 是空操作。
func (n *NoopTracer) Reset() {}

type noopHandle struct{}

func (h *noopHandle) SetAttr(_ string, _ any)                          {}
func (h *noopHandle) End(_ types.SpanStatus, _ error)                  {}
func (h *noopHandle) StartChild(_ string, _ map[string]any) SpanHandle { return h }
