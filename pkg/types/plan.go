package types

// StepStatus 表示计划中单个步骤的状态。
type StepStatus string

const (
	StepPending    StepStatus = "pending"
	StepInProgress StepStatus = "in_progress"
	StepDone       StepStatus = "done"
	StepFailed     StepStatus = "failed"
	StepSkipped    StepStatus = "skipped"
)

// Step 表示计划中的一个步骤。
type Step struct {
	ID     string
	Action *Action
	Status StepStatus
}

// Plan 是一次任务的整体执行计划。
type Plan struct {
	ID      string
	Goal    string
	Steps   []*Step
	Current int // 当前步骤下标
}
