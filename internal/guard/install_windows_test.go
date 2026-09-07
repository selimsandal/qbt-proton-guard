//go:build windows

package guard

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestWindowsTaskEnabledReadsSettingsNotTrigger(t *testing.T) {
	for _, tc := range []struct {
		xml  string
		want bool
	}{
		{`<Task><Triggers><LogonTrigger><Enabled>false</Enabled></LogonTrigger></Triggers><Settings><Enabled>true</Enabled></Settings></Task>`, true},
		{`<Task><Triggers><LogonTrigger><Enabled>true</Enabled></LogonTrigger></Triggers><Settings><Enabled>false</Enabled></Settings></Task>`, false},
	} {
		got, err := windowsTaskEnabled([]byte(tc.xml))
		if err != nil || got != tc.want {
			t.Errorf("windowsTaskEnabled = %v, %v; want %v", got, err, tc.want)
		}
	}
}

func TestExportWindowsTaskNormalizesUnicodeUTF8(t *testing.T) {
	old := installerCommand
	installerCommand = func(string, ...string) *exec.Cmd {
		return portableWindowsTestCommand(exec.Command("/usr/bin/printf", "\357\273\277<?xml version=\"1.0\" encoding=\"UTF-16\"?><Task><Name>状況</Name></Task>"))
	}
	t.Cleanup(func() { installerCommand = old })
	got, err := exportWindowsTask(statusTaskName)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.HasPrefix(got, []byte{0xef, 0xbb, 0xbf}) || strings.Contains(string(got), "UTF-16") || !strings.Contains(string(got), "状況") {
		t.Fatalf("export was not normalized UTF-8: %q", got)
	}
}

type windowsInstallerFake struct {
	definitions      map[string][]byte
	running, enabled map[string]bool
	calls            []string
	failReplacement  string
	failed           bool
}

func (f *windowsInstallerFake) command(name string, args ...string) *exec.Cmd {
	return portableWindowsTestCommand(f.commandResult(name, args...))
}

func (f *windowsInstallerFake) commandResult(name string, args ...string) *exec.Cmd {
	f.calls = append(f.calls, strings.Join(append([]string{name}, args...), " "))
	if name == "powershell.exe" {
		script := args[len(args)-1]
		var task string
		if strings.Contains(script, statusTaskName) {
			task = statusTaskName
		} else {
			task = taskName
		}
		if strings.Contains(script, "Export-ScheduledTask") {
			return exec.Command("/usr/bin/printf", "%s", string(f.definitions[task]))
		}
		if strings.Contains(script, ".State") {
			if f.running[task] {
				return exec.Command("/usr/bin/printf", "1\n")
			}
			return exec.Command("/usr/bin/printf", "0\n")
		}
		if strings.Contains(script, "Get-ScheduledTask") {
			if _, ok := f.definitions[task]; ok {
				return exec.Command("/usr/bin/printf", "1\n")
			}
		}
		return exec.Command("/usr/bin/printf", "0\n")
	}
	if name != "schtasks.exe" {
		return exec.Command("/usr/bin/true")
	}
	task := ""
	for i := range args {
		if args[i] == "/TN" && i+1 < len(args) {
			task = args[i+1]
		}
	}
	switch args[0] {
	case "/End":
		f.running[task] = false
	case "/Run":
		if !f.enabled[task] {
			return exec.Command("/usr/bin/false")
		}
		f.running[task] = true
	case "/Change":
		f.enabled[task] = args[len(args)-1] == "/ENABLE"
	case "/Delete":
		delete(f.definitions, task)
		delete(f.enabled, task)
	case "/Create":
		if task == f.failReplacement && !f.failed {
			f.failed = true
			return exec.Command("/usr/bin/false")
		}
		data, err := os.ReadFile(args[len(args)-1])
		if err != nil {
			return exec.Command("/usr/bin/false")
		}
		f.definitions[task] = append([]byte(nil), data...)
		on, err := windowsTaskEnabled(data)
		if err == nil {
			f.enabled[task] = on
		}
	}
	return exec.Command("/usr/bin/true")
}

// Run fake commands through the test executable, including on Windows hosts.
func portableWindowsTestCommand(result *exec.Cmd) *exec.Cmd {
	code, output := 0, ""
	if result.Path == "/usr/bin/false" {
		code = 1
	}
	if result.Path == "/usr/bin/printf" {
		values := make([]any, len(result.Args)-2)
		for i, value := range result.Args[2:] {
			values[i] = value
		}
		output = fmt.Sprintf(result.Args[1], values...)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestWindowsInstallerHelper$")
	cmd.Env = append(os.Environ(), "QBT_INSTALLER_TEST_HELPER=1", "QBT_INSTALLER_TEST_OUTPUT="+output, "QBT_INSTALLER_TEST_EXIT="+strconv.Itoa(code))
	return cmd
}

func TestWindowsInstallerHelper(t *testing.T) {
	if os.Getenv("QBT_INSTALLER_TEST_HELPER") != "1" {
		return
	}
	fmt.Print(os.Getenv("QBT_INSTALLER_TEST_OUTPUT"))
	code, _ := strconv.Atoi(os.Getenv("QBT_INSTALLER_TEST_EXIT"))
	os.Exit(code)
}

type windowsFixture struct {
	dir                string
	fake               *windowsInstallerFake
	oldBinary, oldIcon []byte
}

func newWindowsFixture(t *testing.T, statusEnabled, statusRunning bool) *windowsFixture {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("LOCALAPPDATA", dir)
	w := &windowsFixture{dir: dir, oldBinary: []byte("old binary"), oldIcon: []byte("old icon")}
	w.fake = &windowsInstallerFake{
		definitions: map[string][]byte{taskName: []byte(windowsTaskXML("old-guard", "run", true)), statusTaskName: []byte(windowsTaskXML("old-status-状況", "ui", statusEnabled))},
		running:     map[string]bool{taskName: true, statusTaskName: statusRunning}, enabled: map[string]bool{taskName: true, statusTaskName: statusEnabled},
	}
	installDir := filepath.Join(dir, "qbt-proton-guard")
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installDir, "qbt-proton-guard.exe"), w.oldBinary, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installDir, "qbt-proton-guard.ico"), w.oldIcon, 0o644); err != nil {
		t.Fatal(err)
	}
	old := installerCommand
	installerCommand = w.fake.command
	t.Cleanup(func() { installerCommand = old })
	return w
}

func TestWindowsTaskRecreationFailureRestoresFilesAndDefinitions(t *testing.T) {
	w := newWindowsFixture(t, true, true)
	oldDefs := map[string][]byte{taskName: append([]byte(nil), w.fake.definitions[taskName]...), statusTaskName: append([]byte(nil), w.fake.definitions[statusTaskName]...)}
	w.fake.failReplacement = statusTaskName
	if err := Install(); err == nil || !strings.Contains(err.Error(), "create task "+statusTaskName) {
		t.Fatalf("Install error = %v", err)
	}
	for path, want := range map[string][]byte{filepath.Join(w.dir, "qbt-proton-guard/qbt-proton-guard.exe"): w.oldBinary, filepath.Join(w.dir, "qbt-proton-guard/qbt-proton-guard.ico"): w.oldIcon} {
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("%s not restored: %q (%v)", path, got, err)
		}
	}
	for name, want := range oldDefs {
		if !bytes.Equal(w.fake.definitions[name], want) {
			t.Errorf("task %s definition not restored", name)
		}
	}
}

func TestWindowsDisabledRunningStatusStartsTemporarilyEnabledThenDisables(t *testing.T) {
	w := newWindowsFixture(t, false, true)
	if err := Install(); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if w.fake.enabled[statusTaskName] || !w.fake.running[statusTaskName] {
		t.Fatalf("status state = enabled %v, running %v", w.fake.enabled[statusTaskName], w.fake.running[statusTaskName])
	}
	joined := strings.Join(w.fake.calls, "\n")
	enable := strings.Index(joined, "/Change /TN "+statusTaskName+" /ENABLE")
	run := strings.Index(joined, "/Run /TN "+statusTaskName)
	disable := strings.LastIndex(joined, "/Change /TN "+statusTaskName+" /DISABLE")
	if enable < 0 || run < enable || disable < run {
		t.Fatalf("temporary enable/run/disable sequence missing:\n%s", joined)
	}
}

func TestWindowsInstallPreflightFailureDoesNotStopTasks(t *testing.T) {
	t.Setenv("LOCALAPPDATA", filepath.Join(t.TempDir(), "missing"))
	old := installerCommand
	var calls []string
	installerCommand = func(name string, args ...string) *exec.Cmd {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return exec.Command("/usr/bin/true")
	}
	t.Cleanup(func() { installerCommand = old })
	if err := Install(); err == nil {
		t.Fatal("Install succeeded with invalid LOCALAPPDATA")
	}
	for _, call := range calls {
		if strings.Contains(call, "/End") {
			t.Fatalf("preflight stopped a task: %s", call)
		}
	}
}
