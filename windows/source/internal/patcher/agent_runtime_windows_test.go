//go:build windows

package patcher

import (
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestPatchedAgentLanguageServerServesUIWhenRequested starts only the isolated
// fixture process, verifies the actual HTTPS surface, and always terminates the
// exact child it created. It never attaches to an installed Agent process.
func TestPatchedAgentLanguageServerServesUIWhenRequested(t *testing.T) {
	path := os.Getenv("ANTIGRAVITY_WF_MUTABLE_TEST_AGENT_LANGUAGE_SERVER")
	if path == "" {
		t.Skip("isolated patched Agent language server fixture is not configured")
	}
	if strings.Contains(strings.ToLower(path), strings.ToLower(os.Getenv("LOCALAPPDATA")+`\Programs\Antigravity`)) {
		t.Fatal("refusing to start the installed Antigravity language server fixture")
	}
	version := os.Getenv("ANTIGRAVITY_WF_TEST_AGENT_VERSION")
	if version == "" {
		version = "2.6.0"
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	args := []string{
		"--standalone",
		"--override_ide_name", "antigravity",
		"--subclient_type", "hub",
		"--override_ide_version", version,
		"--override_user_agent_name", "antigravity",
		"--https_server_port", strconv.Itoa(port),
		"--csrf_token", "wf-isolated-runtime-validation",
		"--app_data_dir", "antigravity-wf-isolated-test",
		"--api_server_url", "https://generativelanguage.googleapis.com",
		"--cloud_code_endpoint", windowsBaseProxyEndpoint,
		"--enable_sidecars",
	}
	command := exec.Command(path, args...)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if command.Process != nil {
			_ = command.Process.Kill()
			_, _ = command.Process.Wait()
		}
	})

	client := &http.Client{
		Timeout:   2 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}, // test-only loopback fixture
	}
	url := fmt.Sprintf("https://127.0.0.1:%d/main.js", port)
	deadline := time.Now().Add(20 * time.Second)
	var body []byte
	for time.Now().Before(deadline) {
		response, requestErr := client.Get(url)
		if requestErr == nil {
			body, requestErr = io.ReadAll(response.Body)
			_ = response.Body.Close()
			if requestErr == nil && response.StatusCode == http.StatusOK {
				break
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	if !strings.Contains(string(body), agentImageGenerationUIPatchMarker) ||
		!strings.Contains(string(body), agentImageGenerationDedupePatchMarker) {
		t.Fatalf("isolated Agent HTTPS UI did not serve both patch markers (bytes=%d)", len(body))
	}
	t.Logf("isolated Agent HTTPS UI served %d bytes on loopback", len(body))
}
