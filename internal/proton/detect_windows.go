//go:build windows

package proton

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/selimsandal/qbt-proton-guard/internal/command"
)

const windowsAdapterScript = `$ErrorActionPreference='Stop'; $a=Get-NetAdapter -IncludeHidden | Where-Object { $_.Status -eq 'Up' -and ($_.InterfaceGuid -in @('{EAB2262D-9AB1-5975-7D92-334D06F4972B}','{AC128890-BDB1-CE5C-D1DB-EFB01DE370B2}') -or $_.Name -eq 'ProtonVPN TUN' -or $_.InterfaceDescription -like '*TAP-ProtonVPN Windows Adapter V9*') } | Where-Object { $i=$_.ifIndex; (Get-NetIPAddress -InterfaceIndex $i -AddressFamily IPv4 -ErrorAction SilentlyContinue) -and (Get-NetRoute -InterfaceIndex $i -AddressFamily IPv4 -ErrorAction SilentlyContinue | Where-Object { $_.DestinationPrefix -in @('0.0.0.0/0','0.0.0.0/1','128.0.0.0/1') }) } | Select-Object -First 1 Name,@{N='InterfaceGuid';E={$_.InterfaceGuid.ToString()}},ifIndex,@{N='IPAddress';E={(Get-NetIPAddress -InterfaceIndex $_.ifIndex -AddressFamily IPv4 | Select-Object -First 1).IPAddress}}; $a | ConvertTo-Json -Compress`

type windowsAdapter struct {
	Name          string `json:"Name"`
	InterfaceGUID string `json:"InterfaceGuid"`
	Index         int    `json:"ifIndex"`
	IPAddress     string `json:"IPAddress"`
}

func detect(ctx context.Context) (Tunnel, error) {
	output, err := command.Output(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", windowsAdapterScript)
	if err != nil {
		return Tunnel{}, fmt.Errorf("query ProtonVPN network adapter: %w", err)
	}
	if strings.TrimSpace(output) == "" {
		return Tunnel{}, ErrDisconnected
	}
	var adapter windowsAdapter
	if err := json.Unmarshal([]byte(output), &adapter); err != nil {
		return Tunnel{}, fmt.Errorf("decode ProtonVPN network adapter: %w", err)
	}
	if adapter.Name == "" || adapter.InterfaceGUID == "" || adapter.Index <= 0 || adapter.IPAddress == "" {
		return Tunnel{}, fmt.Errorf("ProtonVPN network adapter response is incomplete")
	}
	return Tunnel{Interface: "{" + strings.Trim(adapter.InterfaceGUID, "{}") + "}", Name: adapter.Name, Address: adapter.IPAddress}, nil
}
