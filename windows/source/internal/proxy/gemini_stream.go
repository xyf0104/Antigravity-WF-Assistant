package proxy

import "encoding/json"

// encodeAntigravityStreamEvent wraps a Gemini generate-content response in
// the internal Cloud Code envelope consumed by Antigravity. The public Gemini
// API streams candidates at the top level, but Antigravity's
// v1internal:streamGenerateContent endpoint always nests them under response.
func encodeAntigravityStreamEvent(response map[string]any, traceID string) string {
	if traceID == "" {
		traceID = "antigravity-byok"
	}
	envelope := map[string]any{
		"response": response,
		"traceId":  traceID,
		"metadata": map[string]any{},
	}
	out, _ := json.Marshal(envelope)
	return "data: " + string(out) + "\n\n"
}
