package qbittorrent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPatchBindingExistingSection(t *testing.T) {
	input := "[BitTorrent]\nSession\\Interface=en0\nSession\\InterfaceAddress=192.0.2.1\nSession\\Port=1234\n\n[GUI]\nEnabled=true\n"
	target := Binding{Interface: "utun5", Name: "utun5", Address: "10.2.0.2"}
	got := patchBinding(input, target)
	wantLines := []string{
		`Session\Interface=utun5`,
		`Session\InterfaceAddress=10.2.0.2`,
		`Session\InterfaceName=utun5`,
		`Session\Port=1234`,
		`[GUI]`,
	}
	for _, want := range wantLines {
		if !strings.Contains(got, want) {
			t.Errorf("patched config missing %q:\n%s", want, got)
		}
	}
	if !BindingMatches(parseBinding(got), target) {
		t.Fatalf("binding did not match: %+v", parseBinding(got))
	}
}

func TestPatchBindingAddsSectionAndPreservesCRLF(t *testing.T) {
	target := Binding{Interface: DisabledInterface, Name: DisabledInterface}
	got := patchBinding("[GUI]\r\nEnabled=true\r\n", target)
	if strings.Contains(strings.ReplaceAll(got, "\r\n", ""), "\n") {
		t.Fatalf("introduced bare newline: %q", got)
	}
	if !BindingMatches(parseBinding(got), target) {
		t.Fatalf("binding did not match: %+v", parseBinding(got))
	}
}

func TestWriteBindingAtomically(t *testing.T) {
	path := filepath.Join(t.TempDir(), "qBittorrent.ini")
	if err := os.WriteFile(path, []byte("[BitTorrent]\nSession\\Port=1234\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := Binding{Interface: "proton0", Name: "Proton VPN", Address: "10.2.0.2"}
	if err := WriteBinding(path, target); err != nil {
		t.Fatal(err)
	}
	binding, err := ReadBinding(path)
	if err != nil {
		t.Fatal(err)
	}
	if !BindingMatches(binding, target) {
		t.Fatalf("binding did not match: %+v", binding)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 600", info.Mode().Perm())
	}
}

func TestWriteSafety(t *testing.T) {
	path := filepath.Join(t.TempDir(), "qBittorrent.ini")
	input := "[BitTorrent]\r\nSession\\Port=1111\r\n\r\n[Network]\r\nPortForwardingEnabled=true\r\n"
	if err := os.WriteFile(path, []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}
	desired := Safety{
		Binding:                      Binding{Interface: "utun5", Name: "utun5", Address: "10.2.0.2"},
		Port:                         54321,
		LocalPeerDiscoveryDisabled:   true,
		RouterPortForwardingDisabled: true,
	}
	if err := WriteSafety(path, desired); err != nil {
		t.Fatal(err)
	}
	actual, err := ReadSafety(path)
	if err != nil {
		t.Fatal(err)
	}
	if !SafetyMatches(actual, desired) {
		t.Fatalf("safety settings did not match: %+v", actual)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ReplaceAll(string(data), "\r\n", ""), "\n") {
		t.Fatalf("introduced bare newline: %q", data)
	}
}
