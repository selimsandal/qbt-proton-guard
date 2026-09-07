package portforward

import (
	"encoding/binary"
	"strings"
	"testing"
)

func TestParseResponse(t *testing.T) {
	packet := make([]byte, 16)
	packet[1] = 129
	binary.BigEndian.PutUint16(packet[8:10], 4242)
	binary.BigEndian.PutUint16(packet[10:12], 54321)
	binary.BigEndian.PutUint32(packet[12:16], 7200)
	got, err := parseResponse(packet, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.InternalPort != 4242 || got.ExternalPort != 54321 || got.Lifetime != 7200 {
		t.Fatalf("unexpected response: %+v", got)
	}
}

func TestParseResponseError(t *testing.T) {
	packet := make([]byte, 16)
	packet[1] = 130
	binary.BigEndian.PutUint16(packet[2:4], 2)
	_, err := parseResponse(packet, 2)
	if err == nil || !strings.Contains(err.Error(), "not authorized") {
		t.Fatalf("unexpected error: %v", err)
	}
}
