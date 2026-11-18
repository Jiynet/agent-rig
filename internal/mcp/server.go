package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"ai-cli-agent/internal/tools"
	"ai-cli-agent/pkg/types"
)

const (
	serverName      = "agent-rig"
	serverVersion   = "0.1.0"
	protocolVersion = "2024-11-05"
)

// Server 是 MCP 协议的服务端实现。
type Server struct {
	transport   *StdioTransport
	registry    *tools.Registry
	initialized bool
	closer      io.Closer // 可选：用于在 context 取消时关闭输入
}

// NewServer 创建 MCP 服务端。in 实现 io.Closer 时会在 context 取消时自动关闭。
func NewServer(in io.Reader, out io.Writer, registry *tools.Registry) *Server {
	s := &Server{
		transport: NewStdioTransport(in, out),
		registry:  registry,
	}
	if c, ok := in.(io.Closer); ok {
		s.closer = c
	}
	return s
}

// Run 启动 MCP 服务端主循环，阻塞直到 EOF 或错误。
func (s *Server) Run(ctx context.Context) error {
	// 监听 context 取消，关闭输入以中断阻塞的 Read
	if s.closer != nil {
		go func() {
			<-ctx.Done()
			_ = s.closer.Close()
		}()
	}

	for {
		req, err := s.transport.Read()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			// 检查是否因为 context 取消
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// 解析错误，返回 parse error
			_ = s.transport.Write(&Response{
				JSONRPC: Version,
				Error:   &Error{Code: ParseError, Message: err.Error()},
			})
			continue
		}

		resp := s.handleRequest(ctx, req)
		if resp != nil {
			if err := s.transport.Write(resp); err != nil {
				return fmt.Errorf("write response: %w", err)
			}
		}
	}
}

// handleRequest 分发请求到对应的处理方法。
func (s *Server) handleRequest(ctx context.Context, req *Request) *Response {
	switch req.Method {
	case MethodInitialize:
		return s.handleInitialize(req)
	case MethodInitialized:
		// notifications/initialized 是通知，不需要响应
		s.initialized = true
		return nil
	case MethodToolsList:
		return s.handleToolsList(req)
	case MethodToolsCall:
		return s.handleToolsCall(ctx, req)
	default:
		return &Response{
			JSONRPC: Version,
			ID:      req.ID,
			Error:   &Error{Code: MethodNotFound, Message: fmt.Sprintf("method not found: %s", req.Method)},
		}
	}
}

// handleInitialize 处理握手请求。
func (s *Server) handleInitialize(req *Request) *Response {
	var params InitializeParams
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return &Response{
				JSONRPC: Version,
				ID:      req.ID,
				Error:   &Error{Code: InvalidParams, Message: "invalid initialize params"},
			}
		}
	}

	return &Response{
		JSONRPC: Version,
		ID:      req.ID,
		Result: InitializeResult{
			ProtocolVersion: protocolVersion,
			Capabilities: ServerCapabilities{
				Tools: &ToolsCapability{ListChanged: false},
			},
			ServerInfo: ServerInfo{
				Name:    serverName,
				Version: serverVersion,
			},
		},
	}
}

// handleToolsList 处理工具列表请求。
func (s *Server) handleToolsList(req *Request) *Response {
	defs, err := s.registry.List(context.Background())
	if err != nil {
		return &Response{
			JSONRPC: Version,
			ID:      req.ID,
			Error:   &Error{Code: InternalError, Message: fmt.Sprintf("list tools: %v", err)},
		}
	}

	tools := make([]Tool, 0, len(defs))
	for _, def := range defs {
		tools = append(tools, Tool{
			Name:        def.Name,
			Description: def.Description,
			InputSchema: def.Parameters,
		})
	}

	return &Response{
		JSONRPC: Version,
		ID:      req.ID,
		Result:  ToolsListResult{Tools: tools},
	}
}

// handleToolsCall 处理工具调用请求。
func (s *Server) handleToolsCall(ctx context.Context, req *Request) *Response {
	var params ToolCallParams
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return &Response{
				JSONRPC: Version,
				ID:      req.ID,
				Error:   &Error{Code: InvalidParams, Message: "invalid tool call params"},
			}
		}
	}

	if params.Name == "" {
		return &Response{
			JSONRPC: Version,
			ID:      req.ID,
			Error:   &Error{Code: InvalidParams, Message: "tool name is required"},
		}
	}

	result, err := s.registry.Execute(ctx, &types.ToolCall{
		Name:  params.Name,
		Input: params.Arguments,
	})
	if err != nil {
		return &Response{
			JSONRPC: Version,
			ID:      req.ID,
			Result: ToolCallResult{
				Content: []ToolContent{{Type: "text", Text: fmt.Sprintf("Error: %v", err)}},
				IsError: true,
			},
		}
	}

	return &Response{
		JSONRPC: Version,
		ID:      req.ID,
		Result: ToolCallResult{
			Content: []ToolContent{{Type: "text", Text: result.Output}},
			IsError: !result.OK,
		},
	}
}
