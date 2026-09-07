//go:build darwin

package guard

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestStatusLaunchPlistLoginBehavior(t *testing.T) {
	on := statusLaunchPlist("/Applications/Guard", "/tmp", true)
	if !strings.Contains(on, "<key>RunAtLoad</key><true/>") ||
		!strings.Contains(on, "<key>KeepAlive</key><dict><key>SuccessfulExit</key><false/></dict>") {
		t.Fatalf("login-on plist does not request launch and failure restart:\n%s", on)
	}
	off := statusLaunchPlist("/Applications/Guard", "/tmp", false)
	if !strings.Contains(off, "<key>RunAtLoad</key><false/>") {
		t.Fatalf("login-off plist does not disable RunAtLoad:\n%s", off)
	}
	if strings.Contains(off, "<key>KeepAlive</key>") {
		t.Fatalf("login-off plist has KeepAlive and could implicitly launch after quit:\n%s", off)
	}
}

func TestStatusLaunchPlistEscapesPaths(t *testing.T) {
	plist := statusLaunchPlist("/A&B/<Guard>", "/Log & Data", false)
	if strings.Contains(plist, "/A&B/<Guard>") || !strings.Contains(plist, "/A&amp;B/&lt;Guard&gt;") {
		t.Fatalf("binary path was not XML escaped:\n%s", plist)
	}
}

type fakeDarwinExecutor struct {
	loaded, running map[string]bool
	calls           [][]string
	fail            func(name string, args []string) bool
	notRunning      string
}

func (f *fakeDarwinExecutor) command(name string, args ...string) *exec.Cmd {
	f.calls = append(f.calls, append([]string{name}, args...))
	if f.fail != nil && f.fail(name, args) {
		return exec.Command("/bin/sh", "-c", "printf 'injected failure'; exit 1")
	}
	if name == "swiftc" || name == "iconutil" {
		for i := range args {
			if args[i] == "-o" && i+1 < len(args) {
				_ = os.MkdirAll(filepath.Dir(args[i+1]), 0o755)
				_ = os.WriteFile(args[i+1], []byte("fake output"), 0o755)
			}
		}
	}
	if name == "sips" {
		for i := range args {
			if args[i] == "--out" && i+1 < len(args) {
				_ = os.WriteFile(args[i+1], []byte("fake png"), 0o644)
			}
		}
	}
	if name == "plutil" && len(args) > 0 && args[0] == "-extract" {
		b, _ := os.ReadFile(args[len(args)-1])
		value := "true"
		if strings.Contains(string(b), "<key>RunAtLoad</key><false/>") {
			value = "false"
		}
		return exec.Command("/usr/bin/printf", "%s\n", value)
	}
	if name == "launchctl" && len(args) >= 2 {
		switch args[0] {
		case "print":
			label := filepath.Base(args[1])
			if !f.loaded[label] {
				return exec.Command("/bin/sh", "-c", "exit 113")
			}
			state := "waiting"
			if f.running[label] && f.notRunning != label {
				state = "running"
			}
			return exec.Command("/usr/bin/printf", "state = %s\n", state)
		case "bootout":
			label := filepath.Base(args[1])
			delete(f.loaded, label)
			delete(f.running, label)
		case "bootstrap":
			label := strings.TrimSuffix(filepath.Base(args[len(args)-1]), ".plist")
			plist, _ := os.ReadFile(args[len(args)-1])
			f.loaded[label] = true
			f.running[label] = !strings.Contains(string(plist), "<key>RunAtLoad</key><false/>")
		case "kickstart":
			label := filepath.Base(args[1])
			f.running[label] = true
		}
	}
	return exec.Command("/usr/bin/true")
}

type darwinInstallFixture struct {
	home                         string
	binary, app, service, status string
	fake                         *fakeDarwinExecutor
	old                          map[string][]byte
}

func newDarwinInstallFixture(t *testing.T, login bool) *darwinInstallFixture {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	x := &darwinInstallFixture{
		home:    home,
		binary:  filepath.Join(home, ".local", "bin", "qbt-proton-guard"),
		app:     filepath.Join(home, "Applications", "qbt-proton-guard.app"),
		service: filepath.Join(home, "Library", "LaunchAgents", serviceLabel+".plist"),
		status:  filepath.Join(home, "Library", "LaunchAgents", statusLabel+".plist"),
		fake:    &fakeDarwinExecutor{loaded: map[string]bool{serviceLabel: true, statusLabel: true}, running: map[string]bool{serviceLabel: true, statusLabel: true}},
		old:     map[string][]byte{},
	}
	seed := map[string]string{x.binary: "old binary", filepath.Join(x.app, "old-marker"): "old app", x.service: "old service plist"}
	seed[x.status] = statusLaunchPlist("/old/status", "/old/logs", login)
	for path, value := range seed {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
			t.Fatal(err)
		}
		x.old[path] = []byte(value)
	}
	oldCommand, oldEnforce, oldSleep := installerCommand, installerEnforce, installerSleep
	installerCommand = x.fake.command
	installerEnforce = func(context.Context) (string, error) { return "fake", nil }
	installerSleep = func(_ time.Duration) {}
	t.Cleanup(func() { installerCommand, installerEnforce, installerSleep = oldCommand, oldEnforce, oldSleep })
	return x
}

func (x *darwinInstallFixture) assertOld(t *testing.T) {
	t.Helper()
	for path, want := range x.old {
		got, err := os.ReadFile(path)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("%s not restored: got %q, err %v", path, got, err)
		}
	}
}

func (x *darwinInstallFixture) assertJobs(t *testing.T) {
	t.Helper()
	for _, label := range []string{serviceLabel, statusLabel} {
		if !x.fake.loaded[label] || !x.fake.running[label] {
			t.Errorf("old job %s not restored: loaded=%v running=%v", label, x.fake.loaded[label], x.fake.running[label])
		}
	}
}

func TestDarwinInstallPreflightFailuresLeaveOldInstallationRunning(t *testing.T) {
	for _, failure := range []string{"swiftc", "plutil"} {
		t.Run(failure, func(t *testing.T) {
			x := newDarwinInstallFixture(t, true)
			x.fake.fail = func(name string, args []string) bool {
				return name == failure && (name != "plutil" || len(args) > 0 && args[0] == "-lint")
			}
			if err := Install(); err == nil {
				t.Fatal("Install succeeded despite injected preflight failure")
			}
			x.assertOld(t)
			x.assertJobs(t)
			for _, call := range x.fake.calls {
				if len(call) > 1 && call[0] == "launchctl" && call[1] == "bootout" {
					t.Fatalf("preflight failure stopped a job: %v", call)
				}
			}
		})
	}
}

func TestDarwinInstallSecondStopFailureRestartsFirstJob(t *testing.T) {
	x := newDarwinInstallFixture(t, true)
	x.fake.fail = func(name string, args []string) bool {
		return name == "launchctl" && len(args) > 1 && args[0] == "bootout" && filepath.Base(args[1]) == statusLabel
	}
	err := Install()
	if err == nil || !strings.Contains(err.Error(), "stop LaunchAgent "+statusLabel) {
		t.Fatalf("unexpected error: %v", err)
	}
	x.assertOld(t)
	x.assertJobs(t)
}

func TestDarwinInstallStandaloneStopFailureRestoresServices(t *testing.T) {
	x := newDarwinInstallFixture(t, true)
	x.fake.fail = func(name string, args []string) bool {
		return len(args) > 0 && args[0] == "--stop-running"
	}
	if err := Install(); err == nil || !strings.Contains(err.Error(), "stop standalone status app") {
		t.Fatalf("unexpected error: %v", err)
	}
	x.assertOld(t)
	x.assertJobs(t)
}

func TestDarwinInstallStatusBootstrapFailureRollsBackDisabledRunningStatus(t *testing.T) {
	x := newDarwinInstallFixture(t, false)
	failed := false
	x.fake.fail = func(name string, args []string) bool {
		match := !failed && name == "launchctl" && len(args) > 0 && args[0] == "bootstrap" && strings.HasSuffix(args[len(args)-1], statusLabel+".plist")
		failed = failed || match
		return match
	}
	err := Install()
	if err == nil || !strings.Contains(err.Error(), "start "+statusLabel) {
		t.Fatalf("unexpected error: %v", err)
	}
	x.assertOld(t)
	x.assertJobs(t)
}

func TestDarwinInstallStartupVerificationFailureRollsBack(t *testing.T) {
	x := newDarwinInstallFixture(t, true)
	x.fake.notRunning = serviceLabel
	err := Install()
	if err == nil || !strings.Contains(err.Error(), serviceLabel+" did not remain running") {
		t.Fatalf("unexpected error: %v", err)
	}
	x.fake.notRunning = ""
	x.assertOld(t)
	x.assertJobs(t)
}

func TestDarwinInstallSuccessfulCommitRemovesBackupsAndPreservesDisabledLogin(t *testing.T) {
	x := newDarwinInstallFixture(t, false)
	if err := Install(); err != nil {
		t.Fatalf("Install: %v", err)
	}
	b, err := os.ReadFile(x.status)
	if err != nil || !strings.Contains(string(b), "<key>RunAtLoad</key><false/>") {
		t.Fatalf("disabled login preference not preserved: %v\n%s", err, b)
	}
	for _, dir := range []string{filepath.Dir(x.binary), filepath.Dir(x.app), filepath.Dir(x.service)} {
		matches, err := filepath.Glob(filepath.Join(dir, ".*.upgrade-backup-*"))
		if err != nil || len(matches) != 0 {
			t.Errorf("upgrade backups remain in %s: %v (%v)", dir, matches, err)
		}
	}
	if _, err := os.Stat(filepath.Join(x.app, "old-marker")); !os.IsNotExist(err) {
		t.Errorf("old app survived successful replacement: %v", err)
	}
	x.assertJobs(t)
}
