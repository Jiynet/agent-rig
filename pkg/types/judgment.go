package types

// DecisionType 表示用户对动作的判断结果。
type DecisionType string

const (
	DecisionApprove DecisionType = "approve" // 批准执行
	DecisionReject  DecisionType = "reject"  // 拒绝执行
	DecisionModify  DecisionType = "modify"  // 用户修改了入参
	DecisionUnknown DecisionType = "unknown" // 未识别输入
)

// JudgmentResult 是用户确认环节返回的结果。
type JudgmentResult struct {
	Decision      DecisionType   // 用户的决定
	Feedback      string         // 用户反馈
	ModifiedInput map[string]any // 用户修改后的工具入参（DecisionModify 时有效）
}
