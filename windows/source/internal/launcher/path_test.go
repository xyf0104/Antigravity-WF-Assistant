package launcher

import "testing"

func TestWindowsPathWithinRoot(t *testing.T) {
	root := `C:\Users\WF\AppData\Local\Programs\Antigravity`
	for _, path := range []string{
		`C:\Users\WF\AppData\Local\Programs\Antigravity\Antigravity.exe`,
		`c:/users/wf/appdata/local/programs/antigravity/resources/helper.exe`,
	} {
		if !windowsPathWithinRoot(path, root) {
			t.Fatalf("expected %q inside %q", path, root)
		}
	}
	if windowsPathWithinRoot(`C:\Users\WF\AppData\Local\Programs\Antigravity 2\Antigravity.exe`, root) {
		t.Fatal("sibling installation must not match")
	}
}
