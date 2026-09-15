package assets

import (
	"strings"
	"testing"
)

func TestMacOSMenuBarCleansUpOnLaunchdTermination(t *testing.T) {
	source := string(MacOSMenuBarSource)
	for _, expected := range []string{
		"signal(SIGTERM, SIG_IGN)",
		"DispatchSource.makeSignalSource(signal: SIGTERM, queue: .main)",
		"func applicationWillTerminate(_ notification: Notification)",
		"NSStatusBar.system.removeStatusItem(statusItem)",
	} {
		if !strings.Contains(source, expected) {
			t.Errorf("menu bar source does not contain %q", expected)
		}
	}
}
