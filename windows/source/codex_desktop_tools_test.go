package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"antigravity-byok/internal/codexdesktop"
)

func TestCodexDesktopRendererDTOAndFailureMessageDoNotExposeLocalDetails(t *testing.T) {
	status := codexDesktopStatusForRenderer(codexdesktop.ControlStatus{
		Installation: codexdesktop.Installation{
			Present:            true,
			Source:             codexdesktop.SourceManualSelection,
			Version:            "26.825.32147",
			ExecutableVerified: true,
		},
		Discovered: true,
		Selected:   true,
		CanLaunch:  true,
	}, false, codexDesktopMessageForError(errors.New(`C:\Users\alice\AppData\Local\ChatGPT.exe --pid 1234 token=secret`), "launch"))
	encoded, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("marshal renderer DTO: %v", err)
	}
	for _, forbidden := range []string{`C:\Users\alice`, "ChatGPT.exe", "1234", "secret", "token="} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("renderer DTO leaked %q: %s", forbidden, encoded)
		}
	}
	if strings.Contains(string(encoded), `"executable":`) || strings.Contains(string(encoded), `"path":`) || strings.Contains(string(encoded), `"pid":`) {
		t.Fatalf("renderer DTO contains forbidden control field: %s", encoded)
	}
}
