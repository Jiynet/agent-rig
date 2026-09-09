package memory

import (
	"context"

	"ai-cli-agent/pkg/types"
)

// Noop 是空操作记忆实现，不存储也不检索。
type Noop struct{}

func NewNoop() *Noop { return &Noop{} }

func (n *Noop) Store(_ context.Context, _ *types.MemoryEntry) error { return nil }
func (n *Noop) Retrieve(_ context.Context, _ string, _ int) ([]*types.MemoryEntry, error) {
	return nil, nil
}
