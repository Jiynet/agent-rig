// Package mcp 实现 Model Context Protocol (MCP) 的 Server 端。
// 基于 stdio 传输的 JSON-RPC 2.0 协议，允许外部客户端（如 Claude Desktop）
// 通过标准输入/输出与 Agent 的工具进行交互。
//
// MCP 协议核心方法：
//   - initialize / initialized: 握手与能力协商
//   - tools/list: 列出可用工具
//   - tools/call: 调用指定工具
package mcp

import "encoding/json"

// JSONRPC 2.0 常量
const (
	Version           = "2.0"
	MethodInitialize  = "initialize"
	MethodInitialized = "notifications/initialized"
	MethodToolsList   = "tools/list"
	MethodToolsCall   = "tools/call"
)

// Request 是 JSON-RPC 2.0 请求。
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"` // 可以是 string 或 number
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response 是 JSON-RPC 2.0 响应。
type Response struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id"`
	Result  any    `json:"result,omitempty"`
	Error   *Error `json:"error,omitempty"`
}

// Error 是 JSON-RPC 2.0 错误。
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// 标准错误码
const (
	ParseError     = -32700
	InvalidRequest = -32600
	MethodNotFound = -32601
	InvalidParams  = -32602
	InternalError  = -32603
)

// InitializeParams 是 initialize 请求的参数。
type InitializeParams struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ClientCapabilities `json:"capabilities"`
	ClientInfo      ClientInfo         `json:"clientInfo"`
}

// ClientCapabilities 描述客户端支持的能力。
type ClientCapabilities struct {
	Roots    any `json:"roots,omitempty"`
	Sampling any `json:"sampling,omitempty"`
}

// ClientInfo 描述客户端信息。
type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

// InitializeResult 是 initialize 响应的结果。
type InitializeResult struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ServerCapabilities `json:"capabilities"`
	ServerInfo      ServerInfo         `json:"serverInfo"`
}

// ServerCapabilities 描述服务端支持的能力。
type ServerCapabilities struct {
	Tools *ToolsCapability `json:"tools,omitempty"`
}

// ToolsCapability 描述工具能力。
type ToolsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// ServerInfo 描述服务端信息。
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

// ToolsListResult 是 tools/list 响应的结果。
type ToolsListResult struct {
	Tools []Tool `json:"tools"`
}

// Tool 描述一个 MCP 工具。
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema"`
}

// ToolCallParams 是 tools/call 请求的参数。
type ToolCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

// ToolCallResult 是 tools/call 响应的结果。
type ToolCallResult struct {
	Content []ToolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// ToolContent 是工具返回的内容块。
type ToolContent struct {
	Type string `json:"type"` // "text" / "image" / "resource"
	Text string `json:"text,omitempty"`
}
