// Package builtin 提供 Agent 内置的工具实现。
package builtin

import "ai-cli-agent/internal/tools"

// All 返回全部内置工具，供组装层批量注册。
func All() []*tools.ToolSpec {
	return []*tools.ToolSpec{
		NewShellTool(),
		NewFileReadTool(),
		NewFileWriteTool(),
		NewWebSearchTool(),
		NewHTTPRequestTool(),
	}
}
