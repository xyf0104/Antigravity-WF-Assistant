package claudeconfig

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"regexp"
	"strings"
	"unicode"
)

var (
	errInvalidBaseURL  = errors.New("invalid Claude API root")
	errInvalidAuth     = errors.New("invalid Claude authorization token")
	errInvalidModel    = errors.New("invalid Claude model identifier")
	errInvalidSettings = errors.New("invalid Claude user settings JSON")
	errUnsafePath      = errors.New("unsafe Claude settings path")
)

var modelPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@+\-\[\]]{0,255}$`)

type normalizedApplyConfig struct {
	baseURL   string
	authToken string
	model     string
}

type settingsDocument struct {
	root       map[string]json.RawMessage
	env        map[string]json.RawMessage
	envPresent bool
}

// NormalizeBaseURL accepts an explicit HTTPS API root or an HTTP root bound to
// localhost/loopback only. It intentionally does not append a path such as
// /v1: an API root is caller-owned and guessing endpoint semantics can route
// requests incorrectly.
func NormalizeBaseURL(value string) (string, error) {
	if value == "" || len(value) > maxBaseURLBytes || strings.TrimSpace(value) != value || containsControl(value) {
		return "", errInvalidBaseURL
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed == nil || parsed.Scheme == "" || parsed.Host == "" || parsed.Opaque != "" || parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.RawPath != "" {
		return "", errInvalidBaseURL
	}
	if containsControl(parsed.Hostname()) || containsControl(parsed.Path) || parsed.Hostname() == "" {
		return "", errInvalidBaseURL
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "https" && !(scheme == "http" && isLoopbackHost(parsed.Hostname())) {
		return "", errInvalidBaseURL
	}
	parsed.Scheme = scheme
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	parsed.Fragment = ""
	return parsed.String(), nil
}

func isLoopbackHost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "localhost" {
		return true
	}
	parsed := net.ParseIP(host)
	return parsed != nil && parsed.IsLoopback()
}

func normalizeApplyConfig(input ApplyConfig) (normalizedApplyConfig, error) {
	baseURL, err := NormalizeBaseURL(input.BaseURL)
	if err != nil {
		return normalizedApplyConfig{}, err
	}
	if input.AuthToken == "" || len(input.AuthToken) > maxAuthTokenBytes || strings.TrimSpace(input.AuthToken) != input.AuthToken || containsControl(input.AuthToken) || containsWhitespace(input.AuthToken) || strings.HasPrefix(strings.ToLower(input.AuthToken), "bearer ") {
		return normalizedApplyConfig{}, errInvalidAuth
	}
	if input.Model == "" || len(input.Model) > maxModelBytes || !modelPattern.MatchString(input.Model) {
		return normalizedApplyConfig{}, errInvalidModel
	}
	return normalizedApplyConfig{baseURL: baseURL, authToken: input.AuthToken, model: input.Model}, nil
}

func containsControl(value string) bool {
	for _, character := range value {
		if unicode.IsControl(character) {
			return true
		}
	}
	return false
}

func containsWhitespace(value string) bool {
	for _, character := range value {
		if unicode.IsSpace(character) {
			return true
		}
	}
	return false
}

func parseSettings(data []byte) (settingsDocument, error) {
	if len(data) == 0 || len(data) > maxSettingsBytes {
		return settingsDocument{}, errInvalidSettings
	}
	root, err := decodeJSONObject(data)
	if err != nil {
		return settingsDocument{}, errInvalidSettings
	}
	document := settingsDocument{root: root, env: make(map[string]json.RawMessage)}
	if rawEnv, ok := root["env"]; ok {
		env, err := decodeJSONObject(rawEnv)
		if err != nil {
			return settingsDocument{}, errInvalidSettings
		}
		document.env = env
		document.envPresent = true
	}
	return document, nil
}

func decodeJSONObject(data []byte) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	delimiter, ok := token.(json.Delim)
	if !ok || delimiter != '{' {
		return nil, errors.New("JSON value is not an object")
	}
	object := make(map[string]json.RawMessage)
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		key, ok := keyToken.(string)
		if !ok {
			return nil, errors.New("JSON object key is not a string")
		}
		if _, exists := object[key]; exists {
			return nil, errors.New("duplicate JSON object key")
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		object[key] = append(json.RawMessage(nil), value...)
	}
	closing, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	closingDelimiter, ok := closing.(json.Delim)
	if !ok || closingDelimiter != '}' {
		return nil, errors.New("JSON object did not close")
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, errors.New("trailing JSON value")
		}
		return nil, err
	}
	return object, nil
}

func (document settingsDocument) updated(config normalizedApplyConfig) ([]byte, error) {
	baseURL, err := json.Marshal(config.baseURL)
	if err != nil {
		return nil, errors.New("encode Claude API root")
	}
	authToken, err := json.Marshal(config.authToken)
	if err != nil {
		return nil, errors.New("encode Claude authorization token")
	}
	model, err := json.Marshal(config.model)
	if err != nil {
		return nil, errors.New("encode Claude model")
	}
	document.env["ANTHROPIC_BASE_URL"] = baseURL
	document.env["ANTHROPIC_AUTH_TOKEN"] = authToken
	env, err := json.Marshal(document.env)
	if err != nil {
		return nil, errors.New("encode Claude environment settings")
	}
	document.root["env"] = env
	document.root["model"] = model
	updated, err := json.MarshalIndent(document.root, "", "  ")
	if err != nil {
		return nil, errors.New("encode Claude user settings")
	}
	return append(updated, '\n'), nil
}

func verifyManagedSettings(data []byte, config normalizedApplyConfig) error {
	document, err := parseSettings(data)
	if err != nil {
		return err
	}
	baseURL, baseURLOK := stringValue(document.env["ANTHROPIC_BASE_URL"])
	authToken, authTokenOK := stringValue(document.env["ANTHROPIC_AUTH_TOKEN"])
	model, modelOK := stringValue(document.root["model"])
	if !baseURLOK || !authTokenOK || !modelOK || baseURL != config.baseURL || authToken != config.authToken || model != config.model {
		return errors.New("managed settings verification failed")
	}
	return nil
}

func stringValue(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false
	}
	return value, true
}

func snapshotFromDocument(document settingsDocument) (model, baseURL string, authConfigured, managed bool) {
	if rawModel, ok := stringValue(document.root["model"]); ok && modelPattern.MatchString(rawModel) {
		model = rawModel
	} else if rawModel != "" {
		model = "configured"
	}
	if rawBaseURL, ok := stringValue(document.env["ANTHROPIC_BASE_URL"]); ok && rawBaseURL != "" {
		if normalized, err := NormalizeBaseURL(rawBaseURL); err == nil {
			baseURL = normalized
		} else {
			baseURL = "configured"
		}
	}
	if rawToken, ok := stringValue(document.env["ANTHROPIC_AUTH_TOKEN"]); ok && rawToken != "" {
		authConfigured = true
	}
	_, hasBaseURL := document.env["ANTHROPIC_BASE_URL"]
	_, hasToken := document.env["ANTHROPIC_AUTH_TOKEN"]
	_, hasModel := document.root["model"]
	managed = hasBaseURL && hasToken && hasModel
	return model, baseURL, authConfigured, managed
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
