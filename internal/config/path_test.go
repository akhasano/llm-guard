package config

import (
	"path/filepath"
	"testing"
)

func TestResolvePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, input := range []string{"", "custom.yaml", filepath.Join(home, "custom.yaml")} {
		want := filepath.Join(home, ".config", "llmguard", "config.yaml")
		if input != "" {
			var err error
			want, err = filepath.Abs(input)
			if err != nil {
				t.Fatal(err)
			}
		}
		got, err := ResolvePath(input)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("ResolvePath(%q) = %q, want %q", input, got, want)
		}
	}
}
