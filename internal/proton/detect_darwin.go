//go:build darwin

package proton

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/selimsandal/qbt-proton-guard/internal/command"
)

var macInterfacePattern = regexp.MustCompile(`(?m)^\s*InterfaceName\s*:\s*([^\s]+)\s*$`)
var macIPv4Pattern = regexp.MustCompile(`(?m)^\s*inet\s+([0-9.]+)\s`)

func detect(ctx context.Context) (Tunnel, error) {
	status, err := command.Output(ctx, "scutil", "--nc", "status", "ProtonVPN")
	if err != nil {
		return Tunnel{}, fmt.Errorf("read ProtonVPN status: %w", err)
	}
	if firstLine(status) != "Connected" {
		return Tunnel{}, ErrDisconnected
	}
	match := macInterfacePattern.FindStringSubmatch(status)
	if len(match) != 2 || !strings.HasPrefix(match[1], "utun") {
		return Tunnel{}, fmt.Errorf("connected ProtonVPN status has no valid tunnel interface")
	}
	interfaceStatus, err := command.Output(ctx, "ifconfig", match[1])
	if err != nil {
		return Tunnel{}, fmt.Errorf("validate ProtonVPN interface %q: %w", match[1], err)
	}
	addressMatch := macIPv4Pattern.FindStringSubmatch(interfaceStatus)
	if len(addressMatch) != 2 {
		return Tunnel{}, fmt.Errorf("ProtonVPN interface %q has no IPv4 address", match[1])
	}
	return Tunnel{Interface: match[1], Name: match[1], Address: addressMatch[1]}, nil
}

func firstLine(value string) string {
	if index := strings.IndexByte(value, '\n'); index >= 0 {
		return strings.TrimSpace(value[:index])
	}
	return strings.TrimSpace(value)
}
