package proxy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"antigravity-byok/internal/storage"
)

type promptCacheResult struct {
	key      string
	explicit bool
}

var unsupportedCachePattern = regexp.MustCompile(`(?i)prompt[_ -]?cache|cache[_ -]?control|cache[_ -]?breakpoint|cached[_ -]?content|additional propert|unknown (field|parameter)|unrecognized (field|parameter)|unsupported (field|parameter)`)

func hashString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func stableConversationPrefix(request map[string]any) string {
	var system strings.Builder
	if instruction, ok := request["systemInstruction"].(map[string]any); ok {
		if parts, ok := instruction["parts"].([]any); ok {
			for _, raw := range parts {
				part, _ := raw.(map[string]any)
				if text, ok := part["text"].(string); ok {
					if system.Len() > 0 {
						system.WriteByte('\n')
					}
					system.WriteString(text)
				}
			}
		}
	}
	var first any
	if contents, ok := request["contents"].([]any); ok && len(contents) > 0 {
		first = contents[0]
	}
	prefix, _ := json.Marshal(map[string]any{"system": system.String(), "firstContent": first})
	return hashString(string(prefix))
}

func buildPromptCacheKey(model *storage.CustomModel, request map[string]any) string {
	credentialScope := hashString(model.APIKey)
	if len(credentialScope) > 16 {
		credentialScope = credentialScope[:16]
	}
	scope := strings.Join([]string{
		model.APIURL,
		model.ExternalModelName,
		credentialScope,
		stableConversationPrefix(request),
	}, "\x00")
	digest := hashString(scope)
	return "antigravity:" + digest[:24]
}

// supportsOpenAIExplicitCaching follows the Chat Completions contract where
// prompt_cache_options is supported by GPT-5.6 and later GPT versions.
func supportsOpenAIExplicitCaching(modelName string) bool {
	name := strings.ToLower(modelName)
	match := regexp.MustCompile(`gpt[-_.]?(\d+)(?:[-_.]?(\d+))?`).FindStringSubmatch(name)
	if len(match) == 0 {
		return false
	}
	major, _ := strconv.Atoi(match[1])
	minor := 0
	if len(match) > 2 && match[2] != "" {
		minor, _ = strconv.Atoi(match[2])
	}
	return major > 5 || (major == 5 && minor >= 6)
}

func applyOpenAIPromptCaching(request map[string]any, model *storage.CustomModel, source map[string]any) promptCacheResult {
	key := buildPromptCacheKey(model, source)
	request["prompt_cache_key"] = key
	explicit := supportsOpenAIExplicitCaching(model.ExternalModelName)
	if !explicit {
		return promptCacheResult{key: key}
	}
	request["prompt_cache_options"] = map[string]any{"mode": "implicit", "ttl": "30m"}
	messages, _ := request["messages"].([]map[string]any)
	for _, message := range messages {
		if message["role"] != "system" {
			continue
		}
		if text, ok := message["content"].(string); ok && text != "" {
			message["content"] = []any{map[string]any{
				"type":                    "text",
				"text":                    text,
				"prompt_cache_breakpoint": map[string]any{"mode": "explicit"},
			}}
		}
		break
	}
	return promptCacheResult{key: key, explicit: explicit}
}

func stripOpenAIPromptCaching(request map[string]any) {
	delete(request, "prompt_cache_key")
	delete(request, "prompt_cache_options")
	messages, _ := request["messages"].([]map[string]any)
	for _, message := range messages {
		blocks, ok := message["content"].([]any)
		if !ok {
			continue
		}
		for _, raw := range blocks {
			if block, ok := raw.(map[string]any); ok {
				delete(block, "prompt_cache_breakpoint")
			}
		}
		if len(blocks) == 1 {
			if block, ok := blocks[0].(map[string]any); ok && block["type"] == "text" {
				message["content"], _ = block["text"].(string)
			}
		}
	}
}

func cacheableAnthropicBlock(block map[string]any) bool {
	if block == nil || block["type"] == "thinking" {
		return false
	}
	if block["type"] == "text" && strings.TrimSpace(stringValue(block["text"])) == "" {
		return false
	}
	return true
}

func markLastCacheable(blocks []any) bool {
	for index := len(blocks) - 1; index >= 0; index-- {
		block, _ := blocks[index].(map[string]any)
		if !cacheableAnthropicBlock(block) {
			continue
		}
		block["cache_control"] = map[string]any{"type": "ephemeral"}
		return true
	}
	return false
}

func applyAnthropicPromptCaching(request map[string]any) int {
	count := 0
	if tools, ok := request["tools"].([]map[string]any); ok && len(tools) > 0 {
		tools[len(tools)-1]["cache_control"] = map[string]any{"type": "ephemeral"}
		count++
	}
	if system, ok := request["system"].(string); ok && system != "" {
		request["system"] = []any{map[string]any{"type": "text", "text": system}}
	}
	if system, ok := request["system"].([]any); ok && markLastCacheable(system) {
		count++
	}
	messages, _ := request["messages"].([]map[string]any)
	indices := []int{len(messages) - 2, len(messages) - 1}
	if len(messages) == 1 {
		indices = []int{0}
	}
	for _, index := range indices {
		if count >= 4 || index < 0 || index >= len(messages) {
			continue
		}
		message := messages[index]
		if content, ok := message["content"].(string); ok {
			message["content"] = []any{map[string]any{"type": "text", "text": content}}
		}
		if blocks, ok := message["content"].([]any); ok && markLastCacheable(blocks) {
			count++
		}
	}
	return count
}

func stripAnthropicPromptCaching(request map[string]any) {
	if tools, ok := request["tools"].([]map[string]any); ok {
		for _, tool := range tools {
			delete(tool, "cache_control")
		}
	}
	if system, ok := request["system"].([]any); ok {
		stripCacheControl(system)
		if len(system) == 1 {
			if block, ok := system[0].(map[string]any); ok && block["type"] == "text" {
				request["system"], _ = block["text"].(string)
			}
		}
	}
	messages, _ := request["messages"].([]map[string]any)
	for _, message := range messages {
		blocks, ok := message["content"].([]any)
		if !ok {
			continue
		}
		stripCacheControl(blocks)
		if len(blocks) == 1 {
			if block, ok := blocks[0].(map[string]any); ok && block["type"] == "text" {
				message["content"], _ = block["text"].(string)
			}
		}
	}
}

func stripCacheControl(blocks []any) {
	for _, raw := range blocks {
		if block, ok := raw.(map[string]any); ok {
			delete(block, "cache_control")
		}
	}
}

func isUnsupportedCacheResponse(statusCode int, body string) bool {
	return (statusCode == 400 || statusCode == 422) && unsupportedCachePattern.MatchString(body)
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}
