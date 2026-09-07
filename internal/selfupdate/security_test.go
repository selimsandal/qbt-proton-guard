package selfupdate

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func testSigningKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	previous := releasePublicKeyPEM
	releasePublicKeyPEM = string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
	t.Cleanup(func() { releasePublicKeyPEM = previous })
	return private
}

func TestSignatureRejectsTamperingAndRelabeling(t *testing.T) {
	key := testSigningKey(t)
	const tag = "v0.0.100-gabcdef0"
	sums := []byte("authentic checksum manifest")
	signature := ed25519.Sign(key, append([]byte("qbt-proton-guard-release\n"+tag+"\n"), sums...))
	if err := verifyRelease(tag, sums, signature); err != nil {
		t.Fatal(err)
	}
	_, attacker, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, tag string
		sums, sig []byte
	}{
		{"changed manifest", tag, []byte("attacker checksum"), signature},
		{"relabelled old release", "v0.0.200-gabcdef1", sums, signature},
		{"unsigned", tag, sums, nil},
		{"forged signature", tag, sums, ed25519.Sign(attacker, append([]byte("qbt-proton-guard-release\n"+tag+"\n"), sums...))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := verifyRelease(tc.tag, tc.sums, tc.sig); err == nil {
				t.Fatal("untrusted release accepted")
			}
		})
	}
}

func TestGitHubURLAndRedirectPolicy(t *testing.T) {
	for _, raw := range []string{
		"http://github.com/release", "https://github.com.evil.example/release", "https://github.com@evil.example/release",
		"https://evil.example@github.com/release", "https://127.0.0.1/release", "https://github.com:8443/release",
	} {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if err := trustedURL(u); err == nil {
			t.Errorf("accepted %s", raw)
		}
		if err := updateClient().CheckRedirect(&http.Request{URL: u}, nil); err == nil {
			t.Errorf("redirect accepted %s", raw)
		}
	}
	for _, host := range []string{"github.com", "api.github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com"} {
		u, _ := url.Parse("https://" + host + "/asset")
		if err := trustedURL(u); err != nil {
			t.Error(err)
		}
	}
}

func TestUntrustedTLSCertificateRejected(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()
	// Do not use server.Client(), which would deliberately trust its test CA.
	if response, err := updateClient().Get(server.URL); err == nil {
		response.Body.Close()
		t.Fatal("untrusted TLS certificate accepted")
	}
}
