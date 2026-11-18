// mcp-server 是 AgentRig 的 MCP 协议服务端入口。
// 通过 stdio（stdin/stdout）以 JSON-RPC 2.0 协议暴露已注册的工具，
// 允许外部客户端（如 Claude Desktop）调用 Agent 的工具。
//
// 使用方式：
//
//	echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' | ./bin/mcp-server
//
// 在 Claude Desktop 中配置：
//
//	{
//	  "mcpServers": {
//	    "agent-rig": {
//	      "command": "/path/to/bin/mcp-server"
//	    }
//	  }
//	}
package main

import (
	"context"
	"log"
	"os"

	"ai-cli-agent/internal/mcp"
	"ai-cli-agent/internal/tools"
	"ai-cli-agent/internal/tools/builtin"
)

func main() {
	registry := tools.NewRegistry()
	for _, spec := range builtin.All() {
		registry.Register(spec)
	}

	server := mcp.NewServer(os.Stdin, os.Stdout, registry)

	if err := server.Run(context.Background()); err != nil {
		log.Fatalf("mcp server error: %v", err)
	}
}
