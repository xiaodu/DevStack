package reactive

import (
	"sync/atomic"
	"testing"
	"time"
)

// probe: 服务崩溃后的自动重启必须受重启次数上限约束 —
// restartCount <= maxServiceRestartAttempts 时允许重启，超过后禁止（P0-1 状态机）。
func TestServiceCrashRestartGate(t *testing.T) {
	e := NewEngine(100)
	e.Start()
	defer e.Stop()
	var restarts int32
	RegisterBuiltinRules(e,
		func(serviceID string) error { atomic.AddInt32(&restarts, 1); return nil },
		nil, nil)

	// 次数在上限内：应触发重启
	for count := 0; count <= maxServiceRestartAttempts; count++ {
		e.Publish(NewEvent(EventServiceCrashed, "nginx@1.26.0", map[string]interface{}{
			"serviceID":    "nginx@1.26.0",
			"restartCount": count,
		}))
	}
	waitForCount(t, &restarts, int32(maxServiceRestartAttempts+1), "上限内事件未全部触发重启")

	// 超过上限：禁止重启
	before := atomic.LoadInt32(&restarts)
	e.Publish(NewEvent(EventServiceCrashed, "nginx@1.26.0", map[string]interface{}{
		"serviceID":    "nginx@1.26.0",
		"restartCount": maxServiceRestartAttempts + 1,
	}))
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if after := atomic.LoadInt32(&restarts); after != before {
		t.Fatalf("restartCount>%d 时不应重启, 实际重启 %d 次", maxServiceRestartAttempts, after-before)
	}
}

// probe: 端口冲突重分配规则应调用 reallocator 并把新端口写回事件数据。
func TestPortConflictReassign(t *testing.T) {
	e := NewEngine(100)
	e.Start()
	defer e.Stop()
	resCh := make(chan int, 1)
	RegisterBuiltinRules(e, nil,
		func(owner string, oldPort int) (int, error) {
			resCh <- oldPort + 1
			return oldPort + 1, nil
		}, nil)

	ev := NewEvent(EventPortConflict, "nginx@1.26.0", map[string]interface{}{
		"owner": "nginx@1.26.0",
		"port":  80,
	})
	e.Publish(ev)
	select {
	case got := <-resCh:
		if got != 81 {
			t.Fatalf("reallocator 未按规则返回新端口, got %d want 81", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("reallocator 未被调用")
	}
}

// probe: 服务健康失败事件应触发告警回调（前端依赖该事件提示用户）。
func TestHealthFailureAlertCallback(t *testing.T) {
	e := NewEngine(100)
	e.Start()
	defer e.Stop()
	var alerts int32
	RegisterBuiltinRules(e, nil, nil,
		func(eventType EventType, source string, data map[string]interface{}) {
			atomic.AddInt32(&alerts, 1)
		})

	e.Publish(NewEvent(EventServiceHealthFailed, "redis@7.4.2", map[string]interface{}{"error": "port closed"}))
	waitForCount(t, &alerts, 1, "健康失败告警回调未被调用")
}

func waitForCount(t *testing.T, counter *int32, want int32, msg string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for atomic.LoadInt32(counter) < want && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := atomic.LoadInt32(counter); got != want {
		t.Fatalf("%s: got %d want %d", msg, got, want)
	}
}
