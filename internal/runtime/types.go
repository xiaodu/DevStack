package runtime

import (
	"time"

	"github.com/xiaodu/DevStack/internal/core"
)

type execWrapper struct {
	pid       int
	extraPids []int
	cmd       string
	started   time.Time
}

type Instance struct {
	Info    core.RuntimeInfo
	process *execWrapper
	stopped chan struct{}
}
