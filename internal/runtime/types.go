package runtime

import (
	"github.com/xiaodu/DevStack/internal/core"
	"github.com/xiaodu/DevStack/internal/process"
)

type Instance struct {
	Info    core.RuntimeInfo
	process *process.ProcessHandle
	stopped chan struct{}
}
