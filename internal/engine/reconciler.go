package engine

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/xiaodu/DevStack/internal/environment"
	"github.com/xiaodu/DevStack/internal/graph"
	"github.com/xiaodu/DevStack/internal/project"
	"github.com/xiaodu/DevStack/internal/runtime"
	"github.com/xiaodu/DevStack/internal/service"
)

type ReconcilerImpl struct {
	stateStore  *StateStore
	svcMgr      *service.Manager
	projMgr     *project.Manager
	rtMgr       *runtime.Manager
	resolver    *graph.Resolver
	snapshotMgr *SnapshotManager
	reporter    OperationReporter
	mu          sync.Mutex
}

func NewReconciler(stateStore *StateStore, svcMgr *service.Manager, projMgr *project.Manager, rtMgr *runtime.Manager) *ReconcilerImpl {
	return &ReconcilerImpl{
		stateStore:  stateStore,
		svcMgr:      svcMgr,
		projMgr:     projMgr,
		rtMgr:       rtMgr,
		resolver:    graph.NewResolver(),
		snapshotMgr: NewSnapshotManager(stateStore.dataDir),
	}
}

// SetReporter 注入编排进度上报器（由 App 实现，桥接到前端）。
func (r *ReconcilerImpl) SetReporter(reporter OperationReporter) {
	r.reporter = reporter
}

func kindFromID(id string) string {
	if idx := strings.IndexByte(id, '@'); idx > 0 {
		return id[:idx]
	}
	return id
}

func isRuntime(kind string) bool {
	return kind == "php" || kind == "node" || kind == "python"
}

var unitKindNames = map[string]string{
	"nginx": "Nginx", "apache": "Apache", "mysql": "MySQL", "postgresql": "PostgreSQL",
	"mongodb": "MongoDB", "redis": "Redis", "memcached": "Memcached",
	"php": "PHP", "node": "Node.js", "python": "Python", "composer": "Composer",
}

// unitStartRank 描述组件启动依赖序：数据库/缓存 → 运行时 → Web 服务器。
// Web 服务器（nginx）需要运行时（php）的端口生成上游配置，因此必须最后启动。
var unitStartRank = map[string]int{
	"mysql": 0, "postgresql": 0, "mongodb": 0, "redis": 0, "memcached": 0,
	"php": 1, "node": 1, "python": 1, "composer": 1,
	"nginx": 2, "apache": 2,
}

func unitRank(kind string) int {
	if r, ok := unitStartRank[kind]; ok {
		return r
	}
	return 1
}

func unitLabel(u environment.UnitSpec) string {
	name := unitKindNames[u.Kind]
	if name == "" {
		name = u.Kind
	}
	if u.Version != "" {
		return fmt.Sprintf("%s %s", name, u.Version)
	}
	return name
}

// ── 编排进度上报 ──

func (r *ReconcilerImpl) beginPlan(action, title string, stops, starts []environment.UnitSpec) {
	if r.reporter == nil {
		return
	}
	steps := make([]StepInfo, 0, len(stops)+len(starts))
	for _, u := range stops {
		steps = append(steps, StepInfo{
			ID: u.Kind + "@" + u.Version, Kind: u.Kind, Version: u.Version,
			Label: unitLabel(u), Phase: "stop", State: StepPending,
		})
	}
	for _, u := range starts {
		steps = append(steps, StepInfo{
			ID: u.Kind + "@" + u.Version, Kind: u.Kind, Version: u.Version,
			Label: unitLabel(u), Phase: "start", State: StepPending,
		})
	}
	r.reporter.OnOperationBegin(action, title, steps)
}

func (r *ReconcilerImpl) step(id string, state StepState, message string) {
	if r.reporter == nil {
		return
	}
	r.reporter.OnOperationStep(StepInfo{ID: id, State: state, Message: message})
}

func (r *ReconcilerImpl) end(action string, ok bool, message string) {
	if r.reporter == nil {
		return
	}
	r.reporter.OnOperationEnd(action, ok, message)
}

// Activate 从旧环境切换到新环境：先停旧的全部，再启新的全部。
// 分两阶段展示：停止阶段和启动阶段各自独立遮罩，避免混淆。
func (r *ReconcilerImpl) Activate(oldEnv, newEnv environment.Environment) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// ── 阶段 1：停止旧环境 ──
	if len(oldEnv.Units) > 0 {
		r.beginPlan("activate-stop", "停止旧环境", oldEnv.Units, nil)
		r.stopUnits(oldEnv.Units)
		r.end("activate-stop", true, "")
	}

	// ── 阶段 2：启动新环境 ──
	title := "启动环境"
	if newEnv.Name != "" {
		title = fmt.Sprintf("启动「%s」", newEnv.Name)
	}
	r.beginPlan("activate-start", title, nil, newEnv.Units)
	if err := r.startUnits(newEnv.Units); err != nil {
		r.end("activate-start", false, err.Error())
		return err
	}
	if err := r.stateStore.SaveActive(newEnv.ID); err != nil {
		r.end("activate-start", false, err.Error())
		return err
	}
	r.end("activate-start", true, "")
	return nil
}

// ActivateOnly 仅切换 activeEnvironmentId，不启停任何组件。
func (r *ReconcilerImpl) ActivateOnly(envID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.beginPlan("activate-only", "切换激活标记", nil, nil)
	if err := r.stateStore.SaveActive(envID); err != nil {
		r.end("activate-only", false, err.Error())
		return err
	}
	r.end("activate-only", true, "")
	return nil
}

// Run 启动指定环境的所有组件，不修改 activeEnvironmentId。
func (r *ReconcilerImpl) Run(env environment.Environment) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	title := "运行环境"
	if env.Name != "" {
		title = fmt.Sprintf("运行环境「%s」", env.Name)
	}
	r.beginPlan("run", title, nil, env.Units)
	if err := r.startUnits(env.Units); err != nil {
		r.end("run", false, err.Error())
		return err
	}
	r.end("run", true, "")
	return nil
}

// Deactivate 停止环境中的所有 Unit 并清除激活标记。
func (r *ReconcilerImpl) Deactivate(env environment.Environment) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.beginPlan("deactivate", "停止环境", env.Units, nil)
	r.stopUnits(env.Units)
	if err := r.stateStore.SaveActive(""); err != nil {
		r.end("deactivate", false, err.Error())
		return err
	}
	r.end("deactivate", true, "")
	return nil
}

// stopUnits 逆依赖序停止：Web → 运行时 → 数据库/缓存。
func (r *ReconcilerImpl) stopUnits(units []environment.UnitSpec) {
	ordered := append([]environment.UnitSpec{}, units...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return unitRank(ordered[i].Kind) > unitRank(ordered[j].Kind)
	})
	for _, u := range ordered {
		id := u.Kind + "@" + u.Version
		name := unitLabel(u)
		r.step(id, StepActive, fmt.Sprintf("正在停止 %s", name))
		if isRuntime(u.Kind) {
			_ = r.rtMgr.Stop(id)
		} else {
			_ = r.svcMgr.Stop(id)
		}
		r.step(id, StepDone, fmt.Sprintf("%s 已停止", name))
	}
}

// startUnits 按依赖序启动：数据库/缓存 → 运行时 → Web 服务器。
// 服务在启动前先落盘最新配置（nginx 的 php 上游、mysql 的 my.ini、redis 的 conf）。
func (r *ReconcilerImpl) startUnits(units []environment.UnitSpec) error {
	ordered := append([]environment.UnitSpec{}, units...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return unitRank(ordered[i].Kind) < unitRank(ordered[j].Kind)
	})
	for _, u := range ordered {
		id := u.Kind + "@" + u.Version
		name := unitLabel(u)
		if isRuntime(u.Kind) {
			r.step(id, StepActive, fmt.Sprintf("正在启动 %s", name))
			if err := r.rtMgr.Start(id); err != nil {
				r.step(id, StepError, fmt.Sprintf("%s 启动失败", name))
				return fmt.Errorf("启动 %s 失败: %w", id, err)
			}
			r.step(id, StepDone, fmt.Sprintf("%s 已运行", name))
			continue
		}
		r.step(id, StepActive, fmt.Sprintf("正在生成 %s 配置", name))
		if err := r.svcMgr.Configure(id); err != nil {
			r.step(id, StepError, fmt.Sprintf("%s 配置失败", name))
			return fmt.Errorf("配置 %s 失败: %w", id, err)
		}
		r.step(id, StepActive, fmt.Sprintf("正在启动 %s", name))
		if err := r.svcMgr.Start(id); err != nil {
			r.step(id, StepError, fmt.Sprintf("%s 启动失败", name))
			return fmt.Errorf("启动 %s 失败: %w", id, err)
		}
		r.step(id, StepDone, fmt.Sprintf("%s 已运行", name))
	}
	return nil
}

// SnapshotManager exposes the snapshot manager for save/restore operations.
func (r *ReconcilerImpl) SnapshotManager() *SnapshotManager {
	return r.snapshotMgr
}
