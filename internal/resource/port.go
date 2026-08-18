package resource

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"

	"github.com/xiaodu/DevStack/internal/atomicfile"
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
	// 先在锁外收集候选端口（按 preferred → 区间顺序）
	p.mu.RLock()
	candidates := p.candidates(preferred)
	p.mu.RUnlock()

	// 锁外逐个探测，避免长时间持有写锁阻塞其它分配/释放
	for _, port := range candidates {
		if p.isAvailable(port) {
			p.mu.Lock()
			// 双检：探测期间可能已被他人分配
			if _, taken := p.allocated[port]; !taken {
				p.allocated[port] = owner
				p.Save()
				p.mu.Unlock()
				return port, nil
			}
			p.mu.Unlock()
		}
	}

	return 0, fmt.Errorf("端口池耗尽: 无可用端口 [%d-%d]", p.start, p.end)
}

// candidates 在调用方持有读锁时收集候选端口：preferred 优先，随后为区间内未分配端口。
func (p *PortPool) candidates(preferred int) []int {
	cands := make([]int, 0, p.end-p.start+2)
	if preferred >= p.start && preferred <= p.end && !p.isAllocated(preferred) {
		cands = append(cands, preferred)
	}
	for port := p.start; port <= p.end; port++ {
		if port != preferred && !p.isAllocated(port) {
			cands = append(cands, port)
		}
	}
	return cands
}

// Release 释放端口。owner 非空时校验归属：仅当端口当前确由 owner 持有时才释放，
// 防止补偿逻辑误释放其它调用方占用的端口。已释放端口视为幂等成功。
func (p *PortPool) Release(port int, owner string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	cur, ok := p.allocated[port]
	if !ok {
		return nil
	}
	if owner != "" && cur != owner {
		return fmt.Errorf("端口 %d 由 %s 占用，非 %s 所有，拒绝释放", port, cur, owner)
	}
	delete(p.allocated, port)
	p.Save()
	return nil
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
	data, err := json.MarshalIndent(p.allocated, "", "  ")
	if err != nil {
		return
	}
	atomicfile.WriteFile(path, data, 0644)
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
