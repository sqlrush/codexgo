package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// githubLatestReleaseURL is the GitHub API endpoint for the latest codex release,
// mirroring GITHUB_LATEST_RELEASE_URL in updates.rs.
const githubLatestReleaseURL = "https://api.github.com/repos/openai/codex/releases/latest"

// versionCacheInfo is the on-disk version cache (version.json) shape the doctor
// reads, mirroring the VersionInfo struct in updates.rs.
type versionCacheInfo struct {
	LatestVersion    string `json:"latest_version"`
	LastCheckedAt    string `json:"last_checked_at"`
	DismissedVersion string `json:"dismissed_version"`
}

// parseVersionCache decodes the version.json cache contents.
func parseVersionCache(contents []byte) (versionCacheInfo, error) {
	var info versionCacheInfo
	if err := json.Unmarshal(contents, &info); err != nil {
		return versionCacheInfo{}, fmt.Errorf("decoding version cache: %w", err)
	}
	return info, nil
}

// fetchLatestVersion performs a bounded probe of the latest release version,
// mirroring fetch_latest_github_release_version in updates.rs (the tag is
// "rust-v<semver>"). It returns ("", nil) when CODEX_DOCTOR_SKIP_NETWORK is set so
// offline/deterministic runs skip the row entirely.
func fetchLatestVersion() (string, error) {
	if os.Getenv(doctorSkipNetworkEnv) != "" {
		return "", nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), networkProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubLatestReleaseURL, nil)
	if err != nil {
		return "", fmt.Errorf("building latest-version request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("latest-version request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("latest-version request returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("reading latest-version response: %w", err)
	}
	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &release); err != nil {
		return "", fmt.Errorf("decoding latest-version response: %w", err)
	}
	version, ok := strings.CutPrefix(release.TagName, "rust-v")
	if !ok {
		return "", fmt.Errorf("failed to parse latest tag %s", release.TagName)
	}
	return version, nil
}

// versionIsNewer reports whether latest is a strictly newer semantic version than
// current, mirroring is_newer in updates.rs. Non-semver inputs report false.
func versionIsNewer(latest, current string) bool {
	l, okL := parseSemver(latest)
	c, okC := parseSemver(current)
	if !okL || !okC {
		return false
	}
	for i := 0; i < 3; i++ {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

// parseSemver parses a "major.minor.patch" string into a 3-element array, mirroring
// parse_version in updates.rs. Extra/missing components or non-numeric parts make
// it return false.
func parseSemver(value string) ([3]uint64, bool) {
	parts := strings.Split(strings.TrimSpace(value), ".")
	if len(parts) < 3 {
		return [3]uint64{}, false
	}
	var out [3]uint64
	for i := 0; i < 3; i++ {
		n, err := strconv.ParseUint(parts[i], 10, 64)
		if err != nil {
			return [3]uint64{}, false
		}
		out[i] = n
	}
	return out, true
}
