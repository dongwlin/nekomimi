package mcp

import (
	"sort"
	"strings"
	"sync"
)

type memoryRegistry struct {
	mu      sync.RWMutex
	servers map[string]ServerConfig
	order   []string
}

var _ Registry = (*memoryRegistry)(nil)

// NewRegistry creates an in-memory MCP server registry.
func NewRegistry() Registry {
	return &memoryRegistry{
		servers: make(map[string]ServerConfig),
	}
}

func (r *memoryRegistry) SetServers(servers []ServerConfig) {
	nextServers := make(map[string]ServerConfig, len(servers))
	nextOrder := make([]string, 0, len(servers))

	for _, server := range servers {
		normalized, ok := normalizeServerConfig(server)
		if !ok {
			continue
		}
		key := normalizeName(normalized.Name)
		if _, exists := nextServers[key]; !exists {
			nextOrder = append(nextOrder, key)
		}
		nextServers[key] = normalized
	}

	sort.SliceStable(nextOrder, func(i, j int) bool {
		return nextOrder[i] < nextOrder[j]
	})

	r.mu.Lock()
	r.servers = nextServers
	r.order = nextOrder
	r.mu.Unlock()
}

func (r *memoryRegistry) ListServers() []ServerConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.order) == 0 {
		return nil
	}

	result := make([]ServerConfig, 0, len(r.order))
	for _, key := range r.order {
		server, ok := r.servers[key]
		if !ok {
			continue
		}
		result = append(result, cloneServerConfig(server))
	}
	return result
}

func (r *memoryRegistry) GetServer(name string) (ServerConfig, bool) {
	key := normalizeName(name)
	if key == "" {
		return ServerConfig{}, false
	}

	r.mu.RLock()
	server, ok := r.servers[key]
	r.mu.RUnlock()
	if !ok {
		return ServerConfig{}, false
	}
	return cloneServerConfig(server), true
}

func normalizeServerConfig(server ServerConfig) (ServerConfig, bool) {
	name := strings.TrimSpace(server.Name)
	if name == "" {
		return ServerConfig{}, false
	}

	normalized := ServerConfig{
		Name:            name,
		Transport:       strings.TrimSpace(server.Transport),
		URL:             strings.TrimSpace(server.URL),
		Command:         strings.TrimSpace(server.Command),
		Args:            cloneStringSlice(server.Args),
		Headers:         cloneStringMap(server.Headers),
		AllowTools:      normalizeAllowTools(server.AllowTools),
		Timeout:         server.Timeout,
		MaxPayloadBytes: server.MaxPayloadBytes,
	}
	if normalized.Timeout < 0 {
		normalized.Timeout = 0
	}
	if normalized.MaxPayloadBytes < 0 {
		normalized.MaxPayloadBytes = 0
	}
	return normalized, true
}

func normalizeAllowTools(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		name := strings.TrimSpace(value)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		result = append(result, name)
	}
	if len(result) == 0 {
		return nil
	}
	sort.Strings(result)
	return result
}

func normalizeName(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func cloneServerConfig(server ServerConfig) ServerConfig {
	return ServerConfig{
		Name:            server.Name,
		Transport:       server.Transport,
		URL:             server.URL,
		Command:         server.Command,
		Args:            cloneStringSlice(server.Args),
		Headers:         cloneStringMap(server.Headers),
		AllowTools:      cloneStringSlice(server.AllowTools),
		Timeout:         server.Timeout,
		MaxPayloadBytes: server.MaxPayloadBytes,
	}
}

func cloneStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	return append([]string(nil), values...)
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	result := make(map[string]string, len(values))
	for k, v := range values {
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		result[key] = strings.TrimSpace(v)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}
