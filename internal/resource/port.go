package resource

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
)

const (
	PortRangeStart = 8000
	PortRangeEnd   = 9999
)

type PortPool struct {
	mu        sync.RWMutex
	allocated map[int]string
	start     int
	end       int
	dataDir   string
}

func NewPortPool(dataDir string) *PortPool {
	p := &PortPool{
		allocated: make(map[int]string),
		start:     PortRangeStart,
		end:       PortRangeEnd,
		dataDir:   dataDir,
	}
	p.Load()
	return p
}

func (p *PortPool) Allocate(owner string, preferred int) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if preferred > 0 {
		if !p.isAllocated(preferred) && p.isAvailable(preferred) {
			p.allocated[preferred] = owner
			p.Save()
			return preferred, nil
		}
	}

	for port := p.start; port <= p.end; port++ {
		if !p.isAllocated(port) && p.isAvailable(port) {
			p.allocated[port] = owner
			p.Save()
			return port, nil
		}
	}

	return 0, fmt.Errorf("端口池耗尽: 无可用端口 [%d-%d]", p.start, p.end)
}

func (p *PortPool) Release(port int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.allocated, port)
	p.Save()
}

func (p *PortPool) ReleaseByOwner(owner string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for port, o := range p.allocated {
		if o == owner {
			delete(p.allocated, port)
		}
	}
	p.Save()
}

func (p *PortPool) Load() {
	if p.dataDir == "" {
		return
	}
	path := filepath.Join(p.dataDir, "port_pool.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var allocated map[int]string
	if err := json.Unmarshal(data, &allocated); err == nil {
		p.allocated = allocated
	}
}

func (p *PortPool) Save() {
	if p.dataDir == "" {
		return
	}
	path := filepath.Join(p.dataDir, "port_pool.json")
	os.MkdirAll(p.dataDir, 0755)
	data, err := json.MarshalIndent(p.allocated, "", "  ")
	if err != nil {
		return
	}
	os.WriteFile(path, data, 0644)
}

func (p *PortPool) GetOwner(port int) (string, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	owner, ok := p.allocated[port]
	return owner, ok
}

func (p *PortPool) isAllocated(port int) bool {
	_, ok := p.allocated[port]
	return ok
}

func (p *PortPool) isAvailable(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return false
	}
	ln.Close()
	return true
}

func (p *PortPool) ListAllocated() map[int]string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	result := make(map[int]string, len(p.allocated))
	for k, v := range p.allocated {
		result[k] = v
	}
	return result
}
