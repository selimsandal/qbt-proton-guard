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

func TestMacOSMenuBarGroupsAndAlignsStatus(t *testing.T) {
	source := string(MacOSMenuBarSource)
	for _, expected := range []string{
		"addSection(\"Status\")",
		"addSection(\"Actions\")",
		"addSection(\"Updates\")",
		"addStatusRow(label: \"Last checked\"",
		"private func addUpdateStatus(_ value: String, showsProgress: Bool)",
		"valueField.alignment = .right",
		"valueField.lineBreakMode = .byTruncatingTail",
		"progress.style = .spinning",
		"preferencesItem.submenu = preferences",
	} {
		if !strings.Contains(source, expected) {
			t.Errorf("menu bar source does not contain %q", expected)
		}
	}
}
