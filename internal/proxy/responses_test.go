package proxy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Reproduce ChatGPT/Codex rejecting llm-guard's old top-level system field.
// The request must still redact input and restore a streamed response.
func TestProxy_ResponsesGuardNote(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		if _, exists := body["system"]; exists {
			http.Error(w, "Unsupported parameter: system", http.StatusBadRequest)
			return
		}
		instructions, _ := body["instructions"].(string)
		if !strings.HasPrefix(instructions, "Be brief.\n\n") || !strings.Contains(instructions, "[llm-guard]") {
			t.Errorf("incorrect instructions: %s", instructions)
		}
		input, _ := body["input"].(string)
		if strings.Contains(input, "AKIAIOSFODNN7EXAMPLE") || !strings.Contains(input, "⟦RG:") {
			t.Error("input was not redacted")
		}
		payload, _ := json.Marshal(map[string]string{"type": "response.output_text.delta", "delta": input})
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "event: response.output_text.delta\ndata: %s\n\n", payload)
		w.(http.Flusher).Flush()
	}))
	defer upstream.Close()
	p, err := New(upstream.URL, newTestRedactor(t), nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer p.client.CloseIdleConnections()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/backend-api/codex/responses", strings.NewReader(`{"model":"gpt-test","input":"key AKIAIOSFODNN7EXAMPLE","instructions":"Be brief.","stream":true,"store":false}`))
	p.ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "AKIAIOSFODNN7EXAMPLE") || strings.Contains(w.Body.String(), "⟦RG:") {
		t.Fatalf("response = %d %s", w.Code, w.Body)
	}
}
