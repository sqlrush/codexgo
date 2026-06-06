package plugins

// The real curated-plugins transports (git / GitHub HTTP / export archive),
// porting the transport bodies of the Rust `core-plugins/src/startup_sync.rs`.
// Each transport refreshes the local snapshot and records the synced HEAD sha.

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	curatedPluginsGitTimeout    = 30 * time.Second
	curatedPluginsHTTPTimeout   = 30 * time.Second
	githubAPIAcceptHeader       = "application/vnd.github+json"
	githubAPIVersionHeader      = "2022-11-28"
	githubAPIVersionHeaderField = "X-GitHub-Api-Version"
)

// defaultCuratedSyncTransport implements [CuratedSyncTransport] over the real
// git binary and HTTP endpoints. The fields are seams so tests may point at a
// fake git binary and httptest servers.
type defaultCuratedSyncTransport struct {
	gitBinary           string
	apiBaseURL          string
	backupArchiveAPIURL string
}

var _ CuratedSyncTransport = defaultCuratedSyncTransport{}

// SyncViaGit refreshes the snapshot via git, mirroring Rust's
// `sync_openai_plugins_repo_via_git`: ls-remote the HEAD sha, short-circuit when
// the local snapshot already matches, otherwise shallow-clone, verify the cloned
// HEAD, ensure the manifest, activate, and record the sha.
func (t defaultCuratedSyncTransport) SyncViaGit(codexHome string) (string, error) {
	repoPath := CuratedPluginsRepoPath(codexHome)
	shaPath := curatedPluginsSHAPath(codexHome)
	remoteSHA, err := gitLsRemoteHeadSHA(t.gitBinary)
	if err != nil {
		return "", err
	}
	localSHA := readLocalGitOrSHAFile(repoPath, shaPath, t.gitBinary)
	if localSHA == remoteSHA && isDir(filepath.Join(repoPath, ".git")) {
		return remoteSHA, nil
	}

	stagedDir, err := prepareCuratedRepoParentAndTempDir(repoPath)
	if err != nil {
		return "", err
	}
	cleanup := func() { _ = os.RemoveAll(stagedDir) }

	if err := runGitCloneInto(t.gitBinary, stagedDir); err != nil {
		cleanup()
		return "", err
	}
	clonedSHA, err := gitHeadSHA(stagedDir, t.gitBinary)
	if err != nil {
		cleanup()
		return "", err
	}
	if clonedSHA != remoteSHA {
		cleanup()
		return "", fmt.Errorf("curated plugins clone HEAD mismatch: expected %s, got %s", remoteSHA, clonedSHA)
	}
	if err := ensureMarketplaceManifestExists(stagedDir); err != nil {
		cleanup()
		return "", err
	}
	if err := activateCuratedRepo(repoPath, stagedDir); err != nil {
		cleanup()
		return "", err
	}
	if err := writeCuratedPluginsSHA(shaPath, remoteSHA); err != nil {
		return "", err
	}
	return remoteSHA, nil
}

// SyncViaHTTP refreshes the snapshot via the GitHub HTTP API, mirroring Rust's
// `sync_openai_plugins_repo_via_http`: resolve the default-branch HEAD sha,
// short-circuit when the recorded sha matches, otherwise download the zipball,
// extract, ensure the manifest, activate, and record the sha.
func (t defaultCuratedSyncTransport) SyncViaHTTP(codexHome string) (string, error) {
	repoPath := CuratedPluginsRepoPath(codexHome)
	shaPath := curatedPluginsSHAPath(codexHome)
	remoteSHA, err := fetchCuratedRepoRemoteSHA(t.apiBaseURL)
	if err != nil {
		return "", err
	}
	if localSHA, ok := readSHAFile(shaPath); ok && localSHA == remoteSHA && isDir(repoPath) {
		return remoteSHA, nil
	}

	stagedDir, err := prepareCuratedRepoParentAndTempDir(repoPath)
	if err != nil {
		return "", err
	}
	cleanup := func() { _ = os.RemoveAll(stagedDir) }

	zipBytes, err := fetchCuratedRepoZipball(t.apiBaseURL, remoteSHA)
	if err != nil {
		cleanup()
		return "", err
	}
	if err := extractZipballToDir(zipBytes, stagedDir); err != nil {
		cleanup()
		return "", err
	}
	if err := ensureMarketplaceManifestExists(stagedDir); err != nil {
		cleanup()
		return "", err
	}
	if err := activateCuratedRepo(repoPath, stagedDir); err != nil {
		cleanup()
		return "", err
	}
	if err := writeCuratedPluginsSHA(shaPath, remoteSHA); err != nil {
		return "", err
	}
	return remoteSHA, nil
}

// SyncViaBackupArchive bootstraps the snapshot from the export archive, mirroring
// Rust's `sync_openai_plugins_repo_via_backup_archive`. The recorded version is
// the archive's git sha when present, else the synthetic fallback version.
func (t defaultCuratedSyncTransport) SyncViaBackupArchive(codexHome string) (string, error) {
	repoPath := CuratedPluginsRepoPath(codexHome)
	shaPath := curatedPluginsSHAPath(codexHome)

	stagedDir, err := prepareCuratedRepoParentAndTempDir(repoPath)
	if err != nil {
		return "", err
	}
	cleanup := func() { _ = os.RemoveAll(stagedDir) }

	zipBytes, err := fetchCuratedRepoBackupArchiveZip(t.backupArchiveAPIURL)
	if err != nil {
		cleanup()
		return "", err
	}
	if err := extractZipballToDir(zipBytes, stagedDir); err != nil {
		cleanup()
		return "", err
	}
	if err := ensureMarketplaceManifestExists(stagedDir); err != nil {
		cleanup()
		return "", err
	}
	exportVersion := curatedPluginsBackupArchiveFallbackVersion
	if sha, ok := readExtractedBackupArchiveGitSHA(stagedDir); ok {
		exportVersion = sha
	}
	if err := activateCuratedRepo(repoPath, stagedDir); err != nil {
		cleanup()
		return "", err
	}
	if err := writeCuratedPluginsSHA(shaPath, exportVersion); err != nil {
		return "", err
	}
	return exportVersion, nil
}

// readLocalGitOrSHAFile reads the local HEAD sha from the cloned repo when it is
// a git checkout, else from the recorded sha file, mirroring Rust's
// `read_local_git_or_sha_file`.
func readLocalGitOrSHAFile(repoPath, shaPath, gitBinary string) string {
	if isDir(filepath.Join(repoPath, ".git")) {
		if sha, err := gitHeadSHA(repoPath, gitBinary); err == nil {
			return sha
		}
	}
	sha, _ := readSHAFile(shaPath)
	return sha
}

// gitLsRemoteHeadSHA returns the remote HEAD sha, mirroring Rust's
// `git_ls_remote_head_sha`.
func gitLsRemoteHeadSHA(gitBinary string) (string, error) {
	out, err := runGitWithTimeout(curatedPluginsGitTimeout, gitBinary,
		"ls-remote", openaiPluginsGitURL, "HEAD")
	if err != nil {
		return "", fmt.Errorf("git ls-remote curated plugins repo: %w", err)
	}
	firstLine := firstLine(string(out))
	if firstLine == "" {
		return "", fmt.Errorf("git ls-remote returned empty output for curated plugins repo")
	}
	sha, _, found := strings.Cut(firstLine, "\t")
	if !found {
		return "", fmt.Errorf("unexpected git ls-remote output for curated plugins repo: %s", firstLine)
	}
	if sha == "" {
		return "", fmt.Errorf("git ls-remote returned empty sha for curated plugins repo")
	}
	return sha, nil
}

// runGitCloneInto shallow-clones the curated repo into dest, mirroring the clone
// invocation in Rust's `sync_openai_plugins_repo_via_git`.
func runGitCloneInto(gitBinary, dest string) error {
	if _, err := runGitWithTimeout(curatedPluginsGitTimeout, gitBinary,
		"clone", "--depth", "1", openaiPluginsGitURL, dest); err != nil {
		return fmt.Errorf("git clone curated plugins repo: %w", err)
	}
	return nil
}

// gitHeadSHA returns the HEAD sha of the repo at repoPath, mirroring Rust's
// `git_head_sha`.
func gitHeadSHA(repoPath, gitBinary string) (string, error) {
	out, err := runGitWithTimeout(curatedPluginsGitTimeout, gitBinary,
		"-C", repoPath, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD in %s: %w", repoPath, err)
	}
	sha := strings.TrimSpace(string(out))
	if sha == "" {
		return "", fmt.Errorf("git rev-parse HEAD returned empty output in %s", repoPath)
	}
	return sha, nil
}

// runGitWithTimeout runs git with the given args under a timeout, returning
// stdout. It sets GIT_OPTIONAL_LOCKS=0 like the Rust commands and surfaces
// stderr in the error, mirroring Rust's `ensure_git_success`.
func runGitWithTimeout(timeout time.Duration, gitBinary string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, gitBinary, args...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("git command timed out after %s", timeout)
	}
	if err != nil {
		stderrStr := strings.TrimSpace(stderr.String())
		if stderrStr == "" {
			return nil, fmt.Errorf("git command failed: %w", err)
		}
		return nil, fmt.Errorf("git command failed: %w: %s", err, stderrStr)
	}
	return stdout.Bytes(), nil
}

// githubRepositorySummary mirrors the GitHub repo response subset codex parses.
type githubRepositorySummary struct {
	DefaultBranch string `json:"default_branch"`
}

// githubGitRefSummary mirrors the GitHub ref response subset codex parses.
type githubGitRefSummary struct {
	Object struct {
		SHA string `json:"sha"`
	} `json:"object"`
}

// curatedPluginsBackupArchiveResponse mirrors the export archive metadata.
type curatedPluginsBackupArchiveResponse struct {
	DownloadURL string `json:"download_url"`
}

// fetchCuratedRepoRemoteSHA resolves the curated repo's default-branch HEAD sha
// via the GitHub HTTP API, mirroring Rust's `fetch_curated_repo_remote_sha`.
func fetchCuratedRepoRemoteSHA(apiBaseURL string) (string, error) {
	base := strings.TrimRight(apiBaseURL, "/")
	repoURL := fmt.Sprintf("%s/repos/%s/%s", base, openaiPluginsOwner, openaiPluginsRepo)
	repoBody, err := fetchGitHubText(repoURL, "get curated plugins repository")
	if err != nil {
		return "", err
	}
	var summary githubRepositorySummary
	if err := json.Unmarshal(repoBody, &summary); err != nil {
		return "", fmt.Errorf("failed to parse curated plugins repository response from %s: %w", repoURL, err)
	}
	if summary.DefaultBranch == "" {
		return "", fmt.Errorf("curated plugins repository response from %s did not include a default branch", repoURL)
	}

	refURL := fmt.Sprintf("%s/git/ref/heads/%s", repoURL, summary.DefaultBranch)
	refBody, err := fetchGitHubText(refURL, "get curated plugins HEAD ref")
	if err != nil {
		return "", err
	}
	var ref githubGitRefSummary
	if err := json.Unmarshal(refBody, &ref); err != nil {
		return "", fmt.Errorf("failed to parse curated plugins ref response from %s: %w", refURL, err)
	}
	if ref.Object.SHA == "" {
		return "", fmt.Errorf("curated plugins ref response from %s did not include a HEAD sha", refURL)
	}
	return ref.Object.SHA, nil
}

// fetchCuratedRepoZipball downloads the curated repo zipball for remoteSHA,
// mirroring Rust's `fetch_curated_repo_zipball`.
func fetchCuratedRepoZipball(apiBaseURL, remoteSHA string) ([]byte, error) {
	base := strings.TrimRight(apiBaseURL, "/")
	zipURL := fmt.Sprintf("%s/repos/%s/%s/zipball/%s", base, openaiPluginsOwner, openaiPluginsRepo, remoteSHA)
	return fetchGitHubBytes(zipURL, "download curated plugins archive")
}

// fetchCuratedRepoBackupArchiveZip resolves and downloads the export archive,
// mirroring Rust's `fetch_curated_repo_backup_archive_zip`.
func fetchCuratedRepoBackupArchiveZip(backupArchiveAPIURL string) ([]byte, error) {
	body, err := fetchPublicText(backupArchiveAPIURL, "get curated plugins export archive metadata")
	if err != nil {
		return nil, err
	}
	var resp curatedPluginsBackupArchiveResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse curated plugins backup archive response from %s: %w", backupArchiveAPIURL, err)
	}
	if resp.DownloadURL == "" {
		return nil, fmt.Errorf("curated plugins backup archive response from %s did not include a download URL", backupArchiveAPIURL)
	}
	return fetchPublicBytes(resp.DownloadURL, "download curated plugins export archive")
}

// fetchGitHubText fetches a GitHub API text body with the standard headers,
// mirroring Rust's `fetch_github_text`.
func fetchGitHubText(url, context string) ([]byte, error) {
	return httpGet(url, context, true)
}

// fetchGitHubBytes fetches GitHub API bytes with the standard headers.
func fetchGitHubBytes(url, context string) ([]byte, error) {
	return httpGet(url, context, true)
}

// fetchPublicText fetches a public (non-GitHub) text body.
func fetchPublicText(url, context string) ([]byte, error) {
	return httpGet(url, context, false)
}

// fetchPublicBytes fetches public bytes.
func fetchPublicBytes(url, context string) ([]byte, error) {
	return httpGet(url, context, false)
}

// httpGet performs a GET, applying GitHub headers when github is set, and errors
// on non-2xx, mirroring the Rust fetch helpers' success check.
func httpGet(url, context string, github bool) ([]byte, error) {
	client := &http.Client{Timeout: curatedPluginsHTTPTimeout}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to %s from %s: %w", context, url, err)
	}
	req.Header.Set("User-Agent", "codex-cli")
	if github {
		req.Header.Set("Accept", githubAPIAcceptHeader)
		req.Header.Set(githubAPIVersionHeaderField, githubAPIVersionHeader)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to %s from %s: %w", context, url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s response from %s: %w", context, url, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("failed to %s from %s: status %d", context, url, resp.StatusCode)
	}
	return body, nil
}

// extractZipballToDir extracts a GitHub-style zipball into dir, stripping the
// single top-level wrapper directory the GitHub archive nests everything under,
// mirroring Rust's `extract_zipball_to_dir`.
func extractZipballToDir(zipBytes []byte, dir string) error {
	reader, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return fmt.Errorf("failed to read curated plugins archive: %w", err)
	}
	prefix := zipballTopLevelPrefix(reader)
	for _, file := range reader.File {
		rel := strings.TrimPrefix(file.Name, prefix)
		rel = strings.TrimPrefix(rel, "/")
		if rel == "" {
			continue
		}
		if err := extractZipEntry(file, dir, rel); err != nil {
			return err
		}
	}
	return nil
}

// zipballTopLevelPrefix returns the common single top-level directory the GitHub
// zipball wraps everything in (e.g. "openai-plugins-<sha>/"), or "" when none.
func zipballTopLevelPrefix(reader *zip.Reader) string {
	prefix := ""
	for _, file := range reader.File {
		name := file.Name
		idx := strings.IndexByte(name, '/')
		if idx < 0 {
			return ""
		}
		top := name[:idx+1]
		if prefix == "" {
			prefix = top
		} else if prefix != top {
			return ""
		}
	}
	return prefix
}

// extractZipEntry writes one zip entry under destDir at the relative path rel,
// rejecting path traversal outside destDir.
func extractZipEntry(file *zip.File, destDir, rel string) error {
	target := filepath.Join(destDir, rel)
	cleanDest := filepath.Clean(destDir) + string(os.PathSeparator)
	if !strings.HasPrefix(filepath.Clean(target)+string(os.PathSeparator), cleanDest) &&
		filepath.Clean(target) != filepath.Clean(destDir) {
		return fmt.Errorf("curated plugins archive entry escapes destination: %s", file.Name)
	}
	if file.FileInfo().IsDir() {
		return os.MkdirAll(target, 0o755)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("failed to create curated plugins archive directory %s: %w", filepath.Dir(target), err)
	}
	rc, err := file.Open()
	if err != nil {
		return fmt.Errorf("failed to open curated plugins archive entry %s: %w", file.Name, err)
	}
	defer rc.Close()
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("failed to create curated plugins archive file %s: %w", target, err)
	}
	defer out.Close()
	if _, err := io.Copy(out, rc); err != nil {
		return fmt.Errorf("failed to extract curated plugins archive file %s: %w", target, err)
	}
	return nil
}

// readExtractedBackupArchiveGitSHA reads the git sha recorded in the extracted
// export archive (a `.git-sha` file or a packed HEAD ref), mirroring Rust's
// `read_extracted_backup_archive_git_sha`. Returns ("", false) when absent.
func readExtractedBackupArchiveGitSHA(repoPath string) (string, bool) {
	if sha, ok := readSHAFile(filepath.Join(repoPath, ".git-sha")); ok {
		return sha, true
	}
	if sha, ok := readSHAFile(filepath.Join(repoPath, ".git", "HEAD")); ok && !strings.HasPrefix(sha, "ref:") {
		return sha, true
	}
	return "", false
}

// firstLine returns the first line of s (without the trailing newline).
func firstLine(s string) string {
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		return s[:idx]
	}
	return s
}
