// Package llm 封装大语言模型调用，属于纯基础设施层，
// 仅依赖 pkg/types，不感知上层业务。
package llm

import (
	"context"

	"ai-cli-agent/pkg/types"
)

// LLM 是大语言模型客户端的抽象接口。
type LLM interface {
	Generate(ctx context.Context, req *types.GenerateRequest) (*types.GenerateResponse, error)
	GenerateStream(ctx context.Context, req *types.GenerateRequest, fn func(chunk types.StreamChunk) error) error
}
