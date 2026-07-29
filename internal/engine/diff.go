package engine

import (
	"reflect"

	"github.com/xiaodu/DevStack/internal/core"
)

func (ss *StateStore) Diff(desired EnvironmentState) ([]Change, error) {
	current, err := ss.Load()
	if err != nil {
		return nil, err
	}

	return computeDiff(current, desired), nil
}

func computeDiff(current, desired EnvironmentState) []Change {
	var changes []Change

	changes = append(changes, computeServiceChanges(current.Services, desired.Services)...)
	changes = append(changes, computeProjectChanges(current.Projects, desired.Projects)...)
	changes = append(changes, computeRuntimeChanges(current.Runtimes, desired.Runtimes)...)

	return changes
}

func computeServiceChanges(current, desired map[string]ServiceDesiredState) []Change {
	var changes []Change

	for id, desiredState := range desired {
		if currentState, ok := current[id]; ok {
			if currentState.State != desiredState.State {
				changes = append(changes, Change{
					Type:      changeTypeForState(currentState.State, desiredState.State),
					EntityID:  id,
					EntityType: "service",
					From:      currentState.State,
					To:        desiredState.State,
				})
			}
			if currentState.Version != desiredState.Version {
				changes = append(changes, Change{
					Type:      ChangeConfigure,
					EntityID:  id,
					EntityType: "service",
					From:      map[string]interface{}{"version": currentState.Version},
					To:        map[string]interface{}{"version": desiredState.Version},
				})
			}
		} else {
			changes = append(changes, Change{
				Type:      ChangeCreate,
				EntityID:  id,
				EntityType: "service",
				To:        desiredState,
			})
		}
	}

	for id, currentState := range current {
		if _, ok := desired[id]; !ok {
			changes = append(changes, Change{
				Type:      ChangeDelete,
				EntityID:  id,
				EntityType: "service",
				From:      currentState,
			})
		}
	}

	return changes
}

func computeProjectChanges(current, desired map[string]ProjectDesiredState) []Change {
	var changes []Change

	for id, desiredState := range desired {
		if currentState, ok := current[id]; ok {
			if currentState.Enabled != desiredState.Enabled {
				changes = append(changes, Change{
					Type:      changeTypeForProjectEnable(currentState.Enabled, desiredState.Enabled),
					EntityID:  id,
					EntityType: "project",
					From:      currentState.Enabled,
					To:        desiredState.Enabled,
				})
			}
			if !reflect.DeepEqual(currentState, desiredState) {
				changes = append(changes, Change{
					Type:      ChangeUpdate,
					EntityID:  id,
					EntityType: "project",
					From:      currentState,
					To:        desiredState,
				})
			}
		} else {
			changes = append(changes, Change{
				Type:      ChangeCreate,
				EntityID:  id,
				EntityType: "project",
				To:        desiredState,
			})
		}
	}

	for id, currentState := range current {
		if _, ok := desired[id]; !ok {
			changes = append(changes, Change{
				Type:      ChangeDelete,
				EntityID:  id,
				EntityType: "project",
				From:      currentState,
			})
		}
	}

	return changes
}

func computeRuntimeChanges(current, desired map[string]RuntimeDesiredState) []Change {
	var changes []Change

	for id, desiredState := range desired {
		if currentState, ok := current[id]; ok {
			if currentState.State != desiredState.State {
				changes = append(changes, Change{
					Type:      changeTypeForState(currentState.State, desiredState.State),
					EntityID:  id,
					EntityType: "runtime",
					From:      currentState.State,
					To:        desiredState.State,
				})
			}
			if currentState.Version != desiredState.Version {
				changes = append(changes, Change{
					Type:      ChangeConfigure,
					EntityID:  id,
					EntityType: "runtime",
					From:      map[string]interface{}{"version": currentState.Version},
					To:        map[string]interface{}{"version": desiredState.Version},
				})
			}
		} else {
			changes = append(changes, Change{
				Type:      ChangeCreate,
				EntityID:  id,
				EntityType: "runtime",
				To:        desiredState,
			})
		}
	}

	for id, currentState := range current {
		if _, ok := desired[id]; !ok {
			changes = append(changes, Change{
				Type:      ChangeDelete,
				EntityID:  id,
				EntityType: "runtime",
				From:      currentState,
			})
		}
	}

	return changes
}

func changeTypeForState(from, to core.ServiceStatus) ChangeType {
	if from == core.StatusStopped && to == core.StatusRunning {
		return ChangeStart
	}
	if from == core.StatusRunning && to == core.StatusStopped {
		return ChangeStop
	}
	return ChangeConfigure
}

func changeTypeForProjectEnable(from, to bool) ChangeType {
	if !from && to {
		return ChangeStart
	}
	if from && !to {
		return ChangeStop
	}
	return ChangeUpdate
}
