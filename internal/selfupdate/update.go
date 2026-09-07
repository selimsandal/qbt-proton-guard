// Package selfupdate downloads official GitHub releases and hands them to the
// existing staged installer. It never replaces a running guard directly.
package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

const repository = "https://github.com/selimsandal/qbt-proton-guard"
const latestURL = "https://api.github.com/repos/selimsandal/qbt-proton-guard/releases/latest"

var identityPattern = regexp.MustCompile(`^0\.0\.([0-9]+)-g[0-9a-f]{7}$`)

type release struct {
	Tag        string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Assets     []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

// Run checks or installs the latest release. Installation requires authenticated
// HTTPS, a release signature from the embedded key, and matching binary hashes.
func Run(ctx context.Context, current string, checkOnly bool) (result error) {
	if !checkOnly {
		_ = WriteStatus("Checking for updates…", true, "")
		defer func() {
			if result != nil {
				Finish(result)
			}
		}()
	}
	client := updateClient()
	r, err := latest(ctx, client)
	if err != nil {
		return err
	}
	version := strings.TrimPrefix(r.Tag, "v")
	fmt.Printf("Installed: %s\nLatest: %s\n", current, version)
	if !newer(version, current) {
		fmt.Println("Already up to date; no changes made.")
		if !checkOnly {
			_ = WriteStatus("Up to date", false, "")
		}
		return nil
	}
	if checkOnly {
		fmt.Println("Run qbt-proton-guard update to install this release.")
		return nil
	}
	name := "qbt-proton-guard-" + runtime.GOOS + "-" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	dir, err := os.MkdirTemp("", "qbt-proton-guard-update-*")
	if err != nil {
		return err
	}
	path := filepath.Join(dir, name)
	_ = WriteStatus("Downloading update…", true, "")
	if err := download(ctx, client, r, name, path); err != nil {
		_ = os.RemoveAll(dir)
		return err
	}
	_ = WriteStatus("Installing update…", true, "")
	return install(path, dir)
}

func newer(candidate, current string) bool {
	if candidate == current {
		return false
	}
	a, b := identityPattern.FindStringSubmatch(candidate), identityPattern.FindStringSubmatch(current)
	if len(a) == 0 {
		return false
	}
	if len(b) == 0 {
		return true
	} // migrate legacy and development installations
	x, errA := strconv.ParseUint(a[1], 10, 64)
	y, errB := strconv.ParseUint(b[1], 10, 64)
	return errA == nil && errB == nil && x >= y
}

func latest(ctx context.Context, client *http.Client) (release, error) {
	var r release
	data, err := get(ctx, client, latestURL, 1<<20)
	if err != nil {
		return r, fmt.Errorf("check latest release: %w", err)
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return r, fmt.Errorf("decode release: %w", err)
	}
	if r.Draft || r.Prerelease || !strings.HasPrefix(r.Tag, "v") || !identityPattern.MatchString(strings.TrimPrefix(r.Tag, "v")) {
		return r, fmt.Errorf("unsupported release identity %q", r.Tag)
	}
	return r, nil
}

func assetURL(r release, name string) (string, error) {
	expected := repository + "/releases/download/" + r.Tag + "/" + name
	for _, asset := range r.Assets {
		if asset.Name == name && asset.URL == expected {
			return expected, nil
		}
	}
	return "", fmt.Errorf("release %s has no official %s asset", r.Tag, name)
}

func download(ctx context.Context, client *http.Client, r release, name, destination string) error {
	url, err := assetURL(r, name)
	if err != nil {
		return err
	}
	checksumsURL, err := assetURL(r, "SHA256SUMS")
	if err != nil {
		return err
	}
	sums, err := get(ctx, client, checksumsURL, 64<<10)
	if err != nil {
		return fmt.Errorf("download checksums: %w", err)
	}
	signatureURL, err := assetURL(r, "SHA256SUMS.sig")
	if err != nil {
		return err
	}
	signature, err := get(ctx, client, signatureURL, 64)
	if err != nil {
		return fmt.Errorf("download release signature: %w", err)
	}
	if err := verifyRelease(r.Tag, sums, signature); err != nil {
		return err
	}
	want := ""
	for _, line := range strings.Split(string(sums), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == name {
			if want != "" {
				return fmt.Errorf("duplicate checksum for %s", name)
			}
			want = fields[0]
		}
	}
	decoded, err := hex.DecodeString(want)
	if err != nil || len(decoded) != sha256.Size {
		return fmt.Errorf("missing or invalid checksum for %s", name)
	}
	data, err := get(ctx, client, url, 128<<20)
	if err != nil {
		return fmt.Errorf("download update: %w", err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(data)) != strings.ToLower(want) {
		return fmt.Errorf("update checksum mismatch; installation unchanged")
	}
	if err := os.WriteFile(destination, data, 0o700); err != nil {
		return err
	}
	return nil
}

func get(ctx context.Context, client *http.Client, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if err := trustedURL(req.URL); err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "qbt-proton-guard-updater")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("response exceeds %d bytes", limit)
	}
	return data, nil
}
