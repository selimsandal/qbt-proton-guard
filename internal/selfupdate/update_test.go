package selfupdate

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func testClient(body func(string) string) *http.Client {
	return &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body(r.URL.String()))), Header: make(http.Header)}, nil
	})}
}

func TestNewer(t *testing.T) {
	for _, tc := range []struct {
		candidate, current string
		want               bool
	}{
		{"0.0.200-gabcdef1", "0.0.100-gabcdef0", true},
		{"0.0.100-gabcdef0", "0.0.200-gabcdef1", false},
		{"0.0.100-gabcdef0", "0.0.100-gabcdef0", false},
		{"0.0.100-gabcdef1", "0.0.100-gabcdef0", true},
		{"0.0.100-gabcdef0", "dev", true},
		{"0.0.100-gabcdef0", "0.4.0", true},
		{"bad", "dev", false},
	} {
		if got := newer(tc.candidate, tc.current); got != tc.want {
			t.Errorf("newer(%q,%q)=%v", tc.candidate, tc.current, got)
		}
	}
}

func TestLatestRejectsInvalidReleases(t *testing.T) {
	for _, data := range []string{`{}`, `{"tag_name":"v0.4.0"}`, `{"tag_name":"v0.0.100-gabcdef0","draft":true}`, `{"tag_name":"v0.0.100-gabcdef0","prerelease":true}`, `invalid`} {
		if _, err := latest(context.Background(), testClient(func(string) string { return data })); err == nil {
			t.Fatalf("accepted %s", data)
		}
	}
	if _, err := latest(context.Background(), testClient(func(string) string { return `{"tag_name":"v0.0.100-gabcdef0"}` })); err != nil {
		t.Fatal(err)
	}
}

func TestVerifiedDownload(t *testing.T) {
	key := testSigningKey(t)
	const name = "qbt-proton-guard-darwin-arm64"
	const tag = "v0.0.100-gabcdef0"
	base := repository + "/releases/download/" + tag + "/"
	r, err := latest(context.Background(), testClient(func(string) string {
		return fmt.Sprintf(`{"tag_name":%q,"assets":[{"name":%q,"browser_download_url":%q},{"name":"SHA256SUMS","browser_download_url":%q},{"name":"SHA256SUMS.sig","browser_download_url":%q}]}`, tag, name, base+name, base+"SHA256SUMS", base+"SHA256SUMS.sig")
	}))
	if err != nil {
		t.Fatal(err)
	}
	data := "downloaded executable"
	valid := fmt.Sprintf("%x  %s\n", sha256.Sum256([]byte(data)), name)
	for _, tc := range []struct {
		name, sums string
		ok         bool
	}{
		{"valid", valid, true},
		{"forged signature", valid, false},
		{"missing signature", valid, false},
		{"mismatch", fmt.Sprintf("%064d  %s\n", 0, name), false},
		{"missing", "", false},
		{"duplicate", valid + valid, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), name)
			client := testClient(func(url string) string {
				if strings.HasSuffix(url, "/SHA256SUMS.sig") {
					if tc.name == "forged signature" {
						return string(make([]byte, ed25519.SignatureSize))
					}
					if tc.name == "missing signature" {
						return ""
					}
					return string(ed25519.Sign(key, []byte("qbt-proton-guard-release\n"+tag+"\n"+tc.sums)))
				}
				if strings.HasSuffix(url, "/SHA256SUMS") {
					return tc.sums
				}
				return data
			})
			err := download(context.Background(), client, r, name, path)
			if (err == nil) != tc.ok {
				t.Fatalf("download: %v", err)
			}
			b, readErr := os.ReadFile(path)
			if tc.ok && (readErr != nil || string(b) != data) {
				t.Fatalf("wrong file: %q, %v", b, readErr)
			}
			if !tc.ok && !os.IsNotExist(readErr) {
				t.Fatal("unverified executable written")
			}
		})
	}
	r.Assets[0].URL = "https://example.com/evil"
	if _, err := assetURL(r, name); err == nil {
		t.Fatal("accepted untrusted asset URL")
	}
	if _, err := assetURL(r, "unsupported-platform"); err == nil {
		t.Fatal("accepted missing platform")
	}
}

func TestDownloadResponseLimitsAndErrors(t *testing.T) {
	if _, err := get(context.Background(), testClient(func(string) string { return "12345" }), latestURL, 4); err == nil {
		t.Fatal("oversized response accepted")
	}
	client := &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader("not found"))}, nil
	})}
	if _, err := latest(context.Background(), client); err == nil {
		t.Fatal("HTTP failure accepted")
	}
}

func TestCheckOnlyAndCurrentVersionDoNotDownload(t *testing.T) {
	for _, key := range []string{"HOME", "XDG_CACHE_HOME", "LOCALAPPDATA"} {
		t.Setenv(key, t.TempDir())
	}
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	calls := 0
	http.DefaultTransport = transport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.String() != latestURL {
			t.Fatalf("unexpected download: %s", r.URL)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"tag_name":"v0.0.100-gabcdef0"}`))}, nil
	})
	if err := Run(context.Background(), "dev", true); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), "0.0.100-gabcdef0", false); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("got %d requests", calls)
	}
}
