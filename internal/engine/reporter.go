package engine

// StepState 表示一次环境编排中单个步骤的状态。
type StepState string

const (
	StepPending StepState = "pending"
	StepActive  StepState = "active"
	StepDone    StepState = "done"
	StepError   StepState = "error"
)

// StepInfo 描述编排计划中的一个步骤（组件），用于前端全屏进度展示。
type StepInfo struct {
	ID      string    `json:"id"`
	Kind    string    `json:"kind"`
	Version string    `json:"version"`
	Label   string    `json:"label"`
	Phase   string    `json:"phase"` // "start" | "stop"
	State   StepState `json:"state"`
	Message string    `json:"message"`
}

// OperationReporter 接收环境编排的进度事件。App 实现它并把事件桥接到前端。
type OperationReporter interface {
	OnOperationBegin(action, title string, steps []StepInfo)
	OnOperationStep(step StepInfo)
	OnOperationEnd(action string, ok bool, message string)
}
