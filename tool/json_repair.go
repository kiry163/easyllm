package tool

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	jsonrepair "github.com/RealAlexandreAI/json-repair"
)

type decodeCandidate struct {
	text     string
	repaired bool
}

func DecodeJSONObjectBytes(raw []byte) (map[string]any, bool, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return map[string]any{}, false, nil
	}
	return decodeJSONObjectString(string(raw), false)
}

func DecodeJSONObjectString(text string) (map[string]any, bool, error) {
	return decodeJSONObjectString(text, false)
}

func DecodeJSONArrayString(text string) ([]map[string]any, bool, error) {
	current := strings.TrimSpace(text)
	if current == "" {
		return nil, false, fmt.Errorf("json array string is empty")
	}

	queue := []decodeCandidate{{text: current}}
	seen := map[string]struct{}{}

	for len(queue) > 0 {
		candidate := queue[0]
		queue = queue[1:]
		current = strings.TrimSpace(candidate.text)
		if current == "" {
			continue
		}
		if _, ok := seen[current]; ok {
			continue
		}
		seen[current] = struct{}{}

		if parsed, ok := decodeJSONArrayText(current); ok {
			return parsed, candidate.repaired, nil
		}

		queue = append(queue, nextCandidates(current)...)
	}

	return nil, false, fmt.Errorf("unable to decode JSON array string")
}

func decodeJSONObjectString(text string, repaired bool) (map[string]any, bool, error) {
	current := strings.TrimSpace(text)
	if current == "" {
		return map[string]any{}, repaired, nil
	}

	queue := []decodeCandidate{{text: current, repaired: repaired}}
	seen := map[string]struct{}{}

	for len(queue) > 0 {
		candidate := queue[0]
		queue = queue[1:]
		current = strings.TrimSpace(candidate.text)
		if current == "" {
			return map[string]any{}, candidate.repaired, nil
		}
		if _, ok := seen[current]; ok {
			continue
		}
		seen[current] = struct{}{}

		if parsed, ok := decodeJSONObjectText(current); ok {
			return parsed, candidate.repaired, nil
		}

		for _, next := range nextCandidates(current) {
			next.repaired = next.repaired || candidate.repaired
			queue = append(queue, next)
		}
	}

	return nil, false, fmt.Errorf("unable to decode JSON object string")
}

func nextCandidates(text string) []decodeCandidate {
	candidates := make([]decodeCandidate, 0, 4)
	if nested, ok := unwrapJSONString(text); ok {
		candidates = append(candidates, decodeCandidate{text: nested, repaired: true})
	}
	if stripped, ok := stripCodeFence(text); ok {
		candidates = append(candidates, decodeCandidate{text: stripped, repaired: true})
	}
	if unquoted, ok := trimOuterQuotes(text); ok {
		candidates = append(candidates, decodeCandidate{text: unquoted, repaired: true})
	}
	if repaired, ok := repairJSONText(text); ok {
		candidates = append(candidates, decodeCandidate{text: repaired, repaired: true})
	}
	return candidates
}

func decodeJSONObjectText(text string) (map[string]any, bool) {
	var parsed map[string]any
	if err := json.Unmarshal([]byte(text), &parsed); err == nil && parsed != nil {
		return parsed, true
	}
	return nil, false
}

func decodeJSONArrayText(text string) ([]map[string]any, bool) {
	var decoded []any
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		return nil, false
	}
	items, err := objectItemsFromSlice(decoded)
	if err != nil {
		return nil, false
	}
	return items, true
}

func objectItemsFromSlice(items []any) ([]map[string]any, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("items must be non-empty")
	}
	out := make([]map[string]any, 0, len(items))
	for i, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("items[%d] must be an object", i)
		}
		out = append(out, m)
	}
	return out, nil
}

func unwrapJSONString(text string) (string, bool) {
	var nested string
	if err := json.Unmarshal([]byte(text), &nested); err != nil {
		return "", false
	}
	return strings.TrimSpace(nested), true
}

func stripCodeFence(text string) (string, bool) {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "```") {
		return "", false
	}

	body := strings.TrimPrefix(trimmed, "```")
	if idx := strings.IndexByte(body, '\n'); idx >= 0 {
		body = body[idx+1:]
	}
	body = strings.TrimSpace(body)
	body = strings.TrimSuffix(body, "```")
	body = strings.TrimSpace(body)
	if body == "" || body == trimmed {
		return "", false
	}
	return body, true
}

func trimOuterQuotes(text string) (string, bool) {
	trimmed := strings.TrimSpace(text)
	if len(trimmed) < 2 {
		return "", false
	}
	first := trimmed[0]
	last := trimmed[len(trimmed)-1]
	if (first != '"' || last != '"') && (first != '\'' || last != '\'') {
		return "", false
	}
	return strings.TrimSpace(trimmed[1 : len(trimmed)-1]), true
}

func repairJSONText(text string) (string, bool) {
	repaired, err := jsonrepair.RepairJSON(text)
	if err != nil {
		return "", false
	}
	repaired = strings.TrimSpace(repaired)
	if repaired == "" || repaired == strings.TrimSpace(text) {
		return "", false
	}
	return repaired, true
}
