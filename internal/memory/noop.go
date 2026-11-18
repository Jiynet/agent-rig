package memory

import (
	"context"

	"ai-cli-agent/pkg/types"
)

// Noop 是空操作记忆实现，不存储也不检索。
type Noop struct{}

// NewNoop 创建空操作记忆。
func NewNoop() *Noop { return &Noop{} }

// Store 忽略写入。
func (n *Noop) Store(_ context.Context, _ *types.MemoryEntry) error { return nil }

// Retrieve 总是返回空结果。
func (n *Noop) Retrieve(_ context.Context, _ string, _ int) ([]*types.MemoryEntry, error) {
	return nil, nil
}
