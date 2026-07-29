package engine

import (
	"context"
	"fmt"

	"github.com/xiaodu/DevStack/internal/core"
	"github.com/xiaodu/DevStack/internal/graph"
	"github.com/xiaodu/DevStack/internal/project"
	"github.com/xiaodu/DevStack/internal/runtime"
	"github.com/xiaodu/DevStack/internal/service"
	"github.com/xiaodu/DevStack/internal/tx"
)

type ReconcilerImpl struct {
	stateStore    *StateStore
	svcMgr        *service.Manager
	projMgr       *project.Manager
	rtMgr         *runtime.Manager
	resolver      *graph.Resolver
	snapshotMgr   *SnapshotManager
}

func NewReconciler(stateStore *StateStore, svcMgr *service.Manager, projMgr *project.Manager, rtMgr *runtime.Manager) *ReconcilerImpl {
	return &ReconcilerImpl{
		stateStore:   stateStore,
		svcMgr:       svcMgr,
		projMgr:      projMgr,
		rtMgr:        rtMgr,
		resolver:     graph.NewResolver(),
		snapshotMgr:  NewSnapshotManager(stateStore.dataDir),
	}
}

func (r *ReconcilerImpl) Apply(env EnvironmentState) error {
	current, err := r.stateStore.Load()
	if err != nil {
		return fmt.Errorf("加载当前状态失败: %w", err)
	}

	changes := computeDiff(current, env)
	if len(changes) == 0 {
		return nil
	}

	sortedChanges := r.sortChangesByDependency(changes, env)

	txn := tx.NewTransaction("apply-environment")

	snapStep, _ := r.snapshotMgr.CreateSnapshotStep(current)
	txn.Step("snapshot-before-apply", snapStep, func(ctx context.Context) error {
		return r.snapshotMgr.RestoreLastSnapshot()
	})

	for _, change := range sortedChanges {
		compensate := r.createCompensator(change, current)
		txn.Step(
			fmt.Sprintf("%s-%s-%s", change.EntityType, change.EntityID, change.Type),
			func(ctx context.Context) error {
				return r.executeChange(change, env)
			},
			compensate,
		)
	}

	txn.Step("save-state", func(ctx context.Context) error {
		return r.stateStore.Save(env)
	}, func(ctx context.Context) error {
		return r.stateStore.Save(current)
	})

	if err := txn.Commit(); err != nil {
		return fmt.Errorf("应用环境事务失败: %w", err)
	}

	return nil
}

func (r *ReconcilerImpl) createCompensator(change Change, current EnvironmentState) func(ctx context.Context) error {
	switch change.EntityType {
	case EntityService:
		return r.createServiceCompensator(change, current)
	case EntityProject:
		return r.createProjectCompensator(change, current)
	case EntityRuntime:
		return r.createRuntimeCompensator(change, current)
	default:
		return tx.NewCompensator().Noop()
	}
}

func (r *ReconcilerImpl) createServiceCompensator(change Change, current EnvironmentState) func(ctx context.Context) error {
	switch change.Type {
	case ChangeStart:
		return func(ctx context.Context) error {
			return r.svcMgr.Stop(change.EntityID)
		}
	case ChangeStop:
		return func(ctx context.Context) error {
			return r.svcMgr.Start(change.EntityID)
		}
	case ChangeCreate:
		return func(ctx context.Context) error {
			return r.svcMgr.Stop(change.EntityID)
		}
	case ChangeDelete:
		return func(ctx context.Context) error {
			if _, ok := current.Services[change.EntityID]; ok {
				return r.svcMgr.Start(change.EntityID)
			}
			return nil
		}
	default:
		return tx.NewCompensator().Noop()
	}
}

func (r *ReconcilerImpl) createProjectCompensator(change Change, current EnvironmentState) func(ctx context.Context) error {
	switch change.Type {
	case ChangeStart:
		return func(ctx context.Context) error {
			return r.projMgr.Disable(change.EntityID)
		}
	case ChangeStop:
		return func(ctx context.Context) error {
			return r.projMgr.Enable(change.EntityID)
		}
	case ChangeCreate:
		return func(ctx context.Context) error {
			return r.projMgr.Delete(change.EntityID)
		}
	case ChangeDelete:
		return func(ctx context.Context) error {
			if proj, ok := current.Projects[change.EntityID]; ok {
				p := core.Project{
					ID:           change.EntityID,
					Name:         proj.Name,
					Type:         proj.Type,
					Root:         proj.Root,
					Domain:       proj.Domain,
					Port:         proj.Port,
					HTTPSPort:    proj.HTTPSPort,
					SSL:          proj.SSL,
					PHPVersion:   proj.PHPVersion,
					ProxyPass:    proj.ProxyPass,
					TargetPort:   proj.TargetPort,
					Rewrite:      proj.Rewrite,
					Notes:        proj.Notes,
					Enabled:      proj.Enabled,
					Dependencies: proj.Dependencies,
				}
				r.projMgr.Create(p)
				return nil
			}
			return nil
		}
	default:
		return tx.NewCompensator().Noop()
	}
}

func (r *ReconcilerImpl) createRuntimeCompensator(change Change, current EnvironmentState) func(ctx context.Context) error {
	switch change.Type {
	case ChangeStart:
		return func(ctx context.Context) error {
			return r.rtMgr.Stop(change.EntityID)
		}
	case ChangeStop:
		return func(ctx context.Context) error {
			return r.rtMgr.Start(change.EntityID)
		}
	case ChangeCreate:
		return func(ctx context.Context) error {
			return r.rtMgr.Stop(change.EntityID)
		}
	case ChangeDelete:
		return func(ctx context.Context) error {
			if rt, ok := current.Runtimes[change.EntityID]; ok {
				if rt.State == core.StatusRunning {
					return r.rtMgr.Start(change.EntityID)
				}
			}
			return nil
		}
	default:
		return tx.NewCompensator().Noop()
	}
}

func (r *ReconcilerImpl) Diff(env EnvironmentState) ([]Change, error) {
	return r.stateStore.Diff(env)
}

func (r *ReconcilerImpl) GetCurrentState() (EnvironmentState, error) {
	storedEnv, _ := r.stateStore.Load()

	env := EnvironmentState{
		Services: make(map[string]ServiceDesiredState),
		Projects: make(map[string]ProjectDesiredState),
		Runtimes: make(map[string]RuntimeDesiredState),
	}

	for _, svc := range r.svcMgr.List() {
		if !svc.Installed {
			continue
		}
		state := svc.Status
		if storedSvc, ok := storedEnv.Services[svc.ID]; ok {
			if storedSvc.State != "" {
				state = storedSvc.State
			}
		}
		env.Services[svc.ID] = ServiceDesiredState{
			Version: svc.Version,
			State:   state,
		}
	}

	for _, proj := range r.projMgr.List() {
		enabled := proj.Enabled
		if storedProj, ok := storedEnv.Projects[proj.ID]; ok {
			enabled = storedProj.Enabled
		}
		env.Projects[proj.ID] = ProjectDesiredState{
			Name:          proj.Name,
			Type:          proj.Type,
			Root:          proj.Root,
			Domain:        proj.Domain,
			Port:          proj.Port,
			HTTPSPort:     proj.HTTPSPort,
			SSL:           proj.SSL,
			EnvironmentID: proj.EnvironmentID,
			PHPVersion:    proj.PHPVersion,
			MySQLVersion:  proj.MySQLVersion,
			NginxVersion:  proj.NginxVersion,
			NodeVersion:   proj.NodeVersion,
			PythonVersion: proj.PythonVersion,
			ProxyPass:     proj.ProxyPass,
			TargetPort:    proj.TargetPort,
			Rewrite:       proj.Rewrite,
			Notes:         proj.Notes,
			Enabled:       enabled,
			Dependencies:  proj.Dependencies,
		}
	}

	for _, rt := range r.rtMgr.List() {
		if !rt.Installed {
			continue
		}
		state := rt.Status
		active := rt.Active
		if storedRt, ok := storedEnv.Runtimes[rt.ID]; ok {
			if storedRt.State != "" {
				state = storedRt.State
			}
			active = storedRt.Active
		}
		env.Runtimes[rt.ID] = RuntimeDesiredState{
			Version: rt.Version,
			State:   state,
			Active:  active,
		}
	}

	return env, nil
}

// BuildGraph 基于当前已持久化的期望状态构建依赖图。
// 用于前端可视化、依赖分析、操作前的顺序推导。
func (r *ReconcilerImpl) BuildGraph() (*graph.DependencyGraph, error) {
	env, err := r.stateStore.Load()
	if err != nil {
		return nil, fmt.Errorf("加载期望状态失败: %w", err)
	}
	snap := r.toSnapshot(env)
	return r.resolver.Resolve(snap)
}

func (r *ReconcilerImpl) GetDependents(entityType, entityID string) ([]string, error) {
	g, err := r.BuildGraph()
	if err != nil {
		return nil, err
	}
	return g.DependentsOf(entityType + ":" + entityID), nil
}

func (r *ReconcilerImpl) GetDependencies(entityType, entityID string) ([]string, error) {
	g, err := r.BuildGraph()
	if err != nil {
		return nil, err
	}
	return g.DependenciesOf(entityType + ":" + entityID), nil
}

func (r *ReconcilerImpl) GetStartOrder() ([]string, error) {
	env, err := r.stateStore.Load()
	if err != nil {
		return nil, fmt.Errorf("加载期望状态失败: %w", err)
	}
	snap := r.toSnapshot(env)
	return r.resolver.GetStartOrder(snap)
}

func (r *ReconcilerImpl) GetStopOrder() ([]string, error) {
	env, err := r.stateStore.Load()
	if err != nil {
		return nil, fmt.Errorf("加载期望状态失败: %w", err)
	}
	snap := r.toSnapshot(env)
	return r.resolver.GetStopOrder(snap)
}

func (r *ReconcilerImpl) SnapshotManager() *SnapshotManager {
	return r.snapshotMgr
}

func (r *ReconcilerImpl) sortChangesByDependency(changes []Change, env EnvironmentState) []Change {
	var stops []Change
	var starts []Change
	var others []Change

	for _, change := range changes {
		switch change.Type {
		case ChangeStop:
			stops = append(stops, change)
		case ChangeStart:
			starts = append(starts, change)
		default:
			others = append(others, change)
		}
	}

	snapshot := r.toSnapshot(env)
	startOrder, err := r.resolver.GetStartOrder(snapshot)
	if err != nil {
		result := append(stops, others...)
		result = append(result, starts...)
		return result
	}

	priority := make(map[string]int)
	for i, id := range startOrder {
		priority[id] = i
	}

	sortByPriority := func(list []Change) {
		for i := 1; i < len(list); i++ {
			for j := i; j > 0; j-- {
				curPrio := priority[changeNodeID(list[j])]
				prevPrio := priority[changeNodeID(list[j-1])]
				if curPrio < prevPrio {
					list[j], list[j-1] = list[j-1], list[j]
				} else {
					break
				}
			}
		}
	}

	sortByPriority(stops)
	sortByPriority(starts)

	result := append(stops, others...)
	result = append(result, starts...)
	return result
}

func (r *ReconcilerImpl) toSnapshot(env EnvironmentState) graph.EnvironmentSnapshot {
	snap := graph.EnvironmentSnapshot{
		Services: make(map[string]graph.ServiceDesiredState),
		Projects: make(map[string]graph.ProjectDesiredState),
		Runtimes: make(map[string]graph.RuntimeDesiredState),
	}

	for id, svc := range env.Services {
		snap.Services[id] = graph.ServiceDesiredState{
			Version: svc.Version,
			State:   string(svc.State),
		}
	}

	for id, rt := range env.Runtimes {
		snap.Runtimes[id] = graph.RuntimeDesiredState{
			Version: rt.Version,
			State:   string(rt.State),
			Active:  rt.Active,
		}
	}

	for id, proj := range env.Projects {
		snap.Projects[id] = graph.ProjectDesiredState{
			Name:          proj.Name,
			Domain:        proj.Domain,
			Enabled:       proj.Enabled,
			PHPVersion:    proj.PHPVersion,
			MySQLVersion:  proj.MySQLVersion,
			NginxVersion:  proj.NginxVersion,
			NodeVersion:   proj.NodeVersion,
			PythonVersion: proj.PythonVersion,
			Dependencies:  proj.Dependencies,
		}
	}

	return snap
}

func changeNodeID(c Change) string {
	return c.EntityType + ":" + c.EntityID
}

func (r *ReconcilerImpl) executeChange(change Change, env EnvironmentState) error {
	switch change.EntityType {
	case "service":
		return r.executeServiceChange(change, env)
	case "project":
		return r.executeProjectChange(change, env)
	case "runtime":
		return r.executeRuntimeChange(change, env)
	default:
		return fmt.Errorf("未知实体类型: %s", change.EntityType)
	}
}

func (r *ReconcilerImpl) executeServiceChange(change Change, env EnvironmentState) error {
	switch change.Type {
	case ChangeStart:
		return r.svcMgr.Start(change.EntityID)
	case ChangeStop:
		return r.svcMgr.Stop(change.EntityID)
	case ChangeConfigure:
		if desired, ok := env.Services[change.EntityID]; ok {
			return r.svcMgr.SetVersion(change.EntityID, desired.Version)
		}
		return fmt.Errorf("未找到服务 %s 的期望状态", change.EntityID)
	case ChangeCreate:
		if desired, ok := env.Services[change.EntityID]; ok {
			if err := r.svcMgr.SetVersion(change.EntityID, desired.Version); err != nil {
				return err
			}
			if desired.State == core.StatusRunning {
				return r.svcMgr.Start(change.EntityID)
			}
		}
		return fmt.Errorf("未找到服务 %s 的期望状态", change.EntityID)
	case ChangeDelete:
		return r.svcMgr.Uninstall(change.EntityID)
	default:
		return fmt.Errorf("未知服务变更类型: %s", change.Type)
	}
}

func (r *ReconcilerImpl) executeProjectChange(change Change, env EnvironmentState) error {
	switch change.Type {
	case ChangeStart:
		return r.projMgr.Enable(change.EntityID)
	case ChangeStop:
		return r.projMgr.Disable(change.EntityID)
	case ChangeCreate:
		if desired, ok := env.Projects[change.EntityID]; ok {
			p := core.Project{
				ID:           change.EntityID,
				Name:         desired.Name,
				Type:         desired.Type,
				Root:         desired.Root,
				Domain:       desired.Domain,
				Port:         desired.Port,
				HTTPSPort:    desired.HTTPSPort,
				SSL:          desired.SSL,
				PHPVersion:   desired.PHPVersion,
				ProxyPass:    desired.ProxyPass,
				TargetPort:   desired.TargetPort,
				Rewrite:      desired.Rewrite,
				Notes:        desired.Notes,
				Enabled:      desired.Enabled,
				Dependencies: desired.Dependencies,
			}
			_, err := r.projMgr.Create(p)
			return err
		}
		return fmt.Errorf("未找到项目 %s 的期望状态", change.EntityID)
	case ChangeDelete:
		return r.projMgr.Delete(change.EntityID)
	case ChangeUpdate:
		if desired, ok := env.Projects[change.EntityID]; ok {
			p := core.Project{
				ID:           change.EntityID,
				Name:         desired.Name,
				Type:         desired.Type,
				Root:         desired.Root,
				Domain:       desired.Domain,
				Port:         desired.Port,
				HTTPSPort:    desired.HTTPSPort,
				SSL:          desired.SSL,
				PHPVersion:   desired.PHPVersion,
				ProxyPass:    desired.ProxyPass,
				TargetPort:   desired.TargetPort,
				Rewrite:      desired.Rewrite,
				Notes:        desired.Notes,
				Enabled:      desired.Enabled,
				Dependencies: desired.Dependencies,
			}
			return r.projMgr.Update(p)
		}
		return fmt.Errorf("未找到项目 %s 的期望状态", change.EntityID)
	default:
		return fmt.Errorf("未知项目变更类型: %s", change.Type)
	}
}

func (r *ReconcilerImpl) executeRuntimeChange(change Change, env EnvironmentState) error {
	switch change.Type {
	case ChangeStart:
		return r.rtMgr.Start(change.EntityID)
	case ChangeStop:
		return r.rtMgr.Stop(change.EntityID)
	case ChangeConfigure:
		if _, ok := env.Runtimes[change.EntityID]; ok {
			return r.rtMgr.Start(change.EntityID)
		}
		return fmt.Errorf("未找到运行时 %s 的期望状态", change.EntityID)
	case ChangeCreate:
		if desired, ok := env.Runtimes[change.EntityID]; ok {
			if desired.State == core.StatusRunning {
				return r.rtMgr.Start(change.EntityID)
			}
		}
		return fmt.Errorf("未找到运行时 %s 的期望状态", change.EntityID)
	case ChangeDelete:
		return r.rtMgr.Stop(change.EntityID)
	default:
		return fmt.Errorf("未知运行时变更类型: %s", change.Type)
	}
}
