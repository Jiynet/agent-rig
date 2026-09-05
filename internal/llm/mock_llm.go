package llm

import (
	"context"

	"ai-cli-agent/pkg/types"
)

// MockLLM 是测试/演示用的假实现，返回固定内容。
type MockLLM struct {
	Response string
	Err      error
}

// Generate 返回预设内容，不发起真实请求。
func (m *MockLLM) Generate(_ context.Context, _ *types.GenerateRequest) (*types.GenerateResponse, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	return &types.GenerateResponse{Content: m.Response}, nil
}

// GenerateStream 返回预设内容作为单个流式分片。
func (m *MockLLM) GenerateStream(_ context.Context, _ *types.GenerateRequest, fn func(chunk types.StreamChunk) error) error {
	if m.Err != nil {
		return m.Err
	}
	if err := fn(types.StreamChunk{Delta: m.Response}); err != nil {
		return err
	}
	return fn(types.StreamChunk{Done: true})
}

// ensure MockLLM implements LLM.
var _ LLM = (*MockLLM)(nil)
