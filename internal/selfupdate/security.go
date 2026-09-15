package selfupdate

import (
	"crypto/ed25519"
	"crypto/x509"
	_ "embed"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// The app ships its trust anchor instead of downloading it.
//
//go:embed release-public-key.pem
var releasePublicKeyPEM string

func verifyRelease(tag string, sums, signature []byte) error {
	block, _ := pem.Decode([]byte(releasePublicKeyPEM))
	if block == nil {
		return fmt.Errorf("invalid embedded release key")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return fmt.Errorf("invalid embedded release key: %w", err)
	}
	key, ok := parsed.(ed25519.PublicKey)
	if !ok {
		return fmt.Errorf("release key is not Ed25519")
	}
	// Bind checksums to the release identity and reject relabeled old binaries.
	payload := append([]byte("qbt-proton-guard-release\n"+tag+"\n"), sums...)
	if !ed25519.Verify(key, payload, signature) {
		return fmt.Errorf("release signature verification failed; refusing untrusted update")
	}
	return nil
}

func trustedURL(u *url.URL) error {
	if u.Scheme != "https" || u.User != nil || (u.Port() != "" && u.Port() != "443") {
		return fmt.Errorf("update requires an authenticated HTTPS GitHub URL")
	}
	switch u.Hostname() {
	case "api.github.com", "github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com":
		return nil
	}
	return fmt.Errorf("untrusted update host %q", u.Hostname())
}

func updateClient() *http.Client {
	// Go's default transport validates certificate chains and hostnames.
	// Keep TLS verification enabled for configured HTTPS proxies.
	return &http.Client{Timeout: 2 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("too many update redirects")
		}
		return trustedURL(req.URL)
	}}
}
