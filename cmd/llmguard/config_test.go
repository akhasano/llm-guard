package main

import (
	"path/filepath"
	"strings"
	"testing"

	"llmguard/internal/config"
)

func TestConfigFlag(t *testing.T) {
	for _, args := range [][]string{
		{"--config", "PATH", "init"},
		{"init", "--config", "PATH"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			path := filepath.Join(t.TempDir(), "custom.yaml")
			cfg := config.Default()
			cfg.Listen = "127.0.0.1:9876"
			if err := config.Save(path, cfg); err != nil {
				t.Fatal(err)
			}
			root := rootCmd()
			t.Cleanup(func() { configPath = "" })
			actual := append([]string(nil), args...)
			for i, arg := range actual {
				if arg == "PATH" {
					actual[i] = path
				}
			}
			root.SetArgs(actual)
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}
			loaded, err := loadOrDefaultConfig()
			if err != nil {
				t.Fatal(err)
			}
			if loaded.Listen != cfg.Listen {
				t.Fatalf("listen = %q, want %q", loaded.Listen, cfg.Listen)
			}
			defaultPath, err := config.Path()
			if err != nil {
				t.Fatal(err)
			}
			if config.Exists(defaultPath) {
				t.Fatal("custom config command created default config")
			}
		})
	}
}

func TestMissingExplicitConfig(t *testing.T) {
	configPath = filepath.Join(t.TempDir(), "missing.yaml")
	t.Cleanup(func() { configPath = "" })
	if _, err := loadOrDefaultConfig(); err == nil {
		t.Fatal("missing explicit config should not silently use defaults")
	}
}

func TestDefaultConfigFallback(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	configPath = ""
	if _, err := loadOrDefaultConfig(); err != nil {
		t.Fatal(err)
	}
}
