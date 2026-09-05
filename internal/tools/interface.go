// Package tools 提供工具集：工具的注册、分发与批量执行。
package tools

import (
	"context"

	"ai-cli-agent/pkg/types"
)

// ToolExecutor 是工具执行器的抽象接口。
type ToolExecutor interface {
	Execute(ctx context.Context, call *types.ToolCall) (*types.ToolResult, error)
	ExecuteBatch(ctx context.Context, calls []*types.ToolCall) ([]*types.ToolResult, error)
	List(ctx context.Context) ([]*types.ToolDefinition, error)
}
