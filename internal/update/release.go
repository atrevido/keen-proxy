// Package update coordinates stable release checks and supervised Entware updates.
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strings"
)

const latestReleaseURL = "https://api.github.com/repos/ordo-systems/keen-proxy/releases/latest"

var stable = regexp.MustCompile(`^v(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})$`)

func newer(candidate, current string) bool {
	if !stable.MatchString(candidate) || !stable.MatchString(current) {
		return false
	}
	parts := strings.Split(candidate, ".")
	for i, old := range strings.Split(current, ".") {
		if len(parts[i]) != len(old) {
			return len(parts[i]) > len(old)
		}
		if parts[i] != old {
			return parts[i] > old
		}
	}
	return false
}

func releaseReady(tag string, names []string, variant, arch string) bool {
	binary := "keenproxy-linux-" + arch
	if variant == "lite" {
		binary = "keenproxy-lite-linux-" + arch
	}
	return stable.MatchString(tag) && slices.Contains(names, binary) &&
		slices.Contains(names, "S99keenproxy") && slices.Contains(names, "install.sh") &&
		slices.Contains(names, "SHA256SUMS")
}

// fetchLatest accepts only complete, stable releases from the official repository.
func fetchLatest(ctx context.Context, client *http.Client, variant, arch string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestReleaseURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("release check returned HTTP %d", resp.StatusCode)
	}
	return decodeRelease(resp.Body, variant, arch)
}

func decodeRelease(body io.Reader, variant, arch string) (string, error) {
	var release struct {
		Tag        string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
		Assets     []struct {
			Name  string `json:"name"`
			State string `json:"state"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(body, 1<<20)).Decode(&release); err != nil {
		return "", err
	}
	names := make([]string, 0, len(release.Assets))
	for _, asset := range release.Assets {
		if asset.State == "uploaded" {
			names = append(names, asset.Name)
		}
	}
	if release.Draft || release.Prerelease || !releaseReady(release.Tag, names, variant, arch) {
		return "", fmt.Errorf("latest stable release does not have the required assets")
	}
	return release.Tag, nil
}
