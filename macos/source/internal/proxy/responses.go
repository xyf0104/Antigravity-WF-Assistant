package proxy

import (
	"encoding/json"
	"fmt"
	"strings"

	"antigravity-byok/internal/storage"
)

// OpenAI Responses is the upstream surface that can represent general file
// inputs, hosted web search and image generation. It is selected explicitly,
// or automatically when a request needs one of those capabilities.
func toOpenAIResponsesRequest(gemini map[string]any, modelName string, model *storage.CustomModel) (map[string]any, error) {
	out := map[string]any{
		"model":  modelName,
		"stream": true,
		// Antigravity owns the local conversation transcript. Avoid creating a
		// second, provider-side durable conversation by default.
		"store": false,
	}
	if gen, ok := gemini["generationConfig"].(map[string]any); ok {
		if value, ok := gen["maxOutputTokens"]; ok {
			out["max_output_tokens"] = value
		}
		if value, ok := gen["temperature"]; ok {
			out["temperature"] = value
		}
		if value, ok := gen["topP"]; ok {
			out["top_p"] = value
		}
	}
	if _, ok := out["max_output_tokens"]; !ok {
		out["max_output_tokens"] = 8192
	}

	if system := geminiSystemText(gemini); system != "" {
		out["instructions"] = system
	}

	var input []any
	contents, _ := gemini["contents"].([]any)
	for _, rawContent := range contents {
		content, ok := rawContent.(map[string]any)
		if !ok {
			continue
		}
		role, _ := content["role"].(string)
		if role == "model" {
			role = "assistant"
		} else if role == "" {
			role = "user"
		}
		parts, _ := content["parts"].([]any)
		var blocks []any
		var items []any
		for _, rawPart := range parts {
			part, ok := rawPart.(map[string]any)
			if !ok {
				continue
			}
			if text, ok := part["text"].(string); ok {
				blocks = append(blocks, responseTextBlock(role, text))
				continue
			}
			if attachment, seen, err := attachmentFromGeminiPart(part); seen {
				if err != nil {
					return nil, err
				}
				if role == "assistant" {
					return nil, fmt.Errorf("不能将历史助手附件转换为 Responses 输入")
				}
				blocks = append(blocks, responseAttachmentBlock(attachment))
				continue
			}
			if functionCall, ok := part["functionCall"].(map[string]any); ok {
				name, _ := functionCall["name"].(string)
				arguments, _ := json.Marshal(functionCall["args"])
				callID := getString(functionCall, "id", "callId", "call_id")
				if callID == "" {
					callID = "call_" + name
				}
				items = append(items, map[string]any{
					"type": "function_call", "call_id": callID, "name": name, "arguments": string(arguments),
				})
				continue
			}
			if functionResponse, ok := part["functionResponse"].(map[string]any); ok {
				name, _ := functionResponse["name"].(string)
				output, _ := json.Marshal(functionResponse["response"])
				callID := getString(functionResponse, "id", "callId", "call_id")
				if callID == "" {
					callID = "call_" + name
				}
				items = append(items, map[string]any{
					"type": "function_call_output", "call_id": callID, "output": string(output),
				})
			}
		}
		if len(blocks) > 0 {
			input = append(input, map[string]any{"role": role, "content": blocks})
		}
		input = append(input, items...)
	}
	if len(input) == 0 {
		input = append(input, map[string]any{"role": "user", "content": []any{responseTextBlock("user", " ")}})
	}
	out["input"] = input

	tools := geminiToolsToOpenAIResponses(gemini)
	capabilities := storage.EffectiveCapabilities(*model)
	if capabilities.SupportsWebSearch {
		tools = append(tools, map[string]any{"type": "web_search"})
	}
	if capabilities.SupportsImageGeneration {
		tools = append(tools, map[string]any{"type": "image_generation"})
	}
	if len(tools) > 0 {
		out["tools"] = tools
	}
	return out, nil
}

func geminiSystemText(gemini map[string]any) string {
	instruction, _ := gemini["systemInstruction"].(map[string]any)
	parts, _ := instruction["parts"].([]any)
	var text strings.Builder
	for _, rawPart := range parts {
		part, _ := rawPart.(map[string]any)
		if value, ok := part["text"].(string); ok {
			text.WriteString(value)
		}
	}
	return text.String()
}

func responseTextBlock(role, text string) map[string]any {
	if role == "assistant" {
		return map[string]any{"type": "output_text", "text": text}
	}
	return map[string]any{"type": "input_text", "text": text}
}

func responseAttachmentBlock(attachment *geminiAttachment) map[string]any {
	if attachment.isImage() {
		return map[string]any{"type": "input_image", "image_url": attachment.dataURL(), "detail": "auto"}
	}
	block := map[string]any{
		"type": "input_file", "file_data": attachment.dataURL(),
	}
	if attachment.Filename != "" {
		block["filename"] = attachment.Filename
	}
	if attachment.isPDF() {
		block["detail"] = "auto"
	}
	return block
}

func geminiToolsToOpenAIResponses(gemini map[string]any) []map[string]any {
	rawTools, _ := gemini["tools"].([]any)
	var result []map[string]any
	for _, rawTool := range rawTools {
		tool, _ := rawTool.(map[string]any)
		declarations, _ := tool["functionDeclarations"].([]any)
		for _, rawDeclaration := range declarations {
			declaration, _ := rawDeclaration.(map[string]any)
			name, _ := declaration["name"].(string)
			if name == "" {
				continue
			}
			parameters, _ := declaration["parameters"].(map[string]any)
			if parameters == nil {
				parameters = map[string]any{"type": "object", "properties": map[string]any{}}
			}
			parameters = normalizeJSONSchema(parameters).(map[string]any)
			result = append(result, map[string]any{
				"type": "function", "name": name, "description": getString(declaration, "description"), "parameters": parameters,
			})
		}
	}
	return result
}

type openAIResponsesStreamState struct {
	toolCalls    map[string]*openAIResponsesToolCall
	usage        map[string]any
	traceID      string
	responseID   string
	modelVersion string
	finished     bool
	emittedText  strings.Builder
	unsafeOutput bool
}

type openAIResponsesToolCall struct {
	name    string
	args    strings.Builder
	emitted bool
}

func convertOpenAIResponsesLineToGemini(line string, state *openAIResponsesStreamState) string {
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
	if state.toolCalls == nil {
		state.toolCalls = map[string]*openAIResponsesToolCall{}
	}
	if response, ok := event["response"].(map[string]any); ok {
		captureResponsesMetadata(response, state)
	}
	if id, ok := event["response_id"].(string); ok && id != "" {
		state.responseID = id
	}

	eventType, _ := event["type"].(string)
	var parts []any
	switch eventType {
	case "response.output_text.delta":
		if delta, ok := event["delta"].(string); ok && delta != "" {
			state.emittedText.WriteString(delta)
			parts = append(parts, map[string]any{"text": delta})
		}
	case "response.output_item.added":
		captureResponsesOutputItem(event["item"], state)
	case "response.function_call_arguments.delta":
		callID := getString(event, "call_id", "item_id")
		if callID != "" {
			call := state.toolCalls[callID]
			if call == nil {
				call = &openAIResponsesToolCall{}
				state.toolCalls[callID] = call
			}
			if delta, ok := event["delta"].(string); ok {
				call.args.WriteString(delta)
			}
		}
	case "response.function_call_arguments.done":
		callID := getString(event, "call_id", "item_id")
		if callID != "" {
			if call := state.toolCalls[callID]; call != nil {
				if args, ok := event["arguments"].(string); ok && args != "" && call.args.Len() == 0 {
					call.args.WriteString(args)
				}
				parts = append(parts, responseToolCallPart(callID, call, state))
			}
		}
	case "response.output_item.done":
		parts = append(parts, responseCompletedOutputItem(event["item"], state)...)
	case "response.completed":
		state.finished = true
		return responsesFinishEvent("STOP", state)
	case "response.incomplete":
		state.finished = true
		return responsesFinishEvent("MAX_TOKENS", state)
	case "response.failed":
		// Leave the stream unfinished so the proxy can reconnect or let an
		// upstream account pool fail over instead of surfacing a client retry.
		return ""
	}
	if len(parts) == 0 {
		return ""
	}
	return responsesEvent(parts, "", state)
}

func captureResponsesMetadata(response map[string]any, state *openAIResponsesStreamState) {
	if value, ok := response["id"].(string); ok && value != "" {
		state.responseID = value
	}
	if value, ok := response["model"].(string); ok && value != "" {
		state.modelVersion = value
	}
	if usage, ok := response["usage"].(map[string]any); ok {
		state.usage = usage
	}
}

func captureResponsesOutputItem(raw any, state *openAIResponsesStreamState) {
	item, _ := raw.(map[string]any)
	if item["type"] != "function_call" {
		return
	}
	callID := getString(item, "call_id", "id")
	if callID == "" {
		return
	}
	call := state.toolCalls[callID]
	if call == nil {
		call = &openAIResponsesToolCall{}
		state.toolCalls[callID] = call
	}
	call.name = getString(item, "name")
	if arguments, ok := item["arguments"].(string); ok && arguments != "" && call.args.Len() == 0 {
		call.args.WriteString(arguments)
	}
}

func responseCompletedOutputItem(raw any, state *openAIResponsesStreamState) []any {
	item, _ := raw.(map[string]any)
	if item == nil {
		return nil
	}
	switch item["type"] {
	case "function_call":
		captureResponsesOutputItem(item, state)
		callID := getString(item, "call_id", "id")
		if call := state.toolCalls[callID]; call != nil {
			return []any{responseToolCallPart(callID, call, state)}
		}
	case "image_generation_call":
		if result, ok := item["result"].(string); ok && result != "" {
			data, mimeType, err := normaliseAttachmentData(result, "image/png")
			if err == nil {
				state.unsafeOutput = true
				return []any{map[string]any{"inlineData": map[string]any{"mimeType": normaliseMimeType(mimeType), "data": data}}}
			}
		}
	}
	return nil
}

func responseToolCallPart(callID string, call *openAIResponsesToolCall, state *openAIResponsesStreamState) any {
	if call.emitted {
		return nil
	}
	call.emitted = true
	state.unsafeOutput = true
	arguments := map[string]any{}
	_ = json.Unmarshal([]byte(call.args.String()), &arguments)
	return map[string]any{"functionCall": map[string]any{"id": callID, "name": call.name, "args": arguments}}
}

func responsesEvent(parts []any, finishReason string, state *openAIResponsesStreamState) string {
	filtered := make([]any, 0, len(parts))
	for _, part := range parts {
		if part != nil {
			filtered = append(filtered, part)
		}
	}
	if len(filtered) == 0 && finishReason == "" {
		return ""
	}
	response := map[string]any{
		"candidates": []any{map[string]any{
			"content": map[string]any{"role": "model", "parts": filtered}, "finishReason": finishReason,
		}},
	}
	if state.responseID != "" {
		response["responseId"] = state.responseID
	}
	if state.modelVersion != "" {
		response["modelVersion"] = state.modelVersion
	}
	if state.usage != nil {
		input, _ := state.usage["input_tokens"].(float64)
		output, _ := state.usage["output_tokens"].(float64)
		response["usageMetadata"] = map[string]any{
			"promptTokenCount": int(input), "candidatesTokenCount": int(output), "totalTokenCount": int(input + output),
		}
	}
	return encodeAntigravityStreamEvent(response, state.traceID)
}

func responsesFinishEvent(reason string, state *openAIResponsesStreamState) string {
	return responsesEvent(nil, reason, state)
}
