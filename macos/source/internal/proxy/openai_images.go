package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"antigravity-wf-assistant/internal/storage"
	"antigravity-wf-assistant/internal/upstream"
)

// A direct Images response contains base64 data, so reserve enough space for
// a complete 50 MB image plus its JSON envelope. The same limit is used by the
// attachment bridge; keeping it bounded prevents a malformed gateway response
// from exhausting the local proxy process.
const maxOpenAIImageGenerationResponseBytes int64 = (maxForwardedAttachmentBytes * 4 / 3) + (2 << 20)

// directOpenAIImageModel returns an image-only model from the same configured
// upstream as the active text model. Antigravity sends an image-generation
// feature request to the selected text model (for example gpt-5.6-sol), but
// OpenAI-compatible gateways normally expose image generation through a
// separate model such as gpt-image-2 at /v1/images/generations.
//
// The image model is used only for its upstream model id. Credentials and
// account-pool scheduling deliberately come from the selected text model, so
// one API account remains one coherent pool and a different saved API key can
// never be selected accidentally.
func directOpenAIImageModel(model *storage.CustomModel) *storage.CustomModel {
	if model == nil || !isOpenAICompatibleImageProvider(model.Provider) {
		return nil
	}
	if upstream.IsOpenAICodexOAuth(upstream.ConfigFromModel(*model)) {
		// ChatGPT Codex OAuth has a Responses-only contract; it is not an API-key
		// image endpoint and must stay on the existing Responses route.
		return nil
	}
	endpoint, err := upstream.ResolveImagesGenerationsURLForConfig(upstream.ConfigFromModel(*model))
	if err != nil || endpoint == "" {
		return nil
	}

	candidates := []storage.CustomModel{*model}
	configured, loadErr := storage.LoadEnabledModels()
	if loadErr == nil {
		candidates = append(candidates, configured...)
	}

	valid := make([]storage.CustomModel, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		if !isOpenAICompatibleImageProvider(candidate.Provider) || !isDirectImageModelName(candidate.ExternalModelName) {
			continue
		}
		candidateEndpoint, err := upstream.ResolveImagesGenerationsURLForConfig(upstream.ConfigFromModel(candidate))
		if err != nil || candidateEndpoint != endpoint {
			continue
		}
		key := strings.TrimSpace(candidate.Name) + "\x00" + strings.TrimSpace(candidate.ExternalModelName)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		valid = append(valid, candidate)
	}
	if len(valid) == 0 {
		return nil
	}
	sort.SliceStable(valid, func(i, j int) bool {
		left, right := directImageModelPriority(valid[i].ExternalModelName), directImageModelPriority(valid[j].ExternalModelName)
		if left != right {
			return left < right
		}
		return strings.ToLower(valid[i].ExternalModelName) < strings.ToLower(valid[j].ExternalModelName)
	})
	selected := valid[0]
	return &selected
}

func isOpenAICompatibleImageProvider(provider string) bool {
	switch upstream.NormalizedProvider(provider) {
	case "openai", "grok", "custom":
		return true
	default:
		return false
	}
}

func isDirectImageModelName(value string) bool {
	value = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(value, "models/")))
	if value == "" {
		return false
	}
	for _, marker := range []string{
		"gpt-image", "dall-e", "imagen", "imagegen", "image-gen", "stable-diffusion", "stable_diffusion", "sdxl", "flux", "midjourney",
	} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return strings.HasPrefix(value, "image-") || value == "image" || value == "image2" || value == "image-2"
}

func directImageModelPriority(value string) int {
	value = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(value, "models/")))
	switch {
	case value == "gpt-image-2" || value == "image-2" || value == "image2":
		return 0
	case strings.HasPrefix(value, "gpt-image"):
		return 10
	case strings.HasPrefix(value, "image-") || value == "image":
		return 20
	case strings.Contains(value, "dall-e"):
		return 30
	case strings.Contains(value, "imagen"):
		return 40
	case strings.Contains(value, "flux"):
		return 50
	case strings.Contains(value, "stable") || strings.Contains(value, "sdxl"):
		return 60
	default:
		return 100
	}
}

func requestsDirectImageGeneration(gemini map[string]any) bool {
	if requested, _ := gemini["wfNativeImageGeneration"].(bool); requested {
		return true
	}
	_, requested := requestedResponsesBuiltinTools(gemini)[responseImageGenerationTool]
	return requested
}

// forwardOpenAIImagesGeneration performs one dedicated OpenAI Images request
// and converts its base64 result back into Antigravity's native streaming
// envelope. It never replays an uncertain network request: retry/failover is
// limited to a concrete non-2xx rejection, where the upstream proves that no
// image generation started.
func forwardOpenAIImagesGeneration(w http.ResponseWriter, incoming *http.Request, model *storage.CustomModel, imageModel *storage.CustomModel, gemini map[string]any, requestID string) {
	if model == nil || imageModel == nil || strings.TrimSpace(imageModel.ExternalModelName) == "" {
		http.Error(w, "未找到同一上游的可用图片模型", http.StatusBadRequest)
		return
	}
	prompt := directImageGenerationPrompt(gemini)
	requestBody := map[string]any{
		"model":           strings.TrimSpace(imageModel.ExternalModelName),
		"prompt":          prompt,
		"n":               1,
		"response_format": "b64_json",
	}
	encoded, err := json.Marshal(requestBody)
	if err != nil {
		http.Error(w, "无法构建图片生成请求", http.StatusBadRequest)
		return
	}

	policy := currentStreamRecoveryPolicy()
	excludedAccounts := map[string]struct{}{}
	reconnects := 0
	client := &http.Client{Timeout: upstreamStreamTimeout}
	for attempt := 1; ; attempt++ {
		attemptModel, lease, err := acquireAttemptModel(model, excludedAccounts)
		if err != nil {
			trace("images-account-pool-error", map[string]any{"requestId": requestID, "attempt": attempt, "message": err.Error()})
			http.Error(w, accountPoolError("图片模型", err), http.StatusServiceUnavailable)
			return
		}
		config := upstream.ConfigFromModel(*attemptModel)
		if upstream.IsOpenAICodexOAuth(config) {
			releaseAttemptSuccess(lease)
			http.Error(w, "当前官方 OAuth 账户不支持专用图片模型接口", http.StatusBadRequest)
			return
		}
		endpoint, err := upstream.ResolveImagesGenerationsURLForConfig(config)
		if err != nil {
			releaseAttemptSuccess(lease)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		req, err := http.NewRequestWithContext(incoming.Context(), http.MethodPost, endpoint, bytes.NewReader(encoded))
		if err != nil {
			releaseAttemptSuccess(lease)
			http.Error(w, "无法创建图片生成请求", http.StatusBadGateway)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		if err := upstream.ApplyCredentials(req, config); err != nil {
			releaseAttemptSuccess(lease)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		trace("images-upstream-request", map[string]any{
			"requestId": requestID, "attempt": attempt, "imageModel": imageModel.ExternalModelName,
			"accountId": func() string {
				if lease == nil {
					return ""
				}
				return lease.ID
			}(),
		})
		resp, err := client.Do(req)
		if err != nil {
			releaseAttemptFailure(lease, 0, "", err.Error())
			excludeFailedAttempt(excludedAccounts, lease)
			trace("images-upstream-error", map[string]any{"requestId": requestID, "attempt": attempt, "message": err.Error()})
			if incoming.Context().Err() == nil {
				http.Error(w, "无法确认上游是否已接收图片生成请求；为避免重复扣费，本次请求未自动重试", http.StatusBadGateway)
			}
			return
		}
		observeAttemptQuota(lease, "images", resp)
		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			_, _ = io.ReadAll(io.LimitReader(resp.Body, 64<<10))
			resp.Body.Close()
			retryAfter := resp.Header.Get("Retry-After")
			failureDetail := fmt.Sprintf("图片模型请求失败（HTTP %d）", resp.StatusCode)
			trace("images-upstream-error-response", map[string]any{"requestId": requestID, "attempt": attempt, "statusCode": resp.StatusCode})
			if shouldFailOverAccount(lease, resp.StatusCode) {
				releaseAttemptFailure(lease, resp.StatusCode, retryAfter, failureDetail)
				excludeFailedAttempt(excludedAccounts, lease)
				reconnects++
				// A non-2xx response proves this account rejected the request before
				// generation, so trying a different healthy pooled account is safe.
				if policy.enabled && reconnects <= policy.maxAttempts && waitForRejectedRequestRetry(incoming.Context(), policy, "images", requestID, fmt.Sprintf("http-%d", resp.StatusCode), retryAfter, reconnects) {
					continue
				}
				writeImageGenerationHTTPError(w, resp.StatusCode)
				return
			}
			releaseAttemptFailure(lease, resp.StatusCode, retryAfter, failureDetail)
			writeImageGenerationHTTPError(w, resp.StatusCode)
			return
		}

		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxOpenAIImageGenerationResponseBytes+1))
		resp.Body.Close()
		if readErr != nil || int64(len(body)) > maxOpenAIImageGenerationResponseBytes {
			releaseAttemptSuccess(lease)
			trace("images-upstream-invalid-response", map[string]any{"requestId": requestID, "attempt": attempt, "reason": "body-too-large-or-unreadable"})
			http.Error(w, "上游图片响应无法读取或超过大小限制", http.StatusBadGateway)
			return
		}
		imageData, mimeType, err := openAIImageDataFromResponse(body)
		if err != nil {
			releaseAttemptSuccess(lease)
			trace("images-upstream-invalid-response", map[string]any{"requestId": requestID, "attempt": attempt, "reason": err.Error()})
			http.Error(w, "上游图片响应未返回可嵌入的 Base64 图片", http.StatusBadGateway)
			return
		}
		releaseAttemptSuccess(lease)
		writeDirectImageGenerationResponse(w, incoming, requestID, imageModel.ExternalModelName, mimeType, imageData)
		trace("images-upstream-success", map[string]any{"requestId": requestID, "attempt": attempt, "imageModel": imageModel.ExternalModelName})
		return
	}
}

func writeDirectImageGenerationResponse(w http.ResponseWriter, incoming *http.Request, requestID, modelName, mimeType, imageData string) {
	response := map[string]any{
		"candidates": []any{map[string]any{
			"content": map[string]any{
				"role": "model",
				"parts": []any{map[string]any{"inlineData": map[string]any{
					"mimeType": mimeType,
					"data":     imageData,
				}}},
			},
			"finishReason": "STOP",
		}},
	}
	if strings.TrimSpace(modelName) != "" {
		response["modelVersion"] = modelName
	}
	if incoming != nil && (strings.Contains(cleanPatchedPath(incoming.URL.Path), ":streamGenerateContent") || strings.EqualFold(incoming.URL.Query().Get("alt"), "sse")) {
		newDownstreamSSEWriter(w).write(encodeAntigravityStreamEvent(response, requestID))
		return
	}

	// Native image generation uses v1internal:generateContent, which is unary
	// rather than SSE. The same internal envelope is returned as JSON so the
	// Cortex generate-image handler can read inlineData from response.candidates.
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(antigravityResponseEnvelope(response, requestID))
}

func writeImageGenerationHTTPError(w http.ResponseWriter, statusCode int) {
	if statusCode < http.StatusBadRequest || statusCode > 599 {
		statusCode = http.StatusBadGateway
	}
	http.Error(w, fmt.Sprintf("上游图片模型请求失败（HTTP %d）", statusCode), statusCode)
}

func directImageGenerationPrompt(gemini map[string]any) string {
	contents, _ := gemini["contents"].([]any)
	for index := len(contents) - 1; index >= 0; index-- {
		content, _ := contents[index].(map[string]any)
		role := strings.ToLower(strings.TrimSpace(getString(content, "role")))
		if role != "" && role != "user" {
			continue
		}
		parts, _ := content["parts"].([]any)
		var text strings.Builder
		for _, rawPart := range parts {
			part, _ := rawPart.(map[string]any)
			if value, ok := part["text"].(string); ok && strings.TrimSpace(value) != "" {
				if text.Len() > 0 {
					text.WriteString("\n")
				}
				text.WriteString(strings.TrimSpace(value))
			}
		}
		if prompt := trimImageGenerationPrompt(text.String()); prompt != "" {
			return prompt
		}
	}
	return "Generate an image based on the user's current request."
}

func trimImageGenerationPrompt(value string) string {
	value = strings.TrimSpace(value)
	const maximumRunes = 4096
	runes := []rune(value)
	if len(runes) > maximumRunes {
		return strings.TrimSpace(string(runes[:maximumRunes]))
	}
	return value
}

func openAIImageDataFromResponse(body []byte) (string, string, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", "", fmt.Errorf("图片响应不是 JSON")
	}
	for _, key := range []string{"data", "images", "output", "results"} {
		items, _ := payload[key].([]any)
		for _, rawItem := range items {
			item, _ := rawItem.(map[string]any)
			if data, mimeType, ok := imageDataFromValue(item); ok {
				return data, mimeType, nil
			}
		}
	}
	if data, mimeType, ok := imageDataFromValue(payload); ok {
		return data, mimeType, nil
	}
	return "", "", fmt.Errorf("未找到 Base64 图片数据")
}

func imageDataFromValue(value map[string]any) (string, string, bool) {
	if value == nil {
		return "", "", false
	}
	mimeType := directImageMimeType(getString(value, "mime_type", "mimeType", "content_type", "contentType", "format"))
	for _, key := range []string{"b64_json", "b64Json", "base64", "image_base64", "imageBase64", "result"} {
		raw, _ := value[key].(string)
		if strings.TrimSpace(raw) == "" {
			continue
		}
		data, detectedMime, err := normaliseAttachmentData(raw, mimeType)
		if err != nil {
			return "", "", false
		}
		if detectedMime != "" {
			mimeType = directImageMimeType(detectedMime)
		}
		return data, mimeType, true
	}
	return "", "", false
}

func directImageMimeType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "image/png"
	}
	if !strings.Contains(value, "/") {
		value = "image/" + strings.TrimPrefix(value, ".")
	}
	switch value {
	case "image/png", "image/jpeg", "image/webp", "image/gif":
		return value
	case "image/jpg":
		return "image/jpeg"
	default:
		return "image/png"
	}
}
