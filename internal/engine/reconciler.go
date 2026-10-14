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

// Activate 从旧环境切换到新环境。
//
// 交集优化：新旧环境同 kind@version 的单元直接保留运行，不做无谓的停-启
// （这是环境切换耗时和体感卡顿的主要来源）；启动新环境失败时，
// 回滚本次新启单元并尽力恢复旧环境独有的单元，避免留下"全部停机"的中间态。
func (r *ReconcilerImpl) Activate(oldEnv, newEnv environment.Environment) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// 新环境单元集合（kind@version）
	newKeys := make(map[string]bool, len(newEnv.Units))
	for _, u := range newEnv.Units {
		newKeys[u.Kind+"@"+u.Version] = true
	}

	// ── 阶段 1：停止旧环境（跳过与新环境交集的单元，它们继续运行）──
	var stoppedOld []environment.UnitSpec // 阶段 1 实际停掉的旧环境单元，失败时用于恢复
	if len(oldEnv.Units) > 0 {
		stopList := make([]environment.UnitSpec, 0, len(oldEnv.Units))
		for _, u := range oldEnv.Units {
			if newKeys[u.Kind+"@"+u.Version] {
				continue // 交集单元保留运行
			}
			stopList = append(stopList, u)
		}
		if len(stopList) > 0 {
			r.beginPlan("activate-stop", "停止旧环境", stopList, nil)
			if stopErr := r.stopUnits(stopList); stopErr != nil {
				r.end("activate-stop", false, stopErr.Error())
				return stopErr
			}
			r.end("activate-stop", true, "")
			stoppedOld = stopList
		}
	}

	// ── 阶段 2：启动新环境 ──
	// 交集单元已在运行，svcMgr/rtMgr 的 Start 幂等（运行中直接返回 nil），无需特判
	title := "启动环境"
	if newEnv.Name != "" {
		title = fmt.Sprintf("启动「%s」", newEnv.Name)
	}
	r.beginPlan("activate-start", title, nil, newEnv.Units)
	startErr := r.startUnits(newEnv.Units)
	if startErr != nil {
		r.end("activate-start", false, startErr.Error())
		// 启动失败：恢复阶段 1 停掉的旧环境单元，让用户至少回到切换前的可用状态
		if len(stoppedOld) > 0 {
			if restoreErr := r.startUnits(stoppedOld); restoreErr != nil {
				return fmt.Errorf("%v; 恢复旧环境「%s」也失败: %w", startErr, oldEnv.Name, restoreErr)
			}
		}
		return startErr
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
	stopErr := r.stopUnits(env.Units)
	// 无论停止是否完全成功，激活标记都应清空，否则 UI 会认为环境仍在运行
	if err := r.stateStore.SaveActive(""); err != nil {
		r.end("deactivate", false, err.Error())
		return err
	}
	if stopErr != nil {
		r.end("deactivate", false, stopErr.Error())
		return stopErr
	}
	r.end("deactivate", true, "")
	return nil
}

// stopUnits 逆依赖序停止：Web → 运行时 → 数据库/缓存。
// 收集并返回所有停止失败的错误，供调用方上报。
func (r *ReconcilerImpl) stopUnits(units []environment.UnitSpec) error {
	ordered := append([]environment.UnitSpec{}, units...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return unitRank(ordered[i].Kind) > unitRank(ordered[j].Kind)
	})
	var errs []string
	for _, u := range ordered {
		id := u.Kind + "@" + u.Version
		name := unitLabel(u)
		r.step(id, StepActive, fmt.Sprintf("正在停止 %s", name))
		var err error
		if isRuntime(u.Kind) {
			err = r.rtMgr.Stop(id)
		} else {
			err = r.svcMgr.Stop(id)
		}
		if err != nil {
			r.step(id, StepError, fmt.Sprintf("%s 停止失败: %v", name, err))
			errs = append(errs, fmt.Sprintf("%s: %v", id, err))
		} else {
			r.step(id, StepDone, fmt.Sprintf("%s 已停止", name))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("停止失败: %s", strings.Join(errs, "; "))
	}
	return nil
}

// startUnits 按依赖序启动：数据库/缓存 → 运行时 → Web 服务器。
// 服务在启动前先落盘最新配置（nginx 的 php 上游、mysql 的 my.ini、redis 的 conf）。
// 任一步失败时，按逆序停掉本次已启动的单元，避免半启状态残留。
func (r *ReconcilerImpl) startUnits(units []environment.UnitSpec) error {
	ordered := append([]environment.UnitSpec{}, units...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return unitRank(ordered[i].Kind) < unitRank(ordered[j].Kind)
	})
	var started []environment.UnitSpec
	rollback := func(cause error) error {
		if len(started) == 0 {
			return cause
		}
		reverse := make([]environment.UnitSpec, 0, len(started))
		for i := len(started) - 1; i >= 0; i-- {
			reverse = append(reverse, started[i])
		}
		if stopErr := r.stopUnits(reverse); stopErr != nil {
			return fmt.Errorf("%v; 回滚已启动单元失败: %w", cause, stopErr)
		}
		return cause
	}
	for _, u := range ordered {
		id := u.Kind + "@" + u.Version
		name := unitLabel(u)
		if isRuntime(u.Kind) {
			r.step(id, StepActive, fmt.Sprintf("正在启动 %s", name))
			if err := r.rtMgr.Start(id); err != nil {
				r.step(id, StepError, fmt.Sprintf("%s 启动失败", name))
				return rollback(fmt.Errorf("启动 %s 失败: %w", id, err))
			}
			started = append(started, u)
			r.step(id, StepDone, fmt.Sprintf("%s 已运行", name))
			continue
		}
		r.step(id, StepActive, fmt.Sprintf("正在生成 %s 配置", name))
		if err := r.svcMgr.Configure(id); err != nil {
			r.step(id, StepError, fmt.Sprintf("%s 配置失败", name))
			return rollback(fmt.Errorf("配置 %s 失败: %w", id, err))
		}
		r.step(id, StepActive, fmt.Sprintf("正在启动 %s", name))
		if err := r.svcMgr.Start(id); err != nil {
			r.step(id, StepError, fmt.Sprintf("%s 启动失败", name))
			return rollback(fmt.Errorf("启动 %s 失败: %w", id, err))
		}
		started = append(started, u)
		r.step(id, StepDone, fmt.Sprintf("%s 已运行", name))
	}
	return nil
}

// SnapshotManager exposes the snapshot manager for save/restore operations.
func (r *ReconcilerImpl) SnapshotManager() *SnapshotManager {
	return r.snapshotMgr
}
