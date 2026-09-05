package types

// TaskState 记录任务执行的动态状态。
type TaskState struct {
	TaskID     string
	Plan       *Plan
	History    []*Message
	LastResult *ActionResult
	Iteration  int // 已执行的循环次数
	Finished   bool
}

// ActionResult 表示一次工具调用/推理步骤的结果。
type ActionResult struct {
	ActionID string
	OK       bool
	Output   string
	Error    error
	Data     any
}
