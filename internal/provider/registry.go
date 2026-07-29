package provider

import (
	"fmt"
	"sync"
)

type Capability string

const (
	CapabilityWebServer   Capability = "web_server"
	CapabilityDatabase    Capability = "database"
	CapabilityFPMRuntime  Capability = "fpm_runtime"
	CapabilityCache       Capability = "cache"
	CapabilityMessageQueue Capability = "message_queue"
)

var (
	registryMu    sync.RWMutex
	providerMap   = make(map[string]Provider)
	capabilityMap = make(map[Capability][]string)
)

func Register(name string, p Provider, capabilities ...Capability) {
	registryMu.Lock()
	defer registryMu.Unlock()

	providerMap[name] = p

	for _, cap := range capabilities {
		capabilityMap[cap] = append(capabilityMap[cap], name)
	}
}

func Get(name string) Provider {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return providerMap[name]
}

func ListByCapability(cap Capability) []Provider {
	registryMu.RLock()
	defer registryMu.RUnlock()

	names, ok := capabilityMap[cap]
	if !ok {
		return nil
	}

	var providers []Provider
	for _, name := range names {
		if p, ok := providerMap[name]; ok {
			providers = append(providers, p)
		}
	}
	return providers
}

func ListAll() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()

	names := make([]string, 0, len(providerMap))
	for name := range providerMap {
		names = append(names, name)
	}
	return names
}

func GetCapabilities(name string) []Capability {
	registryMu.RLock()
	defer registryMu.RUnlock()

	var caps []Capability
	for cap, names := range capabilityMap {
		for _, n := range names {
			if n == name {
				caps = append(caps, cap)
				break
			}
		}
	}
	return caps
}

func Supports(name string, cap Capability) bool {
	registryMu.RLock()
	defer registryMu.RUnlock()

	names, ok := capabilityMap[cap]
	if !ok {
		return false
	}

	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}

func MustRegister(name string, p Provider, capabilities ...Capability) {
	registryMu.Lock()
	defer registryMu.Unlock()

	if _, exists := providerMap[name]; exists {
		panic(fmt.Sprintf("provider %q already registered", name))
	}

	providerMap[name] = p

	for _, cap := range capabilities {
		capabilityMap[cap] = append(capabilityMap[cap], name)
	}
}