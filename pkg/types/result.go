package types

// AgentResult 是 Agent 最终交付给用户的结果。
type AgentResult struct {
	Summary string    // 结果摘要
	Detail  string    // 详细输出
	Actions []*Action // 实际执行过的动作
	Err     error     // 整体错误（如有）
}
