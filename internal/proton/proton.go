package proton

import (
	"context"
	"errors"
	"net"
)

var ErrDisconnected = errors.New("not connected")

type Tunnel struct {
	Interface string
	Name      string
	Address   string
}

func Detect(ctx context.Context) (Tunnel, error) {
	tunnel, err := detect(ctx)
	if err != nil {
		return Tunnel{}, err
	}
	if tunnel.Interface == "" {
		return Tunnel{}, ErrDisconnected
	}
	if tunnel.Name == "" {
		tunnel.Name = tunnel.Interface
	}
	if net.ParseIP(tunnel.Address) == nil {
		return Tunnel{}, errors.New("Proton tunnel has no valid IP address")
	}
	return tunnel, nil
}
