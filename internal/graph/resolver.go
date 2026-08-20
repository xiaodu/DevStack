package graph

import (
	"fmt"
	"sync"
)

type ServiceDesiredState struct {
	Version string
	State   string
}

type ProjectDesiredState struct {
	Name          string
	Domain        string
	Enabled       bool
	PHPVersion    string
	MySQLVersion  string
	NginxVersion  string
	NodeVersion   string
	PythonVersion string
	Dependencies  map[string]string
}

type RuntimeDesiredState struct {
	Version string
	State   string
	Active  bool
}

type EnvironmentSnapshot struct {
	Services map[string]ServiceDesiredState
	Projects map[string]ProjectDesiredState
	Runtimes map[string]RuntimeDesiredState
}

type Resolver struct {
	mu    sync.Mutex
	graph *DependencyGraph
}

func NewResolver() *Resolver {
	return &Resolver{
		graph: NewDependencyGraph(),
	}
}

func (r *Resolver) Graph() *DependencyGraph {
	return r.graph
}

func (r *Resolver) Resolve(env EnvironmentSnapshot) (*DependencyGraph, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.graph = NewDependencyGraph()

	for id, svc := range env.Services {
		r.graph.AddNode("service:"+id, "service", map[string]string{
			"version": svc.Version,
			"state":   svc.State,
		})
	}

	for id, rt := range env.Runtimes {
		r.graph.AddNode("runtime:"+id, "runtime", map[string]string{
			"version": rt.Version,
			"state":   rt.State,
		})
	}

	for id, proj := range env.Projects {
		r.graph.AddNode("project:"+id, "project", map[string]string{
			"name":   proj.Name,
			"domain": proj.Domain,
		})
	}

	for id, proj := range env.Projects {
		if !proj.Enabled {
			continue
		}
		if proj.PHPVersion != "" {
			rtID := "runtime:php-" + proj.PHPVersion
			if r.graph.HasNode(rtID) {
				if err := r.graph.AddEdge("project:"+id, rtID); err != nil {
					return nil, fmt.Errorf("添加项目 PHP 依赖边失败: %w", err)
				}
			}
		}
		if proj.NodeVersion != "" {
			rtID := "runtime:node-" + proj.NodeVersion
			if r.graph.HasNode(rtID) {
				if err := r.graph.AddEdge("project:"+id, rtID); err != nil {
					return nil, fmt.Errorf("添加项目 Node 依赖边失败: %w", err)
				}
			}
		}
		if proj.PythonVersion != "" {
			rtID := "runtime:python-" + proj.PythonVersion
			if r.graph.HasNode(rtID) {
				if err := r.graph.AddEdge("project:"+id, rtID); err != nil {
					return nil, fmt.Errorf("添加项目 Python 依赖边失败: %w", err)
				}
			}
		}

		svcNginxID := "service:nginx"
		if proj.NginxVersion != "" {
			svcNginxID = "service:nginx-" + proj.NginxVersion
		}
		if r.graph.HasNode(svcNginxID) {
			if err := r.graph.AddEdge("project:"+id, svcNginxID); err != nil {
				return nil, fmt.Errorf("添加项目 Nginx 依赖边失败: %w", err)
			}
		} else if r.graph.HasNode("service:nginx") {
			if err := r.graph.AddEdge("project:"+id, "service:nginx"); err != nil {
				return nil, fmt.Errorf("添加项目 Nginx 依赖边失败: %w", err)
			}
		}

		if proj.MySQLVersion != "" {
			svcMySQLID := "service:mysql-" + proj.MySQLVersion
			if !r.graph.HasNode(svcMySQLID) {
				svcMySQLID = "service:mysql"
			}
			if r.graph.HasNode(svcMySQLID) {
				if err := r.graph.AddEdge("project:"+id, svcMySQLID); err != nil {
					return nil, fmt.Errorf("添加项目 MySQL 依赖边失败: %w", err)
				}
			}
		}

		for depType, depID := range proj.Dependencies {
			fullDepID := depType + ":" + depID
			if r.graph.HasNode(fullDepID) {
				if err := r.graph.AddEdge("project:"+id, fullDepID); err != nil {
					return nil, fmt.Errorf("添加项目自定义依赖边失败: %w", err)
				}
			}
		}
	}

	return r.graph, nil
}

func (r *Resolver) GetStartOrder(env EnvironmentSnapshot) ([]string, error) {
	g, err := r.Resolve(env)
	if err != nil {
		return nil, err
	}
	return g.TopologicalSort()
}

func (r *Resolver) GetStopOrder(env EnvironmentSnapshot) ([]string, error) {
	g, err := r.Resolve(env)
	if err != nil {
		return nil, err
	}
	return g.GetStopOrder()
}
