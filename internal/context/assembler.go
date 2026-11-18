package context

import (
	"fmt"
	"strings"

	"ai-cli-agent/pkg/types"
)

// Config 配置上下文组装行为。
type Config struct {
	// MaxPromptTokens 是提示词的 token 预算（估算值）。
	// <= 0 表示关闭组装，全量历史透传。
	MaxPromptTokens int
	// RecentMessages 是至少保留原文的最近消息条数（按消息计，不拆散工具调用单元）。
	RecentMessages int
	// MaxToolOutputTokens 是单条工具观察的最大 token。
	MaxToolOutputTokens int
	// SummaryMaxTokens 是旧轮次摘要的最大 token。
	SummaryMaxTokens int
}

// DefaultConfig 返回适合终端 Agent 的默认预算。
func DefaultConfig() Config {
	return Config{
		MaxPromptTokens:     6000,
		RecentMessages:      8,
		MaxToolOutputTokens: 400,
		SummaryMaxTokens:    300,
	}
}

// Stats 记录一次组装的效果。
type Stats struct {
	OriginalTokens        int // 组装前历史估算 token
	AssembledTokens       int // 组装后估算 token
	Budget                int // 配置的预算
	SummarizedMessages    int // 被压缩进摘要的消息数
	TruncatedToolMessages int // 被截断的工具观察数
	SummaryUsed           bool
	OverBudget            bool
}

// Assembler 在预算内组装上下文。
type Assembler struct {
	cfg Config
}

// NewAssembler 创建组装器；预算 > 0 时补齐零值配置。
func NewAssembler(cfg Config) *Assembler {
	if cfg.MaxPromptTokens > 0 {
		if cfg.RecentMessages <= 0 {
			cfg.RecentMessages = 1
		}
		if cfg.MaxToolOutputTokens <= 0 {
			cfg.MaxToolOutputTokens = 200
		}
		if cfg.SummaryMaxTokens <= 0 {
			cfg.SummaryMaxTokens = 200
		}
	}
	return &Assembler{cfg: cfg}
}

// Config 返回当前配置。
func (a *Assembler) Config() Config { return a.cfg }

// unit 是一个不可拆分的消息组：
// 普通消息自成一个 unit；assistant(tool_calls) 与后续 tool 消息合成一个 unit，
// 保证裁剪时不会产生孤立的工具调用或工具结果。
type unit struct {
	messages  []*types.Message
	pinned    bool
	prepared  []*types.Message
	tokens    int
	toolMsgs  int
	truncated int
}

// Build 在预算内组装消息。返回新切片，不修改 history。
func (a *Assembler) Build(history []*types.Message) ([]*types.Message, Stats) {
	stats := Stats{Budget: a.cfg.MaxPromptTokens}
	stats.OriginalTokens = EstimateMessages(history)

	// 未启用组装：全量透传。
	if a.cfg.MaxPromptTokens <= 0 || len(history) == 0 {
		out := cloneNonNil(history)
		stats.AssembledTokens = EstimateMessages(out)
		return out, stats
	}

	units := a.buildUnits(history)
	for _, u := range units {
		u.prepared, u.truncated = a.prepare(u.messages, a.cfg.MaxToolOutputTokens)
		u.tokens = EstimateMessages(u.prepared)
		u.toolMsgs = countToolMessages(u.messages)
	}

	// 预留 5% 安全余量，抵消估算误差。
	alloc := a.cfg.MaxPromptTokens * 95 / 100
	include := make([]bool, len(units))

	pinnedTokens := 0
	for i, u := range units {
		if u.pinned {
			include[i] = true
			pinnedTokens += u.tokens
		}
	}
	remaining := alloc - pinnedTokens
	if remaining < 0 {
		remaining = 0
		stats.OverBudget = true
	}

	// 1. 优先保留最近的若干条消息原文。
	recentMsgs := 0
	lastNonPinned := -1
	for i := len(units) - 1; i >= 0; i-- {
		if units[i].pinned {
			continue
		}
		if lastNonPinned == -1 {
			lastNonPinned = i
		}
		if recentMsgs >= a.cfg.RecentMessages {
			break
		}
		if units[i].tokens <= remaining {
			include[i] = true
			remaining -= units[i].tokens
			recentMsgs += len(units[i].messages)
		} else {
			break
		}
	}

	// 2. 最新的观察必须保留：若放不下，就进一步截断工具输出。
	if lastNonPinned >= 0 && !include[lastNonPinned] {
		u := units[lastNonPinned]
		perTool := 0
		if u.toolMsgs > 0 {
			perTool = remaining / u.toolMsgs
			if perTool < 40 {
				perTool = 40
			}
		}
		prepared, truncated := a.prepare(u.messages, perTool)
		u.prepared = prepared
		u.tokens = EstimateMessages(prepared)
		u.truncated += truncated
		include[lastNonPinned] = true
		if u.tokens > remaining {
			stats.OverBudget = true
			remaining = 0
		} else {
			remaining -= u.tokens
		}
	}

	// 3. 从新到旧继续填充剩余预算；放不下的更早单元全部进入摘要。
	for i := len(units) - 1; i >= 0; i-- {
		if include[i] || units[i].pinned {
			continue
		}
		if units[i].tokens <= remaining {
			include[i] = true
			remaining -= units[i].tokens
		} else {
			break
		}
	}

	// 4. 未纳入的消息压缩为一条摘要，插入到第一个被压缩单元的位置。
	summarized := 0
	for i, u := range units {
		if !include[i] && !u.pinned {
			summarized += len(u.messages)
		}
	}

	var summary *types.Message
	if summarized > 0 {
		allowance := remaining
		if allowance > a.cfg.SummaryMaxTokens {
			allowance = a.cfg.SummaryMaxTokens
		}
		if allowance > 0 {
			summary = &types.Message{
				// 用 assistant 角色承载摘要：语义上是“模型自己对早前对话的记录”，
				// 比在对话中插入 system 指令更兼容不同 OpenAI 兼容服务。
				Role:    types.RoleAssistant,
				Content: a.buildSummary(units, include, allowance),
			}
			stats.SummaryUsed = true
		} else {
			// 预算已经用尽，至少留一句提示，避免模型以为历史不存在。
			summary = &types.Message{
				Role:    types.RoleAssistant,
				Content: "[较早的对话已被压缩以适配上下文预算]",
			}
			stats.SummaryUsed = true
		}
	}

	out := make([]*types.Message, 0, len(history))
	summaryInserted := false
	for i, u := range units {
		if !include[i] && !u.pinned {
			if summary != nil && !summaryInserted {
				out = append(out, summary)
				summaryInserted = true
			}
			continue
		}
		out = append(out, u.prepared...)
	}
	if summary != nil && !summaryInserted {
		out = append(out, summary)
	}

	for i, u := range units {
		if include[i] {
			stats.TruncatedToolMessages += u.truncated
		}
	}
	stats.SummarizedMessages = summarized
	stats.AssembledTokens = EstimateMessages(out)
	if stats.AssembledTokens > a.cfg.MaxPromptTokens {
		stats.OverBudget = true
	}
	return out, stats
}

// buildUnits 把历史切成不可拆分的单元，并标记必须保留的消息。
func (a *Assembler) buildUnits(history []*types.Message) []*unit {
	firstUser, lastUser := -1, -1
	for i, m := range history {
		if m == nil || m.Role != types.RoleUser {
			continue
		}
		if firstUser == -1 {
			firstUser = i
		}
		lastUser = i
	}

	units := make([]*unit, 0, len(history))
	for i := 0; i < len(history); {
		m := history[i]
		if m == nil {
			i++
			continue
		}

		pinned := m.Role == types.RoleSystem || i == firstUser || i == lastUser
		if m.Role == types.RoleAssistant && len(m.ToolCalls) > 0 {
			j := i + 1
			for j < len(history) && history[j] != nil && history[j].Role == types.RoleTool {
				j++
			}
			units = append(units, &unit{messages: history[i:j], pinned: pinned})
			i = j
			continue
		}

		units = append(units, &unit{messages: []*types.Message{m}, pinned: pinned})
		i++
	}
	return units
}

// prepare 复制消息并截断过长的工具观察。
func (a *Assembler) prepare(msgs []*types.Message, maxToolTokens int) ([]*types.Message, int) {
	out := make([]*types.Message, 0, len(msgs))
	truncated := 0
	for _, m := range msgs {
		if m == nil {
			continue
		}
		cp := *m
		if m.Role == types.RoleTool && maxToolTokens > 0 && EstimateTokens(m.Content) > maxToolTokens {
			cp.Content = truncateToTokens(m.Content, maxToolTokens)
			truncated++
		}
		out = append(out, &cp)
	}
	return out, truncated
}

// buildSummary 为未纳入的单元生成紧凑摘要。
func (a *Assembler) buildSummary(units []*unit, include []bool, maxTokens int) string {
	prefix := "[历史对话摘要] 以下是较早轮次的压缩记录，仅供参考：\n"
	used := EstimateTokens(prefix)
	var lines []string

	for i, u := range units {
		if include[i] || u.pinned {
			continue
		}
		for _, line := range summarizeUnit(u.messages) {
			t := EstimateTokens(line)
			if used+t > maxTokens {
				lines = append(lines, "...")
				return prefix + strings.Join(lines, "\n")
			}
			lines = append(lines, line)
			used += t
		}
	}
	if len(lines) == 0 {
		return prefix + "..."
	}
	return prefix + strings.Join(lines, "\n")
}

// summarizeUnit 把一组消息压成若干行文本。
func summarizeUnit(msgs []*types.Message) []string {
	names := make(map[string]string)
	for _, m := range msgs {
		if m == nil {
			continue
		}
		for _, tc := range m.ToolCalls {
			if tc != nil {
				names[tc.ID] = tc.Name
			}
		}
	}

	var lines []string
	for _, m := range msgs {
		if m == nil {
			continue
		}
		switch m.Role {
		case types.RoleUser:
			lines = append(lines, "用户: "+truncateToTokens(m.Content, 40))
		case types.RoleAssistant:
			line := "助手: " + truncateToTokens(m.Content, 40)
			if len(m.ToolCalls) > 0 {
				var tools []string
				for _, tc := range m.ToolCalls {
					if tc != nil {
						tools = append(tools, tc.Name)
					}
				}
				if len(tools) > 0 {
					line += " [调用工具: " + strings.Join(tools, ", ") + "]"
				}
			}
			lines = append(lines, line)
		case types.RoleTool:
			name := names[m.ToolCallID]
			if name == "" {
				name = "unknown"
			}
			lines = append(lines, fmt.Sprintf("工具结果(%s): %s", name, truncateToTokens(m.Content, 60)))
		}
	}
	return lines
}

func countToolMessages(msgs []*types.Message) int {
	n := 0
	for _, m := range msgs {
		if m != nil && m.Role == types.RoleTool {
			n++
		}
	}
	return n
}

func cloneNonNil(history []*types.Message) []*types.Message {
	out := make([]*types.Message, 0, len(history))
	for _, m := range history {
		if m != nil {
			out = append(out, m)
		}
	}
	return out
}
