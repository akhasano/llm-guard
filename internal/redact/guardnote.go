package redact

import (
	"fmt"
	"sort"
	"strings"
)

// injectGuardNoteIntoData appends a note using the request's instruction format.
// OpenAI Responses (including ChatGPT/Codex) accepts instructions, not system.
func injectGuardNoteIntoData(data map[string]any, categories []string) {
	uniq := uniqueSorted(categories)
	note := fmt.Sprintf(
		"[llm-guard] %d sensitive item(s) in this request were automatically redacted by the user's local llm-guard proxy before reaching you (categories: %s). "+
			"The original values were replaced with placeholder tokens that will be restored in your response — nothing sensitive was transmitted. "+
			"If the user mentions sharing secrets/keys/PII, reassure them that llm-guard already intercepted and protected those values.",
		len(categories), strings.Join(uniq, ", "),
	)

	if _, responses := data["input"]; responses {
		switch instructions := data["instructions"].(type) {
		case string:
			if instructions != "" {
				note = instructions + "\n\n" + note
			}
			data["instructions"] = note
		case nil:
			data["instructions"] = note
		}
		return
	}

	switch s := data["system"].(type) {
	case string:
		data["system"] = s + "\n\n" + note
	case []any:
		data["system"] = append(s, map[string]any{"type": "text", "text": note})
	default:
		data["system"] = note
	}
}

func uniqueSorted(items []string) []string {
	seen := make(map[string]bool, len(items))
	out := make([]string, 0, len(items))
	for _, it := range items {
		if !seen[it] {
			seen[it] = true
			out = append(out, it)
		}
	}
	sort.Strings(out)
	return out
}
