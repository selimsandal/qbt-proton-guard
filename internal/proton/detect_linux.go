//go:build linux

package proton

import (
	"context"
	"fmt"
	"strings"

	"github.com/selimsandal/qbt-proton-guard/internal/command"
)

func detect(ctx context.Context) (Tunnel, error) {
	output, err := command.Output(ctx, "nmcli", "-t", "--escape", "no", "-f", "NAME,TYPE,DEVICE,STATE", "connection", "show", "--active")
	if err != nil {
		return Tunnel{}, fmt.Errorf("query NetworkManager: %w", err)
	}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Split(line, ":")
		if len(fields) != 4 || !strings.HasPrefix(fields[0], "ProtonVPN ") {
			continue
		}
		if (fields[1] == "wireguard" || fields[1] == "vpn") && fields[2] != "" && fields[3] == "activated" {
			addressOutput, err := command.Output(ctx, "ip", "-o", "-4", "address", "show", "dev", fields[2], "scope", "global")
			if err != nil {
				return Tunnel{}, fmt.Errorf("query ProtonVPN interface address: %w", err)
			}
			addressFields := strings.Fields(addressOutput)
			for index, field := range addressFields {
				if field == "inet" && index+1 < len(addressFields) {
					address := strings.SplitN(addressFields[index+1], "/", 2)[0]
					return Tunnel{Interface: fields[2], Name: fields[2], Address: address}, nil
				}
			}
			return Tunnel{}, fmt.Errorf("ProtonVPN interface %q has no IPv4 address", fields[2])
		}
	}
	return Tunnel{}, ErrDisconnected
}
