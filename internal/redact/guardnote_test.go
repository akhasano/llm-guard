package redact

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRedactForProxy_ResponsesInstructions(t *testing.T) {
	for _, instructions := range []string{"", `,"instructions":"Keep existing instructions."`, `,"instructions":null`} {
		for _, input := range []string{`"key AKIAIOSFODNN7EXAMPLE"`, `[{"role":"user","content":[{"type":"input_text","text":"key AKIAIOSFODNN7EXAMPLE"}]}]`} {
			t.Run(instructions+input, func(t *testing.T) {
				r := newTestRedactor(t)
				body := []byte(`{"model":"gpt-test","input":` + input + instructions + `,"stream":true,"store":false}`)
				redacted, categories := r.RedactForProxy(body)
				if len(categories) == 0 {
					t.Fatal("expected redaction")
				}
				var data map[string]any
				if err := json.Unmarshal(redacted, &data); err != nil {
					t.Fatal(err)
				}
				if _, exists := data["system"]; exists {
					t.Fatal("Responses API must not receive unsupported system parameter")
				}
				note, ok := data["instructions"].(string)
				if !ok || !strings.Contains(note, "[llm-guard]") {
					t.Fatalf("missing guard instructions: %v", data["instructions"])
				}
				if strings.Contains(instructions, "Keep existing") && !strings.HasPrefix(note, "Keep existing instructions.\n\n") {
					t.Fatal("existing instructions were lost")
				}
				if strings.Contains(string(redacted), "AKIAIOSFODNN7EXAMPLE") || !strings.Contains(string(redacted), "⟦RG:") {
					t.Fatal("secret not redacted")
				}
				if data["stream"] != true || data["store"] != false || data["model"] != "gpt-test" {
					t.Fatal("protocol fields changed")
				}
			})
		}
	}
}

func TestRedactForProxy_ResponsesWithoutSecrets(t *testing.T) {
	body := []byte(`{"input":"Hello","instructions":"Be brief","stream":true}`)
	redacted, categories := newTestRedactor(t).RedactForProxy(body)
	if string(redacted) != string(body) || len(categories) != 0 {
		t.Fatal("request without secrets should be unchanged")
	}
}

func TestGuardNote_AnthropicSystem(t *testing.T) {
	for _, system := range []any{"Existing system", []any{map[string]any{"type": "text", "text": "Existing system"}}} {
		data := map[string]any{"system": system, "messages": []any{}}
		injectGuardNoteIntoData(data, []string{"aws_access_key"})
		encoded, err := json.Marshal(data)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(encoded), "Existing system") || !strings.Contains(string(encoded), "[llm-guard]") {
			t.Fatal("Anthropic system instructions were lost")
		}
		if _, exists := data["instructions"]; exists {
			t.Fatal("Anthropic must not receive Responses instructions parameter")
		}
	}
}
