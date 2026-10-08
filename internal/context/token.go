// Package context 提供预算化的上下文组装能力：
// 在有限 token 预算内，决定哪些消息原文保留、哪些压缩、哪些工具输出截断，
// 并保证 assistant(tool_calls) 与 tool(tool_call_id) 的配对不被破坏。
package context

import (
	"encoding/json"
	"fmt"
	"unicode"

	"ai-cli-agent/pkg/types"
)

// EstimateTokens 是保守的 token 估算：
// CJK 字符按 1 token/字，其他字符按 4 字符/token。
// 真实 tokenizer 因模型而异，这里用于预算分配，不要求精确。
func EstimateTokens(text string) int {
	if text == "" {
		return 0
	}
	var cjk, other int
	for _, r := range text {
		if isCJK(r) {
			cjk++
		} else {
			other++
		}
	}
	t := cjk + (other+3)/4
	if t < 1 {
		t = 1
	}
	return t
}

// EstimateMessageTokens 估算单条消息的 token（含角色与工具调用开销）。
func EstimateMessageTokens(m *types.Message) int {
	if m == nil {
		return 0
	}
	t := 4 + EstimateTokens(m.Content)
	if m.ToolCallID != "" {
		t += 2
	}
	for _, tc := range m.ToolCalls {
		if tc == nil {
			continue
		}
		t += 8 + EstimateTokens(tc.Name) + estimateInputTokens(tc.Input)
	}
	return t
}

// EstimateMessages 估算一组消息的 token。
func EstimateMessages(msgs []*types.Message) int {
	total := 0
	for _, m := range msgs {
		total += EstimateMessageTokens(m)
	}
	return total
}

func estimateInputTokens(input map[string]any) int {
	if len(input) == 0 {
		return 0
	}
	b, err := json.Marshal(input)
	if err != nil {
		return EstimateTokens(fmt.Sprint(input))
	}
	return EstimateTokens(string(b))
}

func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) ||
		unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) ||
		unicode.Is(unicode.Hangul, r) ||
		(r >= 0x3000 && r <= 0x303F) || // CJK 标点
		(r >= 0xFF00 && r <= 0xFFEF) // 全角字符
}

// truncateToTokens 保留文本头部约 60%、尾部约 40%，中间用标记替代。
// 用二分查找保证截断后不超过 maxTokens（估算值）。
func truncateToTokens(text string, maxTokens int) string {
	if maxTokens <= 0 || EstimateTokens(text) <= maxTokens {
		return text
	}
	runes := []rune(text)
	if len(runes) == 0 {
		return ""
	}

	best := ""
	low, high := 0, len(runes)
	for low <= high {
		mid := (low + high) / 2
		head := mid * 3 / 5
		tail := mid - head
		omitted := len(runes) - mid

		var candidate string
		if tail > 0 {
			candidate = string(runes[:head]) +
				fmt.Sprintf("\n...[truncated %d chars]...\n", omitted) +
				string(runes[len(runes)-tail:])
		} else {
			candidate = string(runes[:head]) +
				fmt.Sprintf("\n...[truncated %d chars]...", omitted)
		}

		if EstimateTokens(candidate) <= maxTokens {
			best = candidate
			low = mid + 1
		} else {
			high = mid - 1
		}
	}
	if best == "" {
		return fmt.Sprintf("...[truncated %d chars]...", len(runes))
	}
	return best
}
