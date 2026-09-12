package tools

import (
	"context"
	"fmt"
	"sync"
	"time"

	"ai-cli-agent/pkg/types"
)

// ExecutorConfig 配置并发执行器的行为。
type ExecutorConfig struct {
	MaxConcurrency int           // 最大并发数（<=0 表示不限制）
	ToolTimeout    time.Duration // 单个工具调用超时（0 表示不限制）
	MaxRetries     int           // 最大重试次数（0 表示不重试）
	RetryBackoff   time.Duration // 重试退避基数（指数退避）
}

// DefaultExecutorConfig 返回合理默认配置。
func DefaultExecutorConfig() ExecutorConfig {
	return ExecutorConfig{
		MaxConcurrency: 5,
		ToolTimeout:    30 * time.Second,
		MaxRetries:     2,
		RetryBackoff:   500 * time.Millisecond,
	}
}

// ConcurrentExecutor 支持并发执行多个工具调用，带超时与重试。
type ConcurrentExecutor struct {
	registry *Registry
	config   ExecutorConfig
}

// NewConcurrentExecutor 创建并发执行器。
func NewConcurrentExecutor(registry *Registry, config ExecutorConfig) *ConcurrentExecutor {
	return &ConcurrentExecutor{registry: registry, config: config}
}

// batchResult 收集单个工具调用的结果。
type batchResult struct {
	index  int
	result *types.ToolResult
	err    error
}

// ExecuteBatch 并发执行多个工具调用。
// 结果按输入顺序返回，任何一个不可恢复的错误会导致整体失败。
func (e *ConcurrentExecutor) ExecuteBatch(ctx context.Context, calls []*types.ToolCall) ([]*types.ToolResult, error) {
	if len(calls) == 0 {
		return nil, nil
	}
	if len(calls) == 1 {
		// 单个调用走重试路径，不需要并发
		res, err := e.executeWithRetry(ctx, calls[0])
		if err != nil {
			return nil, err
		}
		return []*types.ToolResult{res}, nil
	}

	results := make([]*types.ToolResult, len(calls))
	ch := make(chan batchResult, len(calls))

	// 信号量控制并发数；MaxConcurrency <= 0 表示不限制并发。
	var sem chan struct{}
	if e.config.MaxConcurrency > 0 {
		sem = make(chan struct{}, e.config.MaxConcurrency)
	}
	var wg sync.WaitGroup

	for i, call := range calls {
		wg.Add(1)
		go func(idx int, c *types.ToolCall) {
			defer wg.Done()

			// 获取信号量；等待期间响应 context 取消，避免 goroutine 卡死。
			if sem != nil {
				select {
				case sem <- struct{}{}:
					defer func() { <-sem }()
				case <-ctx.Done():
					ch <- batchResult{index: idx, err: ctx.Err()}
					return
				}
			}

			res, err := e.executeWithRetry(ctx, c)
			ch <- batchResult{index: idx, result: res, err: err}
		}(i, call)
	}

	// 等待所有 goroutine 完成后关闭 channel
	go func() {
		wg.Wait()
		close(ch)
	}()

	// 收集结果
	var firstErr error
	for r := range ch {
		if r.err != nil && firstErr == nil {
			firstErr = r.err
		}
		results[r.index] = r.result
	}

	if firstErr != nil {
		return results, fmt.Errorf("batch execution failed: %w", firstErr)
	}
	return results, nil
}

// executeWithRetry 执行单个工具调用，支持超时与指数退避重试。
func (e *ConcurrentExecutor) executeWithRetry(ctx context.Context, call *types.ToolCall) (*types.ToolResult, error) {
	maxRetries := e.config.MaxRetries
	if maxRetries < 0 {
		maxRetries = 0
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			// 指数退避
			backoff := e.config.RetryBackoff * time.Duration(1<<(attempt-1))
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		// 为每次调用创建独立的超时 context
		callCtx := ctx
		var cancel context.CancelFunc
		if e.config.ToolTimeout > 0 {
			callCtx, cancel = context.WithTimeout(ctx, e.config.ToolTimeout)
		}

		res, err := e.registry.Execute(callCtx, call)
		if cancel != nil {
			cancel()
		}

		if err == nil {
			return res, nil
		}
		lastErr = err

		// 工具返回了结果但 OK=false，说明是业务错误，不重试
		if res != nil && !res.OK {
			return res, nil
		}
	}

	return nil, fmt.Errorf("tool %s failed after %d attempts: %w", call.Name, maxRetries+1, lastErr)
}
