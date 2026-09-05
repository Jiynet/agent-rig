package planning

import "ai-cli-agent/pkg/types"

// NewTaskState 创建任务初始状态。
func NewTaskState(taskID string, plan *types.Plan) *types.TaskState {
	return &types.TaskState{
		TaskID:  taskID,
		Plan:    plan,
		History: []*types.Message{},
	}
}
