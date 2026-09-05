package tools

import (
	"context"

	"ai-cli-agent/pkg/types"
)

// ToolHandler 是单个工具的执行函数签名。
type ToolHandler func(ctx context.Context, args map[string]any) (*types.ToolResult, error)

// ToolSpec 描述一个已注册工具：元信息 + 执行函数。
type ToolSpec struct {
	Definition *types.ToolDefinition
	Handler    ToolHandler
}
