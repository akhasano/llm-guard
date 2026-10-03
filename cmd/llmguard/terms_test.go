package main

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"llmguard/internal/config"
)

func TestBuildRedactorTerms(t *testing.T) {
	cfg := config.Default()
	cfg.Detectors.Regex.Enabled = false
	cfg.Detectors.Terms = []string{"ООО Ромашка", "Project Aurora"}
	r, cleanup, err := buildRedactor(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	body := []byte(`{"messages":[{"role":"user","content":"This is test data: ооо РОМАШКА / PROJECT AURORA"}]}`)
	redacted, cats := r.Redact(body)
	if bytes.Contains(redacted, []byte("РОМАШКА")) || bytes.Contains(redacted, []byte("AURORA")) {
		t.Fatal("terms leaked in test context with regex disabled")
	}
	if !reflect.DeepEqual(cats, []string{"custom_term", "custom_term"}) {
		t.Fatalf("unexpected categories: %v", cats)
	}
	var original, restored any
	if err := json.Unmarshal(body, &original); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(r.Restore(redacted), &restored); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restored, original) {
		t.Fatal("restoration did not preserve original case and content")
	}
}

func TestBuildRedactorInvalidTerms(t *testing.T) {
	cfg := config.Default()
	cfg.Detectors.Terms = []string{" "}
	if _, cleanup, err := buildRedactor(cfg); err == nil {
		cleanup()
		t.Fatal("expected invalid terms error")
	}
}
