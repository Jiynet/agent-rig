package tools

import (
	"context"
	"fmt"

	"ai-cli-agent/pkg/types"
)

// Registry 管理工具的注册与分发。
type Registry struct {
	index map[string]*ToolSpec
}

// NewRegistry 创建一个空的工具注册表。
func NewRegistry() *Registry {
	return &Registry{index: make(map[string]*ToolSpec)}
}

// Register 注册一个工具，同名工具会被覆盖。
func (r *Registry) Register(spec *ToolSpec) {
	r.index[spec.Definition.Name] = spec
}

// Execute 执行单个工具调用。
func (r *Registry) Execute(ctx context.Context, call *types.ToolCall) (*types.ToolResult, error) {
	spec, ok := r.index[call.Name]
	if !ok {
		return nil, &UnknownToolError{Name: call.Name}
	}
	return spec.Handler(ctx, call.Input)
}

// ExecuteBatch 依次执行多个工具调用，遇到错误即返回已完成的批次。
func (r *Registry) ExecuteBatch(ctx context.Context, calls []*types.ToolCall) ([]*types.ToolResult, error) {
	results := make([]*types.ToolResult, 0, len(calls))
	for _, call := range calls {
		res, err := r.Execute(ctx, call)
		if err != nil {
			return results, err
		}
		results = append(results, res)
	}
	return results, nil
}

// List 返回所有已注册工具的定义，供 LLM 感知可用工具。
func (r *Registry) List(_ context.Context) ([]*types.ToolDefinition, error) {
	defs := make([]*types.ToolDefinition, 0, len(r.index))
	for _, spec := range r.index {
		defs = append(defs, spec.Definition)
	}
	return defs, nil
}

// UnknownToolError 表示尝试调用一个未注册的工具。
type UnknownToolError struct{ Name string }

func (e *UnknownToolError) Error() string { return fmt.Sprintf("unknown tool: %s", e.Name) }

// ensure Registry implements ToolExecutor.
var _ ToolExecutor = (*Registry)(nil)
