package portforward

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/selimsandal/qbt-proton-guard/internal/proton"
)

const (
	protonGateway = "10.2.0.1"
	protonNATPMP  = 5351
	leaseDuration = 7200 * time.Second
)

type Manager struct {
	mu        sync.Mutex
	tunnelKey string
	port      uint16
	internal  uint16
	renewAt   time.Time
	expiresAt time.Time
	nextTry   time.Time
	lastErr   error
}

func (manager *Manager) Forward(ctx context.Context, tunnel proton.Tunnel) (uint16, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()

	key := tunnel.Interface + "\x00" + tunnel.Address
	now := time.Now()
	if key != manager.tunnelKey {
		manager.tunnelKey = key
		manager.port = 0
		manager.internal = 0
		manager.renewAt = time.Time{}
		manager.nextTry = time.Time{}
		manager.lastErr = nil
	}
	if manager.port != 0 && now.Before(manager.renewAt) {
		return manager.port, nil
	}
	if manager.port != 0 && !now.Before(manager.expiresAt) {
		manager.port = 0
		manager.internal = 0
	}
	if now.Before(manager.nextTry) {
		return manager.port, manager.lastErr
	}

	internal, requested := manager.internal, manager.port
	udp, err := request(ctx, tunnel.Address, 1, internal, requested, uint32(leaseDuration/time.Second))
	if err != nil {
		manager.nextTry = now.Add(15 * time.Second)
		manager.lastErr = err
		return manager.port, err
	}
	tcp, err := request(ctx, tunnel.Address, 2, udp.InternalPort, udp.ExternalPort, uint32(leaseDuration/time.Second))
	if err != nil {
		manager.nextTry = now.Add(15 * time.Second)
		manager.lastErr = err
		return manager.port, err
	}
	if udp.ExternalPort == 0 || tcp.ExternalPort != udp.ExternalPort {
		err := fmt.Errorf("Proton returned different UDP and TCP ports (%d and %d)", udp.ExternalPort, tcp.ExternalPort)
		manager.nextTry = now.Add(15 * time.Second)
		manager.lastErr = err
		return manager.port, err
	}

	manager.port = udp.ExternalPort
	manager.internal = udp.InternalPort
	lifetime := time.Duration(min(udp.Lifetime, tcp.Lifetime)) * time.Second
	if lifetime <= 0 {
		return 0, errors.New("Proton returned an empty NAT-PMP lease")
	}
	manager.renewAt = now.Add(lifetime * 3 / 4)
	manager.expiresAt = now.Add(lifetime)
	manager.nextTry = time.Time{}
	manager.lastErr = nil
	return manager.port, nil
}

func (manager *Manager) Reset() {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	manager.tunnelKey = ""
	manager.port = 0
	manager.internal = 0
	manager.renewAt = time.Time{}
	manager.expiresAt = time.Time{}
	manager.nextTry = time.Time{}
	manager.lastErr = nil
}

func (manager *Manager) Status() (uint16, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	return manager.port, manager.lastErr
}

type response struct {
	InternalPort uint16
	ExternalPort uint16
	Lifetime     uint32
}

func request(ctx context.Context, localAddress string, protocol byte, internalPort, externalPort uint16, lifetime uint32) (response, error) {
	localIP := net.ParseIP(localAddress)
	if localIP == nil {
		return response{}, fmt.Errorf("invalid tunnel address %q", localAddress)
	}
	connection, err := net.ListenUDP("udp4", &net.UDPAddr{IP: localIP})
	if err != nil {
		return response{}, fmt.Errorf("bind NAT-PMP client to %s: %w", localAddress, err)
	}
	defer connection.Close()

	packet := make([]byte, 12)
	packet[1] = protocol
	binary.BigEndian.PutUint16(packet[4:6], internalPort)
	binary.BigEndian.PutUint16(packet[6:8], externalPort)
	binary.BigEndian.PutUint32(packet[8:12], lifetime)
	destination := &net.UDPAddr{IP: net.ParseIP(protonGateway), Port: protonNATPMP}
	delays := []time.Duration{250 * time.Millisecond, 500 * time.Millisecond, time.Second}
	buffer := make([]byte, 32)
	for _, delay := range delays {
		if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < delay {
			delay = time.Until(deadline)
		}
		if delay <= 0 {
			return response{}, ctx.Err()
		}
		if _, err := connection.WriteToUDP(packet, destination); err != nil {
			return response{}, fmt.Errorf("send NAT-PMP request: %w", err)
		}
		_ = connection.SetReadDeadline(time.Now().Add(delay))
		length, sender, err := connection.ReadFromUDP(buffer)
		if err != nil {
			if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
				continue
			}
			return response{}, fmt.Errorf("receive NAT-PMP response: %w", err)
		}
		if !sender.IP.Equal(destination.IP) {
			continue
		}
		return parseResponse(buffer[:length], protocol)
	}
	return response{}, errors.New("Proton NAT-PMP gateway did not respond; enable Port Forwarding and connect to a P2P server")
}

func parseResponse(packet []byte, protocol byte) (response, error) {
	if len(packet) < 16 {
		return response{}, fmt.Errorf("short NAT-PMP response (%d bytes)", len(packet))
	}
	if packet[0] != 0 || packet[1] != protocol+128 {
		return response{}, fmt.Errorf("unexpected NAT-PMP response version/opcode %d/%d", packet[0], packet[1])
	}
	if code := binary.BigEndian.Uint16(packet[2:4]); code != 0 {
		messages := map[uint16]string{1: "unsupported version", 2: "not authorized", 3: "network failure", 4: "out of resources", 5: "unsupported opcode"}
		message := messages[code]
		if message == "" {
			message = "unknown error"
		}
		return response{}, fmt.Errorf("Proton NAT-PMP error %d: %s", code, message)
	}
	return response{
		InternalPort: binary.BigEndian.Uint16(packet[8:10]),
		ExternalPort: binary.BigEndian.Uint16(packet[10:12]),
		Lifetime:     binary.BigEndian.Uint32(packet[12:16]),
	}, nil
}
