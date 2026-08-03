package proxy

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ─── Anthropic translator ────────────────────────────────────────────────────

type anthropicStreamState struct {
	toolName     string
	toolID       string
	toolInput    strings.Builder
	traceID      string
	responseID   string
	modelVersion string
	finished     bool
	emittedText  strings.Builder
	unsafeOutput bool
}

type anthropicUsageTotals struct {
	input      int
	output     int
	cacheRead  int
	cacheWrite int
	seen       bool
}

// toAnthropicRequest is retained for existing tests. Runtime forwarding uses
// toAnthropicRequestWithMedia so attachment conversion failures are surfaced to
// the user instead of becoming a silently text-only request.
func toAnthropicRequest(gemini map[string]any, modelName string) map[string]any {
	out, _ := toAnthropicRequestWithMedia(gemini, modelName)
	return out
}

// toAnthropicRequestWithMedia converts Gemini parts into the Anthropic
// Messages content-block format, including base64 images and PDF documents.
func toAnthropicRequestWithMedia(gemini map[string]any, modelName string) (map[string]any, error) {
	out := map[string]any{
		"model":  modelName,
		"stream": true,
	}

	if gen, ok := gemini["generationConfig"].(map[string]any); ok {
		if v, ok := gen["maxOutputTokens"]; ok {
			out["max_tokens"] = v
		}
		if v, ok := gen["temperature"]; ok {
			out["temperature"] = v
		}
	}
	if _, ok := out["max_tokens"]; !ok {
		out["max_tokens"] = 8192
	}

	// System prompt. Cache breakpoints are applied immediately before the
	// upstream request so they can be removed and retried on providers that do
	// not implement Anthropic prompt caching.
	if si, ok := gemini["systemInstruction"].(map[string]any); ok {
		var systemText string
		parts, _ := si["parts"].([]any)
		for _, p := range parts {
			if pm, ok := p.(map[string]any); ok {
				if t, ok := pm["text"].(string); ok {
					systemText += t
				}
			}
		}
		if systemText != "" {
			out["system"] = systemText
		}
	}

	// Messages
	var messages []map[string]any
	contents, _ := gemini["contents"].([]any)
	for _, c := range contents {
		cm, ok := c.(map[string]any)
		if !ok {
			continue
		}
		role, _ := cm["role"].(string)
		if role == "model" {
			role = "assistant"
		} else {
			role = "user"
		}

		parts, _ := cm["parts"].([]any)
		var blocks []any

		for _, p := range parts {
			pm, ok := p.(map[string]any)
			if !ok {
				continue
			}
			if t, ok := pm["text"].(string); ok && t != "" {
				blocks = append(blocks, map[string]any{"type": "text", "text": t})
				continue
			}
			if attachment, seen, err := attachmentFromGeminiPart(pm); seen {
				if err != nil {
					return nil, err
				}
				if err := appendAnthropicAttachment(&blocks, attachment); err != nil {
					return nil, err
				}
				continue
			}
			if fc, ok := pm["functionCall"].(map[string]any); ok {
				name, _ := fc["name"].(string)
				callID := getString(fc, "id", "callId", "call_id")
				if callID == "" {
					callID = "toolu_" + name
				}
				blocks = append(blocks, map[string]any{
					"type":  "tool_use",
					"id":    callID,
					"name":  name,
					"input": fc["args"],
				})
			} else if fr, ok := pm["functionResponse"].(map[string]any); ok {
				name, _ := fr["name"].(string)
				respJSON, _ := json.Marshal(fr["response"])
				callID := getString(fr, "id", "callId", "call_id")
				if callID == "" {
					callID = "toolu_" + name
				}
				blocks = append(blocks, map[string]any{
					"type":        "tool_result",
					"tool_use_id": callID,
					"content":     string(respJSON),
				})
			}
		}

		if len(blocks) == 0 {
			blocks = append(blocks, map[string]any{"type": "text", "text": " "})
		}

		messages = append(messages, map[string]any{
			"role":    role,
			"content": blocks,
		})
	}

	out["messages"] = messages

	// Tools
	tools := geminiToolsToAnthropic(gemini)
	if len(tools) > 0 {
		out["tools"] = tools
	}

	return out, nil
}

func appendAnthropicAttachment(blocks *[]any, attachment *geminiAttachment) error {
	if attachment.isImage() {
		*blocks = append(*blocks, map[string]any{
			"type": "image",
			"source": map[string]any{
				"type": "base64", "media_type": attachment.MimeType, "data": attachment.Data,
			},
		})
		return nil
	}
	if attachment.isPDF() {
		*blocks = append(*blocks, map[string]any{
			"type": "document",
			"source": map[string]any{
				"type": "base64", "media_type": attachment.MimeType, "data": attachment.Data,
			},
		})
		return nil
	}
	if attachment.isText() {
		text, err := attachment.text()
		if err != nil {
			return err
		}
		if attachment.Filename != "" {
			text = "[附件 " + attachment.Filename + "]\n" + text
		}
		*blocks = append(*blocks, map[string]any{"type": "text", "text": text})
		return nil
	}
	return fmt.Errorf("Anthropic Messages 不支持直接转发 %s 附件；请使用 PDF、图片或文本文件", attachment.MimeType)
}

func geminiToolsToAnthropic(gemini map[string]any) []map[string]any {
	rawTools, ok := gemini["tools"].([]any)
	if !ok {
		return nil
	}
	var result []map[string]any
	for _, rt := range rawTools {
		tm, ok := rt.(map[string]any)
		if !ok {
			continue
		}
		fds, _ := tm["functionDeclarations"].([]any)
		for _, fd := range fds {
			fdm, ok := fd.(map[string]any)
			if !ok {
				continue
			}
			name, _ := fdm["name"].(string)
			desc, _ := fdm["description"].(string)
			params, _ := fdm["parameters"].(map[string]any)
			if params == nil {
				params = map[string]any{"type": "object", "properties": map[string]any{}}
			}
			params = normalizeJSONSchema(params).(map[string]any)
			result = append(result, map[string]any{
				"name":         name,
				"description":  desc,
				"input_schema": params,
			})
		}
	}
	return result
}

// collectAnthropicUsage accumulates token usage from Anthropic SSE events.
func collectAnthropicUsage(line string, totals *anthropicUsageTotals) {
	if !strings.HasPrefix(line, "data: ") {
		return
	}
	payload := strings.TrimSpace(strings.TrimPrefix(line, "data: "))
	if payload == "" || payload == "[DONE]" {
		return
	}
	var event map[string]any
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		return
	}

	extract := func(usage map[string]any) {
		totals.seen = true
		if v, ok := usage["input_tokens"].(float64); ok {
			totals.input += int(v)
		}
		if v, ok := usage["output_tokens"].(float64); ok {
			totals.output += int(v)
		}
		if v, ok := usage["cache_read_input_tokens"].(float64); ok {
			totals.cacheRead += int(v)
		}
		if v, ok := usage["cache_creation_input_tokens"].(float64); ok {
			totals.cacheWrite += int(v)
		}
	}

	if msg, ok := event["message"].(map[string]any); ok {
		if usage, ok := msg["usage"].(map[string]any); ok {
			extract(usage)
		}
	}
	if usage, ok := event["usage"].(map[string]any); ok {
		extract(usage)
	}
}

// convertAnthropicLineToGemini converts an Anthropic SSE event to Gemini format.
func convertAnthropicLineToGemini(line string, state *anthropicStreamState) string {
	if !strings.HasPrefix(line, "data: ") {
		return ""
	}
	payload := strings.TrimSpace(strings.TrimPrefix(line, "data: "))
	if payload == "" || payload == "[DONE]" {
		return ""
	}

	var event map[string]any
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		return ""
	}

	eventType, _ := event["type"].(string)
	var parts []map[string]any
	if message, ok := event["message"].(map[string]any); ok {
		if responseID, ok := message["id"].(string); ok && responseID != "" {
			state.responseID = responseID
		}
		if modelVersion, ok := message["model"].(string); ok && modelVersion != "" {
			state.modelVersion = modelVersion
		}
	}

	switch eventType {
	case "content_block_start":
		if block, ok := event["content_block"].(map[string]any); ok {
			if block["type"] == "tool_use" {
				state.toolName, _ = block["name"].(string)
				state.toolID, _ = block["id"].(string)
				state.toolInput.Reset()
			}
		}

	case "content_block_delta":
		if delta, ok := event["delta"].(map[string]any); ok {
			deltaType, _ := delta["type"].(string)
			if deltaType == "text_delta" {
				if text, ok := delta["text"].(string); ok && text != "" {
					state.emittedText.WriteString(text)
					parts = append(parts, map[string]any{"text": text})
				}
			} else if deltaType == "input_json_delta" {
				if partial, ok := delta["partial_json"].(string); ok {
					state.toolInput.WriteString(partial)
				}
			}
		}

	case "content_block_stop":
		if state.toolName != "" {
			var args map[string]any
			json.Unmarshal([]byte(state.toolInput.String()), &args)
			if args == nil {
				args = map[string]any{}
			}
			parts = append(parts, map[string]any{
				"functionCall": map[string]any{
					"name": state.toolName,
					"args": args,
				},
			})
			state.unsafeOutput = true
			state.toolName = ""
			state.toolID = ""
			state.toolInput.Reset()
		}

	case "message_stop":
		state.finished = true
		response := map[string]any{
			"candidates": []any{
				map[string]any{
					"content":      map[string]any{"role": "model", "parts": []any{}},
					"finishReason": "STOP",
				},
			},
		}
		if state.responseID != "" {
			response["responseId"] = state.responseID
		}
		if state.modelVersion != "" {
			response["modelVersion"] = state.modelVersion
		}
		return encodeAntigravityStreamEvent(response, state.traceID)
	}

	if len(parts) == 0 {
		return ""
	}

	response := map[string]any{
		"candidates": []any{
			map[string]any{
				"content": map[string]any{
					"role":  "model",
					"parts": parts,
				},
			},
		},
	}
	if state.responseID != "" {
		response["responseId"] = state.responseID
	}
	if state.modelVersion != "" {
		response["modelVersion"] = state.modelVersion
	}
	return encodeAntigravityStreamEvent(response, state.traceID)
}
