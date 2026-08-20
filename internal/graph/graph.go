package graph

import (
	"fmt"
	"sort"
	"sync"
)

type Node struct {
	ID       string
	Kind     string
	Metadata map[string]string
}

type Edge struct {
	From string
	To   string
}

type DependencyGraph struct {
	mu    sync.RWMutex
	nodes map[string]*Node
	edges map[string][]string
	rev   map[string][]string
}

func NewDependencyGraph() *DependencyGraph {
	return &DependencyGraph{
		nodes: make(map[string]*Node),
		edges: make(map[string][]string),
		rev:   make(map[string][]string),
	}
}

func (g *DependencyGraph) AddNode(id, kind string, metadata map[string]string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.nodes[id]; ok {
		g.nodes[id].Kind = kind
		g.nodes[id].Metadata = metadata
		return
	}
	g.nodes[id] = &Node{
		ID:       id,
		Kind:     kind,
		Metadata: metadata,
	}
}

func (g *DependencyGraph) AddEdge(from, to string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if _, ok := g.nodes[from]; !ok {
		return fmt.Errorf("节点不存在: %s", from)
	}
	if _, ok := g.nodes[to]; !ok {
		return fmt.Errorf("节点不存在: %s", to)
	}

	for _, existing := range g.edges[from] {
		if existing == to {
			return nil
		}
	}

	if g.detectCycleLocked(from, to) {
		return fmt.Errorf("检测到环: %s -> %s", from, to)
	}

	g.edges[from] = append(g.edges[from], to)
	g.rev[to] = append(g.rev[to], from)
	return nil
}

func (g *DependencyGraph) detectCycleLocked(from, to string) bool {
	if from == to {
		return true
	}
	visited := make(map[string]bool)
	stack := []string{to}
	for len(stack) > 0 {
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if visited[current] {
			continue
		}
		visited[current] = true
		if current == from {
			return true
		}
		for _, next := range g.edges[current] {
			if !visited[next] {
				stack = append(stack, next)
			}
		}
	}
	return false
}

func (g *DependencyGraph) RemoveNode(id string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	delete(g.nodes, id)
	delete(g.edges, id)
	delete(g.rev, id)

	for from, targets := range g.edges {
		filtered := targets[:0]
		for _, t := range targets {
			if t != id {
				filtered = append(filtered, t)
			}
		}
		g.edges[from] = filtered
	}

	for key, sources := range g.rev {
		filtered := sources[:0]
		for _, s := range sources {
			if s != id {
				filtered = append(filtered, s)
			}
		}
		g.rev[key] = filtered
	}
}

func (g *DependencyGraph) DependenciesOf(id string) []string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	deps := make([]string, len(g.edges[id]))
	copy(deps, g.edges[id])
	sort.Strings(deps)
	return deps
}

func (g *DependencyGraph) DependentsOf(id string) []string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	deps := make([]string, len(g.rev[id]))
	copy(deps, g.rev[id])
	sort.Strings(deps)
	return deps
}

func (g *DependencyGraph) HasNode(id string) bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	_, ok := g.nodes[id]
	return ok
}

func (g *DependencyGraph) GetNode(id string) (*Node, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	n, ok := g.nodes[id]
	return n, ok
}

func (g *DependencyGraph) TopologicalSort() ([]string, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	inDegree := make(map[string]int)
	for id := range g.nodes {
		inDegree[id] = len(g.edges[id])
	}

	queue := make([]string, 0)
	for id, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, id)
		}
	}
	sort.Strings(queue)

	result := make([]string, 0, len(g.nodes))
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		result = append(result, current)

		for _, dependent := range g.rev[current] {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				idx := sort.SearchStrings(queue, dependent)
				queue = append(queue, "")
				copy(queue[idx+1:], queue[idx:])
				queue[idx] = dependent
			}
		}
	}

	if len(result) != len(g.nodes) {
		return nil, fmt.Errorf("依赖图中存在环，无法拓扑排序")
	}

	return result, nil
}

func (g *DependencyGraph) GetStopOrder() ([]string, error) {
	startOrder, err := g.TopologicalSort()
	if err != nil {
		return nil, err
	}

	stopOrder := make([]string, len(startOrder))
	for i, j := 0, len(startOrder)-1; i < j; i, j = i+1, j-1 {
		stopOrder[i], stopOrder[j] = startOrder[j], startOrder[i]
	}
	return stopOrder, nil
}

func (g *DependencyGraph) NodeCount() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return len(g.nodes)
}

func (g *DependencyGraph) EdgeCount() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	count := 0
	for _, targets := range g.edges {
		count += len(targets)
	}
	return count
}

func (g *DependencyGraph) IterateNodes(fn func(n *Node)) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	for _, n := range g.nodes {
		fn(n)
	}
}

func (g *DependencyGraph) IterateEdges(fn func(from, to string)) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	for from, targets := range g.edges {
		for _, to := range targets {
			fn(from, to)
		}
	}
}
