package qbittorrent

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

const DisabledInterface = "qbt-proton-guard-disabled"

type Binding struct {
	Interface string
	Name      string
	Address   string
}

type Safety struct {
	Binding
	Port                         uint16
	LocalPeerDiscoveryDisabled   bool
	RouterPortForwardingDisabled bool
}

func FindConfig() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find home directory: %w", err)
	}
	var candidates []string
	switch runtime.GOOS {
	case "darwin":
		candidates = []string{
			filepath.Join(home, ".config", "qBittorrent", "qBittorrent.ini"),
			filepath.Join(home, ".config", "qBittorrent", "qBittorrent.conf"),
			filepath.Join(home, "Library", "Preferences", "qBittorrent", "qBittorrent.ini"),
		}
	case "windows":
		if appData := os.Getenv("APPDATA"); appData != "" {
			candidates = []string{
				filepath.Join(appData, "qBittorrent", "qBittorrent.ini"),
				filepath.Join(appData, "qBittorrent", "qBittorrent.conf"),
			}
		}
	default:
		configHome := os.Getenv("XDG_CONFIG_HOME")
		if configHome == "" {
			configHome = filepath.Join(home, ".config")
		}
		candidates = []string{
			filepath.Join(configHome, "qBittorrent", "qBittorrent.ini"),
			filepath.Join(configHome, "qBittorrent", "qBittorrent.conf"),
		}
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("qBittorrent config not found; start and quit qBittorrent once first")
}

func ReadBinding(path string) (Binding, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Binding{}, err
	}
	return parseBinding(string(data)), nil
}

func WriteBinding(path string, binding Binding) error {
	if binding.Interface == "" || binding.Name == "" || strings.ContainsAny(binding.Interface+binding.Name, "\r\n=") {
		return fmt.Errorf("invalid binding %+v", binding)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read qBittorrent config: %w", err)
	}
	updated := patchBinding(string(data), binding)
	return replaceContents(path, string(data), updated)
}

func ReadSafety(path string) (Safety, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Safety{}, err
	}
	result := Safety{Binding: parseBinding(string(data))}
	section := ""
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		switch {
		case section == "BitTorrent" && key == `Session\Port`:
			if port, err := strconv.ParseUint(value, 10, 16); err == nil {
				result.Port = uint16(port)
			}
		case section == "BitTorrent" && key == `Session\LSDEnabled`:
			result.LocalPeerDiscoveryDisabled = strings.EqualFold(value, "false")
		case section == "Network" && key == `PortForwardingEnabled`:
			result.RouterPortForwardingDisabled = strings.EqualFold(value, "false")
		}
	}
	return result, scanner.Err()
}

func WriteSafety(path string, desired Safety) error {
	if desired.Interface == "" || desired.Name == "" || strings.ContainsAny(desired.Interface+desired.Name, "\r\n=") {
		return fmt.Errorf("invalid binding %+v", desired.Binding)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read qBittorrent config: %w", err)
	}
	updated := patchBinding(string(data), desired.Binding)
	updated = patchSetting(updated, "BitTorrent", `Session\LSDEnabled`, "false")
	updated = patchSetting(updated, "Network", `PortForwardingEnabled`, "false")
	if desired.Port != 0 {
		updated = patchSetting(updated, "BitTorrent", `Session\Port`, strconv.FormatUint(uint64(desired.Port), 10))
	}
	return replaceContents(path, string(data), updated)
}

func SafetyMatches(actual, expected Safety) bool {
	portMatches := expected.Port == 0 || actual.Port == expected.Port
	return BindingMatches(actual.Binding, expected.Binding) && portMatches && actual.LocalPeerDiscoveryDisabled && actual.RouterPortForwardingDisabled
}

func replaceContents(path, original, updated string) error {
	if updated == original {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat qBittorrent config: %w", err)
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".qBittorrent.ini.guard-*")
	if err != nil {
		return fmt.Errorf("create temporary config: %w", err)
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(info.Mode().Perm()); err != nil {
		temp.Close()
		return fmt.Errorf("set temporary config permissions: %w", err)
	}
	if _, err := temp.WriteString(updated); err != nil {
		temp.Close()
		return fmt.Errorf("write temporary config: %w", err)
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return fmt.Errorf("sync temporary config: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close temporary config: %w", err)
	}
	if err := replaceFile(tempName, path); err != nil {
		return fmt.Errorf("replace qBittorrent config: %w", err)
	}
	return nil
}

func parseBinding(data string) Binding {
	var result Binding
	section := ""
	scanner := bufio.NewScanner(strings.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}
		if section != "BitTorrent" {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		switch strings.TrimSpace(key) {
		case `Session\Interface`:
			result.Interface = strings.TrimSpace(value)
		case `Session\InterfaceName`:
			result.Name = strings.TrimSpace(value)
		case `Session\InterfaceAddress`:
			result.Address = strings.TrimSpace(value)
		}
	}
	return result
}

func patchBinding(data string, binding Binding) string {
	newline := "\n"
	if strings.Contains(data, "\r\n") {
		newline = "\r\n"
	}
	normalized := strings.ReplaceAll(data, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	sectionStart, sectionEnd := -1, len(lines)
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			if sectionStart >= 0 {
				sectionEnd = index
				break
			}
			if trimmed == "[BitTorrent]" {
				sectionStart = index
			}
		}
	}
	settings := map[string]string{
		`Session\Interface`:        binding.Interface,
		`Session\InterfaceName`:    binding.Name,
		`Session\InterfaceAddress`: binding.Address,
	}
	if sectionStart < 0 {
		if len(lines) > 0 && lines[len(lines)-1] != "" {
			lines = append(lines, "")
		}
		lines = append(lines, "[BitTorrent]")
		for _, key := range []string{`Session\Interface`, `Session\InterfaceAddress`, `Session\InterfaceName`} {
			lines = append(lines, key+"="+settings[key])
		}
		return strings.Join(lines, newline)
	}

	found := make(map[string]bool)
	for index := sectionStart + 1; index < sectionEnd; index++ {
		key, _, ok := strings.Cut(strings.TrimSpace(lines[index]), "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if value, managed := settings[key]; managed {
			lines[index] = key + "=" + value
			found[key] = true
		}
	}
	var missing []string
	for _, key := range []string{`Session\Interface`, `Session\InterfaceAddress`, `Session\InterfaceName`} {
		if !found[key] {
			missing = append(missing, key+"="+settings[key])
		}
	}
	lines = append(lines[:sectionEnd], append(missing, lines[sectionEnd:]...)...)
	return strings.Join(lines, newline)
}

func patchSetting(data, wantedSection, wantedKey, wantedValue string) string {
	newline := "\n"
	if strings.Contains(data, "\r\n") {
		newline = "\r\n"
	}
	lines := strings.Split(strings.ReplaceAll(data, "\r\n", "\n"), "\n")
	sectionStart, sectionEnd := -1, len(lines)
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "[") || !strings.HasSuffix(trimmed, "]") {
			continue
		}
		if sectionStart >= 0 {
			sectionEnd = index
			break
		}
		if strings.TrimSpace(trimmed[1:len(trimmed)-1]) == wantedSection {
			sectionStart = index
		}
	}
	if sectionStart < 0 {
		if len(lines) > 0 && lines[len(lines)-1] != "" {
			lines = append(lines, "")
		}
		lines = append(lines, "["+wantedSection+"]", wantedKey+"="+wantedValue)
		return strings.Join(lines, newline)
	}
	for index := sectionStart + 1; index < sectionEnd; index++ {
		key, _, found := strings.Cut(strings.TrimSpace(lines[index]), "=")
		if found && strings.TrimSpace(key) == wantedKey {
			lines[index] = wantedKey + "=" + wantedValue
			return strings.Join(lines, newline)
		}
	}
	lines = append(lines[:sectionEnd], append([]string{wantedKey + "=" + wantedValue}, lines[sectionEnd:]...)...)
	return strings.Join(lines, newline)
}

func BindingMatches(actual, expected Binding) bool {
	return actual.Interface == expected.Interface && actual.Name == expected.Name && actual.Address == expected.Address
}

var errProcessTimeout = errors.New("timed out waiting for qBittorrent to stop")
