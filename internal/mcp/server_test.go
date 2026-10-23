package mcp_test

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"ai-cli-agent/internal/mcp"
	"ai-cli-agent/internal/tools"
	"ai-cli-agent/internal/tools/builtin"
)

// sendRequest 构造 JSON-RPC 请求。
func sendRequest(id any, method string, params any) []byte {
	req := mcp.Request{
		JSONRPC: mcp.Version,
		ID:      id,
		Method:  method,
	}
	if params != nil {
		data, _ := json.Marshal(params)
		req.Params = data
	}
	data, _ := json.Marshal(req)
	data = append(data, '\n')
	return data
}

// runMCPSession 模拟一次 MCP 会话：发送 requests，返回所有响应。
func runMCPSession(t *testing.T, registry *tools.Registry, requests ...[]byte) []mcp.Response {
	t.Helper()

	inR, inW := io.Pipe()
	outR, outW := io.Pipe()

	server := mcp.NewServer(inR, outW, registry)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		server.Run(ctx)
		outW.Close() // server 结束后关闭输出，让 reader 收到 EOF
	}()

	// 写入所有请求
	go func() {
		for _, req := range requests {
			inW.Write(req)
		}
		inW.Close()
	}()

	// 读取所有响应
	var responses []mcp.Response
	decoder := json.NewDecoder(outR)
	for {
		var resp mcp.Response
		if err := decoder.Decode(&resp); err != nil {
			break
		}
		responses = append(responses, resp)
	}

	return responses
}

func TestMCPInitialize(t *testing.T) {
	registry := tools.NewRegistry()
	responses := runMCPSession(t, registry,
		sendRequest(1, mcp.MethodInitialize, mcp.InitializeParams{
			ProtocolVersion: "2024-11-05",
			ClientInfo:      mcp.ClientInfo{Name: "test-client"},
		}),
	)

	if len(responses) == 0 {
		t.Fatal("no response received")
	}

	resp := responses[0]
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", resp.Result)
	}
	if result["protocolVersion"] != "2024-11-05" {
		t.Fatalf("unexpected protocol version: %v", result["protocolVersion"])
	}

	serverInfo, ok := result["serverInfo"].(map[string]any)
	if !ok {
		t.Fatal("expected serverInfo in result")
	}
	if serverInfo["name"] != "ai-cli-agent" {
		t.Fatalf("unexpected server name: %v", serverInfo["name"])
	}
}

func TestMCPToolsList(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(builtin.NewShellTool())
	registry.Register(builtin.NewFileReadTool())

	responses := runMCPSession(t, registry,
		sendRequest(1, mcp.MethodInitialize, mcp.InitializeParams{
			ProtocolVersion: "2024-11-05",
			ClientInfo:      mcp.ClientInfo{Name: "test"},
		}),
		sendRequest(nil, mcp.MethodInitialized, nil),
		sendRequest(2, mcp.MethodToolsList, nil),
	)

	// 找到 tools/list 响应（id=2）
	var toolsResp *mcp.Response
	for i := range responses {
		if responses[i].ID == float64(2) {
			toolsResp = &responses[i]
			break
		}
	}
	if toolsResp == nil {
		t.Fatal("tools/list response not found")
	}
	if toolsResp.Error != nil {
		t.Fatalf("unexpected error: %s", toolsResp.Error.Message)
	}

	result, ok := toolsResp.Result.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", toolsResp.Result)
	}

	toolList, ok := result["tools"].([]any)
	if !ok {
		t.Fatal("expected tools array in result")
	}
	if len(toolList) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(toolList))
	}
}

func TestMCPToolsCall(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(builtin.NewShellTool())

	responses := runMCPSession(t, registry,
		sendRequest(1, mcp.MethodInitialize, mcp.InitializeParams{
			ProtocolVersion: "2024-11-05",
			ClientInfo:      mcp.ClientInfo{Name: "test"},
		}),
		sendRequest(nil, mcp.MethodInitialized, nil),
		sendRequest(2, mcp.MethodToolsCall, mcp.ToolCallParams{
			Name:      "shell",
			Arguments: map[string]any{"command": "echo mcp-ok"},
		}),
	)

	// 找到 tools/call 响应
	var callResp *mcp.Response
	for i := range responses {
		if responses[i].ID == float64(2) {
			callResp = &responses[i]
			break
		}
	}
	if callResp == nil {
		t.Fatal("tools/call response not found")
	}
	if callResp.Error != nil {
		t.Fatalf("unexpected error: %s", callResp.Error.Message)
	}

	result, ok := callResp.Result.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", callResp.Result)
	}

	content, ok := result["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatal("expected content in result")
	}

	firstContent, ok := content[0].(map[string]any)
	if !ok {
		t.Fatal("expected content item to be map")
	}
	if !strings.Contains(firstContent["text"].(string), "mcp-ok") {
		t.Fatalf("expected 'mcp-ok' in output, got: %v", firstContent["text"])
	}
}

func TestMCPUnknownMethod(t *testing.T) {
	registry := tools.NewRegistry()
	responses := runMCPSession(t, registry,
		sendRequest(1, "unknown/method", nil),
	)

	if len(responses) == 0 {
		t.Fatal("no response received")
	}

	resp := responses[0]
	if resp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if resp.Error.Code != mcp.MethodNotFound {
		t.Fatalf("expected method not found error, got code: %d", resp.Error.Code)
	}
}

func TestMCPTransportParseError(t *testing.T) {
	registry := tools.NewRegistry()

	inR, inW := io.Pipe()
	outR, outW := io.Pipe()

	server := mcp.NewServer(inR, outW, registry)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		server.Run(ctx)
		outW.Close()
	}()

	go func() {
		inW.Write([]byte("not json\n"))
		inW.Close()
	}()

	decoder := json.NewDecoder(outR)
	var resp mcp.Response
	if err := decoder.Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected parse error")
	}
	if resp.Error.Code != mcp.ParseError {
		t.Fatalf("expected parse error code, got: %d", resp.Error.Code)
	}
}

func TestMCPServerEOF(t *testing.T) {
	registry := tools.NewRegistry()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()

	server := mcp.NewServer(inR, outW, registry)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Run(ctx)
		outW.Close()
	}()

	// 立即关闭输入
	inW.Close()

	// 消费输出直到 EOF
	go func() {
		io.ReadAll(outR)
	}()

	err := <-errCh
	if err != nil {
		t.Fatalf("expected nil error on EOF, got: %v", err)
	}
}

func TestMCPServerContextCancel(t *testing.T) {
	registry := tools.NewRegistry()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()

	server := mcp.NewServer(inR, outW, registry)

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Run(ctx)
		outW.Close()
	}()

	// 取消 context（触发 closer 关闭 inR，导致 Read 返回 EOF）
	cancel()
	inW.Close()

	// 消费输出
	go func() {
		io.ReadAll(outR)
	}()

	err := <-errCh
	// context 取消后，server 可能返回 ctx.Err() 或 nil（EOF 先到达）
	if err != nil && err != context.Canceled {
		t.Fatalf("expected nil or context.Canceled, got: %v", err)
	}
}
