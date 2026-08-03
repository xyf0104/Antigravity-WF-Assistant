// Package upstream contains the credential-safe endpoint discovery and health
// checks used by the desktop UI. It deliberately does not know about Wails so
// the same code can be used by the proxy and unit tested with httptest.
package upstream

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"antigravity-byok/internal/storage"
)

const DefaultXIASSBaseURL = "https://api.xiass.com/v1"

type Config struct {
	Provider   string            `json:"provider"`
	APIURL     string            `json:"apiUrl"`
	APIKey     string            `json:"apiKey"`
	APIStyle   string            `json:"apiStyle"`
	AuthMode   string            `json:"authMode"`
	AuthHeader string            `json:"authHeader"`
	Headers    map[string]string `json:"headers"`
}

type ModelInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type DiscoveryResult struct {
	OK         bool        `json:"ok"`
	Message    string      `json:"message"`
	Models     []ModelInfo `json:"models"`
	Endpoint   string      `json:"endpoint"`
	StatusCode int         `json:"statusCode"`
}

type TestResult struct {
	OK         bool   `json:"ok"`
	Message    string `json:"message"`
	Endpoint   string `json:"endpoint"`
	APIStyle   string `json:"apiStyle"`
	StatusCode int    `json:"statusCode"`
	ElapsedMs  int64  `json:"elapsedMs"`
}

var headerNamePattern = regexp.MustCompile(`^[!#$%&'*+\-.^_` + "`" + `|~0-9A-Za-z]+$`)

var blockedHeaderNames = map[string]struct{}{
	"host": {}, "content-length": {}, "content-type": {}, "transfer-encoding": {},
	"connection": {}, "keep-alive": {}, "proxy-authorization": {}, "proxy-authenticate": {},
	"cookie": {}, "set-cookie": {}, "accept-encoding": {},
}

func ConfigFromModel(model storage.CustomModel) Config {
	return Config{
		Provider: model.Provider, APIURL: model.APIURL, APIKey: model.APIKey,
		APIStyle: model.APIStyle, AuthMode: model.AuthMode, AuthHeader: model.AuthHeader,
		Headers: model.Headers,
	}
}

func NormalizedProvider(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "anthropic", "grok", "openai", "custom":
		return value
	default:
		return "openai"
	}
}

// EffectiveAPIStyle preserves legacy configs (which were Chat Completions)
// while letting newly-created models opt into automatic Responses detection.
func EffectiveAPIStyle(config Config) string {
	if NormalizedProvider(config.Provider) == "anthropic" {
		return "messages"
	}
	switch strings.ToLower(strings.TrimSpace(config.APIStyle)) {
	case "responses", "chat_completions", "messages", "auto":
		return strings.ToLower(strings.TrimSpace(config.APIStyle))
	default:
		return "chat_completions"
	}
}

func validateBaseURL(rawURL string) (*url.URL, error) {
	value := strings.TrimSpace(rawURL)
	if value == "" {
		value = DefaultXIASSBaseURL
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil {
		return nil, fmt.Errorf("API 地址无效")
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && isLoopbackHost(parsed.Hostname())) {
		return nil, fmt.Errorf("API 地址必须使用 HTTPS；仅本机回环地址可使用 HTTP")
	}
	return parsed, nil
}

func isLoopbackHost(host string) bool {
	host = strings.Trim(strings.ToLower(host), "[]")
	if host == "localhost" || host == "::1" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func endpointURL(rawURL, leaf string) (string, error) {
	parsed, err := validateBaseURL(rawURL)
	if err != nil {
		return "", err
	}
	path := strings.TrimRight(parsed.Path, "/")
	for _, suffix := range []string{"/chat/completions", "/responses", "/messages", "/models"} {
		if strings.HasSuffix(path, suffix) {
			path = strings.TrimSuffix(path, suffix)
			break
		}
	}
	if path == "" {
		path = "/v1"
	}
	parsed.Path = strings.TrimRight(path, "/") + "/" + strings.TrimLeft(leaf, "/")
	parsed.RawPath = ""
	return parsed.String(), nil
}

func ResolveChatCompletionsURL(rawURL string) (string, error) {
	return endpointURL(rawURL, "chat/completions")
}
func ResolveResponsesURL(rawURL string) (string, error) { return endpointURL(rawURL, "responses") }
func ResolveAnthropicMessagesURL(rawURL string) (string, error) {
	return endpointURL(rawURL, "messages")
}
func ResolveModelsURL(rawURL string) (string, error) { return endpointURL(rawURL, "models") }

func ValidateConfig(config Config) error {
	if _, err := validateBaseURL(config.APIURL); err != nil {
		return err
	}
	if strings.TrimSpace(config.APIKey) == "" {
		return fmt.Errorf("请填写 API Key 或访问令牌")
	}
	for name := range config.Headers {
		if err := validateHeaderName(name, false); err != nil {
			return err
		}
	}
	if strings.EqualFold(strings.TrimSpace(config.AuthMode), "custom_header") {
		if err := validateHeaderName(config.AuthHeader, true); err != nil {
			return err
		}
	}
	return nil
}

func validateHeaderName(name string, allowAuth bool) error {
	name = strings.TrimSpace(name)
	lower := strings.ToLower(name)
	if name == "" || !headerNamePattern.MatchString(name) {
		return fmt.Errorf("请求头名称无效")
	}
	if _, blocked := blockedHeaderNames[lower]; blocked {
		return fmt.Errorf("请求头 %s 不允许覆盖", name)
	}
	if !allowAuth && (lower == "authorization" || lower == "x-api-key") {
		return fmt.Errorf("认证请求头请使用认证方式配置")
	}
	return nil
}

func ApplyCredentials(req *http.Request, config Config) error {
	if err := ValidateConfig(config); err != nil {
		return err
	}
	for name, value := range config.Headers {
		if strings.ContainsAny(value, "\r\n") {
			return fmt.Errorf("请求头值无效")
		}
		req.Header.Set(strings.TrimSpace(name), strings.TrimSpace(value))
	}
	mode := strings.ToLower(strings.TrimSpace(config.AuthMode))
	if mode == "" {
		if NormalizedProvider(config.Provider) == "anthropic" {
			mode = "x_api_key"
		} else {
			mode = "bearer"
		}
	}
	switch mode {
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(config.APIKey))
	case "x_api_key":
		req.Header.Set("x-api-key", strings.TrimSpace(config.APIKey))
	case "custom_header":
		req.Header.Set(strings.TrimSpace(config.AuthHeader), strings.TrimSpace(config.APIKey))
	default:
		return fmt.Errorf("不支持的认证方式")
	}
	if NormalizedProvider(config.Provider) == "anthropic" {
		req.Header.Set("anthropic-version", "2023-06-01")
	}
	return nil
}

func DiscoverModels(ctx context.Context, config Config) DiscoveryResult {
	if err := ValidateConfig(config); err != nil {
		return DiscoveryResult{Message: err.Error()}
	}
	endpoint, err := ResolveModelsURL(config.APIURL)
	if err != nil {
		return DiscoveryResult{Message: err.Error()}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return DiscoveryResult{Message: "无法创建模型列表请求", Endpoint: endpoint}
	}
	request.Header.Set("Accept", "application/json")
	if err := ApplyCredentials(request, config); err != nil {
		return DiscoveryResult{Message: err.Error(), Endpoint: endpoint}
	}
	client := &http.Client{Timeout: 30 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return DiscoveryResult{Message: "无法连接上游：" + safeNetworkError(err), Endpoint: endpoint}
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return DiscoveryResult{Message: statusMessage(response.StatusCode, body), Endpoint: endpoint, StatusCode: response.StatusCode}
	}
	models, err := ParseModels(body)
	if err != nil {
		return DiscoveryResult{Message: "上游返回的模型列表无法识别", Endpoint: endpoint, StatusCode: response.StatusCode}
	}
	if len(models) == 0 {
		return DiscoveryResult{Message: "上游未返回可添加的模型", Endpoint: endpoint, StatusCode: response.StatusCode}
	}
	return DiscoveryResult{OK: true, Message: fmt.Sprintf("已发现 %d 个可用模型", len(models)), Models: models, Endpoint: endpoint, StatusCode: response.StatusCode}
}

func ParseModels(body []byte) ([]ModelInfo, error) {
	var decoded any
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, err
	}
	seen := map[string]ModelInfo{}
	collectModelList(decoded, seen, 0)
	result := make([]ModelInfo, 0, len(seen))
	for _, model := range seen {
		result = append(result, model)
	}
	sort.Slice(result, func(i, j int) bool { return strings.ToLower(result[i].ID) < strings.ToLower(result[j].ID) })
	return result, nil
}

func collectModelList(value any, seen map[string]ModelInfo, depth int) {
	if depth > 5 {
		return
	}
	switch current := value.(type) {
	case []any:
		for _, item := range current {
			collectModelList(item, seen, depth+1)
		}
	case map[string]any:
		if id := modelID(current); id != "" {
			seen[id] = ModelInfo{ID: id, Name: modelDisplayName(current, id)}
		}
		for _, key := range []string{"data", "models", "available_models", "availableModels", "result", "response"} {
			if child, ok := current[key]; ok {
				collectModelList(child, seen, depth+1)
			}
		}
	case string:
		id := strings.TrimSpace(strings.TrimPrefix(current, "models/"))
		if id != "" && !strings.ContainsAny(id, " \t\r\n") {
			seen[id] = ModelInfo{ID: id, Name: id}
		}
	}
}

func modelID(value map[string]any) string {
	for _, field := range []string{"id", "model", "model_id", "modelId", "name"} {
		candidate, _ := value[field].(string)
		candidate = strings.TrimSpace(strings.TrimPrefix(candidate, "models/"))
		if candidate != "" {
			return candidate
		}
	}
	return ""
}

func modelDisplayName(value map[string]any, fallback string) string {
	for _, field := range []string{"display_name", "displayName", "name", "id", "model"} {
		if candidate, ok := value[field].(string); ok && strings.TrimSpace(candidate) != "" {
			return strings.TrimSpace(strings.TrimPrefix(candidate, "models/"))
		}
	}
	return fallback
}

// TestModel sends a tiny non-streaming request only after the user explicitly
// presses the test action. It never logs credentials or response bodies.
func TestModel(ctx context.Context, config Config, model string) TestResult {
	started := time.Now()
	finish := func(result TestResult) TestResult {
		result.ElapsedMs = time.Since(started).Milliseconds()
		return result
	}
	if err := ValidateConfig(config); err != nil {
		return finish(TestResult{Message: err.Error()})
	}
	model = strings.TrimSpace(strings.TrimPrefix(model, "models/"))
	if model == "" {
		return finish(TestResult{Message: "请选择需要测试的模型"})
	}
	style := EffectiveAPIStyle(config)
	if style == "auto" {
		result := testResponses(ctx, config, model)
		if result.OK || !CanFallbackToChat(result.StatusCode) {
			return finish(result)
		}
		return finish(testChatCompletions(ctx, config, model))
	}
	switch style {
	case "messages":
		return finish(testAnthropic(ctx, config, model))
	case "responses":
		return finish(testResponses(ctx, config, model))
	default:
		return finish(testChatCompletions(ctx, config, model))
	}
}

// CanFallbackToChat identifies only endpoint-availability errors. Callers must
// not hide an authentication, quota, capability, or validation error by
// retrying it on a different API surface.
func CanFallbackToChat(statusCode int) bool {
	return statusCode == http.StatusNotFound || statusCode == http.StatusMethodNotAllowed || statusCode == http.StatusNotImplemented
}

func testChatCompletions(ctx context.Context, config Config, model string) TestResult {
	endpoint, err := ResolveChatCompletionsURL(config.APIURL)
	if err != nil {
		return TestResult{Message: err.Error(), APIStyle: "chat_completions"}
	}
	body, _ := json.Marshal(map[string]any{
		"model": model, "stream": false, "max_tokens": 8,
		"messages": []any{map[string]any{"role": "user", "content": "Reply with OK."}},
	})
	return doTestRequest(ctx, config, endpoint, "chat_completions", body)
}

func testResponses(ctx context.Context, config Config, model string) TestResult {
	endpoint, err := ResolveResponsesURL(config.APIURL)
	if err != nil {
		return TestResult{Message: err.Error(), APIStyle: "responses"}
	}
	body, _ := json.Marshal(map[string]any{"model": model, "input": "Reply with OK.", "max_output_tokens": 8})
	return doTestRequest(ctx, config, endpoint, "responses", body)
}

func testAnthropic(ctx context.Context, config Config, model string) TestResult {
	endpoint, err := ResolveAnthropicMessagesURL(config.APIURL)
	if err != nil {
		return TestResult{Message: err.Error(), APIStyle: "messages"}
	}
	body, _ := json.Marshal(map[string]any{
		"model": model, "max_tokens": 8, "messages": []any{map[string]any{"role": "user", "content": "Reply with OK."}},
	})
	return doTestRequest(ctx, config, endpoint, "messages", body)
}

func doTestRequest(ctx context.Context, config Config, endpoint, style string, body []byte) TestResult {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return TestResult{Message: "无法创建测试请求", Endpoint: endpoint, APIStyle: style}
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	if err := ApplyCredentials(request, config); err != nil {
		return TestResult{Message: err.Error(), Endpoint: endpoint, APIStyle: style}
	}
	response, err := (&http.Client{Timeout: 45 * time.Second}).Do(request)
	if err != nil {
		return TestResult{Message: "无法连接上游：" + safeNetworkError(err), Endpoint: endpoint, APIStyle: style}
	}
	defer response.Body.Close()
	responseBody, _ := io.ReadAll(io.LimitReader(response.Body, 512<<10))
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return TestResult{Message: statusMessage(response.StatusCode, responseBody), Endpoint: endpoint, APIStyle: style, StatusCode: response.StatusCode}
	}
	return TestResult{OK: true, Message: fmt.Sprintf("模型可用（HTTP %d）", response.StatusCode), Endpoint: endpoint, APIStyle: style, StatusCode: response.StatusCode}
}

func safeNetworkError(err error) string {
	if errors, ok := err.(interface{ Timeout() bool }); ok && errors.Timeout() {
		return "请求超时"
	}
	return "连接失败"
}

func statusMessage(statusCode int, body []byte) string {
	var decoded map[string]any
	if json.Unmarshal(body, &decoded) == nil {
		if errorValue, ok := decoded["error"].(map[string]any); ok {
			if message, ok := errorValue["message"].(string); ok && strings.TrimSpace(message) != "" {
				return fmt.Sprintf("上游返回 HTTP %d：%s", statusCode, strings.TrimSpace(message))
			}
		}
		if message, ok := decoded["message"].(string); ok && strings.TrimSpace(message) != "" {
			return fmt.Sprintf("上游返回 HTTP %d：%s", statusCode, strings.TrimSpace(message))
		}
	}
	return fmt.Sprintf("上游返回 HTTP %d", statusCode)
}
