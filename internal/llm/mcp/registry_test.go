package mcp

import (
	"testing"
	"time"
)

func TestMemoryRegistry_SetListGet(t *testing.T) {
	registry := NewRegistry()
	registry.SetServers([]ServerConfig{
		{
			Name:            "  ",
			MaxPayloadBytes: 1,
		},
		{
			Name:            "Alpha",
			AllowTools:      []string{"echo", "", "echo", "sum"},
			Timeout:         -1 * time.Second,
			MaxPayloadBytes: -1,
			Headers: map[string]string{
				"":     "x",
				"Auth": " token ",
			},
		},
		{
			Name:       "beta",
			AllowTools: []string{"ping"},
		},
		{
			Name:       "ALPHA",
			AllowTools: []string{"new"},
			Headers: map[string]string{
				"Auth": "override",
			},
		},
	})

	servers := registry.ListServers()
	if len(servers) != 2 {
		t.Fatalf("server count mismatch: got %d, want %d", len(servers), 2)
	}
	if servers[0].Name != "ALPHA" {
		t.Fatalf("first server mismatch: got %q", servers[0].Name)
	}
	if servers[1].Name != "beta" {
		t.Fatalf("second server mismatch: got %q", servers[1].Name)
	}

	alpha, ok := registry.GetServer("alpha")
	if !ok {
		t.Fatalf("alpha server should exist")
	}
	if len(alpha.AllowTools) != 1 || alpha.AllowTools[0] != "new" {
		t.Fatalf("alpha allow tools mismatch: %#v", alpha.AllowTools)
	}

	alpha.AllowTools[0] = "mutated"
	alpha.Headers["X-Test"] = "y"

	alphaReloaded, ok := registry.GetServer("ALPHA")
	if !ok {
		t.Fatalf("alpha server should still exist")
	}
	if alphaReloaded.AllowTools[0] != "new" {
		t.Fatalf("registry should return defensive copies")
	}
	if _, exists := alphaReloaded.Headers["X-Test"]; exists {
		t.Fatalf("registry headers should not be externally mutable")
	}
}
