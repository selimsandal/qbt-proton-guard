//go:build linux

package guard

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type linuxInstallerFake struct {
	active, enabled map[string]bool
	calls           []string
	failStop        string
	failStatusStart bool
	statusStarted   bool
}

func (f *linuxInstallerFake) command(name string, args ...string) *exec.Cmd {
	call := strings.Join(append([]string{name}, args...), " ")
	f.calls = append(f.calls, call)
	if name != "systemctl" || len(args) < 3 {
		return exec.Command("/usr/bin/true")
	}
	action, unit := args[1], args[len(args)-1]
	switch action {
	case "is-active":
		if f.active[unit] && !(f.failStatusStart && f.statusStarted && unit == statusServiceName) {
			return exec.Command("/usr/bin/true")
		}
		return exec.Command("/usr/bin/false")
	case "is-enabled":
		if args[2] == "--quiet" {
			if f.enabled[unit] {
				return exec.Command("/usr/bin/true")
			}
			return exec.Command("/usr/bin/false")
		}
		if f.enabled[unit] {
			return exec.Command("/usr/bin/printf", "enabled\n")
		}
		return exec.Command("/usr/bin/printf", "disabled\n")
	case "stop":
		if unit == f.failStop {
			return exec.Command("/usr/bin/false")
		}
		f.active[unit] = false
	case "start":
		f.active[unit] = true
		if unit == statusServiceName {
			f.statusStarted = true
		}
	case "enable":
		f.enabled[unit] = true
	case "disable":
		f.enabled[unit] = false
	}
	return exec.Command("/usr/bin/true")
}

type linuxFixture struct {
	home string
	fake *linuxInstallerFake
	old  map[string][]byte
}

func newLinuxFixture(t *testing.T, statusEnabled, statusRunning bool) *linuxFixture {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	f := &linuxFixture{home: home, fake: &linuxInstallerFake{
		active:  map[string]bool{serviceName: true, statusServiceName: statusRunning},
		enabled: map[string]bool{serviceName: true, statusServiceName: statusEnabled},
	}, old: map[string][]byte{}}
	for path, text := range map[string]string{
		filepath.Join(home, ".local/bin/qbt-proton-guard"):                                  "old binary",
		filepath.Join(home, ".config/systemd/user", serviceName):                            "old guard unit",
		filepath.Join(home, ".config/systemd/user", statusServiceName):                      "old status unit",
		filepath.Join(home, ".local/share/icons/hicolor/256x256/apps/qbt-proton-guard.png"): "old icon",
		filepath.Join(home, ".local/share/applications/qbt-proton-guard.desktop"):           "old desktop",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o755); err != nil {
			t.Fatal(err)
		}
		f.old[path] = []byte(text)
	}
	oldCommand := installerCommand
	installerCommand = f.fake.command
	t.Cleanup(func() { installerCommand = oldCommand })
	return f
}

func (f *linuxFixture) assertOld(t *testing.T) {
	t.Helper()
	for path, want := range f.old {
		got, err := os.ReadFile(path)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("%s not restored: %q (%v)", path, got, err)
		}
	}
}

func TestLinuxInstallSecondStopFailureRestartsGuardWithoutChangingFiles(t *testing.T) {
	f := newLinuxFixture(t, true, true)
	f.fake.failStop = statusServiceName
	err := Install()
	if err == nil || !strings.Contains(err.Error(), "stop "+statusServiceName) {
		t.Fatalf("Install error = %v", err)
	}
	f.assertOld(t)
	if !f.fake.active[serviceName] || !f.fake.active[statusServiceName] {
		t.Fatalf("old active states not restored: %#v", f.fake.active)
	}
}

func TestLinuxStatusStartupFailureStopsReplacementBeforeRollback(t *testing.T) {
	f := newLinuxFixture(t, true, true)
	f.fake.failStatusStart = true
	err := Install()
	if err == nil || !strings.Contains(err.Error(), "status icon did not become active") {
		t.Fatalf("Install error = %v", err)
	}
	f.assertOld(t)
	joined := strings.Join(f.fake.calls, "\n")
	if strings.Count(joined, "systemctl --user stop "+serviceName) < 2 {
		t.Fatalf("replacement guard was not stopped before rollback:\n%s", joined)
	}
	if !f.fake.enabled[serviceName] || !f.fake.enabled[statusServiceName] || !f.fake.active[serviceName] || !f.fake.active[statusServiceName] {
		t.Fatalf("old enabled/running states not restored: enabled=%v active=%v", f.fake.enabled, f.fake.active)
	}
}

func TestLinuxInstallPreservesDisabledStatusLoginAndGuardRestartPolicy(t *testing.T) {
	f := newLinuxFixture(t, false, false)
	if err := Install(); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if f.fake.enabled[statusServiceName] {
		t.Fatal("disabled status login was enabled")
	}
	if f.fake.active[statusServiceName] {
		t.Fatal("disabled, previously stopped status was started")
	}
	unit, err := os.ReadFile(filepath.Join(f.home, ".config/systemd/user", serviceName))
	if err != nil || !strings.Contains(string(unit), "Restart=always") {
		t.Fatalf("guard Restart policy changed: %v\n%s", err, unit)
	}
}

func TestLinuxInstallPreflightFailureDoesNotStopServices(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "missing"))
	old := installerCommand
	var calls int
	installerCommand = func(string, ...string) *exec.Cmd { calls++; return exec.Command("/usr/bin/true") }
	t.Cleanup(func() { installerCommand = old })
	if err := Install(); err == nil {
		t.Fatal("Install succeeded with invalid HOME")
	}
	if calls != 0 {
		t.Fatalf("preflight invoked service manager %d times", calls)
	}
}
