package config

import (
	"testing"
)

func TestGetDefault(t *testing.T) {
	cfg := GetDefault("/tmp/test-workspace")

	if cfg.WorkspaceDir != "/tmp/test-workspace" {
		t.Errorf("expected WorkspaceDir '/tmp/test-workspace', got '%s'", cfg.WorkspaceDir)
	}

	if cfg.L0Dir != "/tmp/test-workspace/_yumem/l0" {
		t.Errorf("expected L0Dir to contain '_yumem/l0', got '%s'", cfg.L0Dir)
	}

	if cfg.L1Dir != "/tmp/test-workspace/_yumem/l1" {
		t.Errorf("expected L1Dir to contain '_yumem/l1', got '%s'", cfg.L1Dir)
	}

	if cfg.L2Dir != "/tmp/test-workspace/_yumem/l2" {
		t.Errorf("expected L2Dir to contain '_yumem/l2', got '%s'", cfg.L2Dir)
	}

	if cfg.AI.DefaultProvider == "" {
		t.Error("expected non-empty default provider")
	}

	if len(cfg.AI.Providers) == 0 {
		t.Error("expected at least one default provider configured")
	}

	if _, ok := cfg.AI.Providers["local"]; !ok {
		t.Error("expected 'local' provider in defaults")
	}
}

func TestLoadFromFileDefaults(t *testing.T) {
	// When no config file exists, should return defaults
	cfg := LoadFromFile("/tmp/nonexistent-workspace-12345")

	if cfg.AI.DefaultProvider == "" {
		t.Error("expected non-empty default provider from LoadFromFile fallback")
	}

	if len(cfg.AI.Providers) == 0 {
		t.Error("expected default providers from LoadFromFile fallback")
	}
}
