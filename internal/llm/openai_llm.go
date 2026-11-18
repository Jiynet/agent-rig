package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"ai-cli-agent/pkg/types"
)

// OpenAILLM 是 OpenAI 兼容接口（/v1/chat/completions）的实现，
// 兼容 OpenAI、DeepSeek、vLLM、Ollama 等提供方。基于 net/http，无第三方依赖。
type OpenAILLM struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

// NewOpenAILLM 构造一个 OpenAI 兼容客户端。
// baseURL 留空时使用 https://api.openai.com/v1。
func NewOpenAILLM(apiKey, baseURL string) *OpenAILLM {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	return &OpenAILLM{
		apiKey:     apiKey,
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{},
	}
}

// openAIChatMessage 是对应 OpenAI API 的对话消息结构。
type openAIChatMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
}

// openAIToolCall 对应 OpenAI API 的 tool_calls 元素。
type openAIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"` // JSON 字符串
	} `json:"function"`
}

// openAIRequest 对应 OpenAI API 的请求体。
type openAIRequest struct {
	Model       string              `json:"model"`
	Messages    []openAIChatMessage `json:"messages"`
	Temperature float64             `json:"temperature,omitempty"`
	MaxTokens   int                 `json:"max_tokens,omitempty"`
	Stream      bool                `json:"stream,omitempty"`
	Tools       []any               `json:"tools,omitempty"`
}

// openAIResponse 对应 OpenAI API 的非流式响应体。
type openAIResponse struct {
	Choices []struct {
		Message struct {
			Content   string           `json:"content"`
			ToolCalls []openAIToolCall `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// Generate 发起一次非流式对话。
func (l *OpenAILLM) Generate(ctx context.Context, req *types.GenerateRequest) (*types.GenerateResponse, error) {
	body, err := l.buildBody(req, false)
	if err != nil {
		return nil, err
	}

	httpReq, err := l.newRequest(ctx, body)
	if err != nil {
		return nil, err
	}

	resp, err := l.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("llm request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("llm api error status=%d body=%s", resp.StatusCode, string(raw))
	}

	var parsed openAIResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	out := &types.GenerateResponse{
		Usage: types.Usage{
			PromptTokens:     parsed.Usage.PromptTokens,
			CompletionTokens: parsed.Usage.CompletionTokens,
			TotalTokens:      parsed.Usage.TotalTokens,
		},
	}
	if len(parsed.Choices) > 0 {
		out.Content = parsed.Choices[0].Message.Content
		for _, tc := range parsed.Choices[0].Message.ToolCalls {
			call := &types.ToolCall{ID: tc.ID, Name: tc.Function.Name}
			// 参数是 JSON 字符串，尽量解析为 map，失败时保留原文。
			if tc.Function.Arguments != "" {
				var m map[string]any
				if json.Unmarshal([]byte(tc.Function.Arguments), &m) == nil {
					call.Input = m
				}
			}
			out.ToolCalls = append(out.ToolCalls, call)
		}
	}
	return out, nil
}

// GenerateStream 发起流式对话，将每个增量分片回调给 fn。
func (l *OpenAILLM) GenerateStream(ctx context.Context, req *types.GenerateRequest, fn func(chunk types.StreamChunk) error) error {
	body, err := l.buildBody(req, true)
	if err != nil {
		return err
	}

	httpReq, err := l.newRequest(ctx, body)
	if err != nil {
		return err
	}

	resp, err := l.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("llm stream request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("llm api error status=%d body=%s", resp.StatusCode, string(raw))
	}

	// 解析 SSE 流：每行以 "data:" 开头，遇 "[DONE]" 结束。
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			return fn(types.StreamChunk{Done: true})
		}
		var chunk openAIStreamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue // 忽略无法解析的分片
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		delta := chunk.Choices[0].Delta
		c := types.StreamChunk{Delta: delta.Content}
		if len(delta.ToolCalls) > 0 {
			tc := delta.ToolCalls[0]
			c.ToolCall = &types.ToolCall{ID: tc.ID, Name: tc.Function.Name, Input: map[string]any{"arguments": tc.Function.Arguments}}
		}
		if err := fn(c); err != nil {
			return err
		}
	}
	return scanner.Err()
}

// openAIStreamChunk 对应 OpenAI API 流式响应的单个 data 块。
type openAIStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content   string           `json:"content"`
			ToolCalls []openAIToolCall `json:"tool_calls"`
		} `json:"delta"`
	} `json:"choices"`
}

// buildBody 组装请求体。
func (l *OpenAILLM) buildBody(req *types.GenerateRequest, stream bool) ([]byte, error) {
	messages := make([]openAIChatMessage, 0, len(req.Messages)+1)
	if req.System != "" {
		messages = append(messages, openAIChatMessage{Role: "system", Content: req.System})
	}
	for _, m := range req.Messages {
		if m == nil {
			continue
		}
		om := openAIChatMessage{Role: string(m.Role), Content: m.Content}
		if m.ToolCallID != "" {
			om.ToolCallID = m.ToolCallID
		}
		for _, tc := range m.ToolCalls {
			if tc == nil {
				continue
			}
			om.ToolCalls = append(om.ToolCalls, openAIToolCall{
				ID:   tc.ID,
				Type: "function",
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{
					Name:      tc.Name,
					Arguments: toolArgumentsJSON(tc.Input),
				},
			})
		}
		messages = append(messages, om)
	}

	var tools []any
	for _, t := range req.Tools {
		if t == nil {
			continue
		}
		tools = append(tools, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  t.Parameters,
			},
		})
	}

	body, err := json.Marshal(openAIRequest{
		Model:       req.Model,
		Messages:    messages,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		Stream:      stream,
		Tools:       tools,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	return body, nil
}

// toolArgumentsJSON 将工具入参序列化为 OpenAI 要求的 JSON 字符串；为空时回退为 "{}"。
func toolArgumentsJSON(input map[string]any) string {
	if input == nil {
		return "{}"
	}
	b, err := json.Marshal(input)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// newRequest 构造带鉴权的 HTTP 请求。
func (l *OpenAILLM) newRequest(ctx context.Context, body []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, l.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if l.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+l.apiKey)
	}
	return req, nil
}

// ensure OpenAILLM implements LLM.
var _ LLM = (*OpenAILLM)(nil)
