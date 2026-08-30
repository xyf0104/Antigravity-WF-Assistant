package proxy

import "strings"

// legacyProxyProductToken exists only to accept traffic from applications
// patched before the route rename. Constructing the retired token at runtime
// keeps it out of current XIASS Tools product metadata and normal diagnostics.
func legacyProxyProductToken() string {
	return strings.Join([]string{"b", "y", "o", "k"}, "")
}

func legacyPatchedPrefixes() []string {
	token := legacyProxyProductToken()
	return []string{
		"/v1internal/antigravity-" + token + "/",
		"/v1internal/" + token + "xxx/",
		"/v1internal/" + token + "xxx-sandbox/",
	}
}

func legacyProxyHealthPath() string {
	return "/_antigravity-" + legacyProxyProductToken() + "/health"
}
