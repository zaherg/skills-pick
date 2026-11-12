package main

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	releaseRepo      = "zaherg/skills-pick"
	latestReleaseAPI = "https://api.github.com/repos/" + releaseRepo + "/releases/latest"

	// Release archives are about 1 MiB; these bounds only stop runaway or
	// hostile responses from exhausting memory.
	maxMetadataBytes = 1 << 20
	maxArchiveBytes  = 64 << 20
	maxBinaryBytes   = 128 << 20
)

type releaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

type releaseInfo struct {
	TagName string         `json:"tag_name"`
	Assets  []releaseAsset `json:"assets"`
}

type upgrader struct {
	client  *http.Client
	apiURL  string
	current string // version of the running binary, without a leading "v"
	goos    string
	goarch  string
	exePath string // resolved path of the binary to replace
	force   bool
	out     io.Writer
}

func cmdUpgrade(args []string) {
	force := false
	for _, a := range args {
		switch a {
		case "--force", "-f":
			force = true
		default:
			fmt.Fprintln(os.Stderr, "Usage: skills-pick upgrade [--force]")
			os.Exit(1)
		}
	}

	exe, err := resolveExecutable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "upgrade failed: %v\n", err)
		os.Exit(1)
	}
	u := &upgrader{
		client:  &http.Client{Timeout: 2 * time.Minute},
		apiURL:  latestReleaseAPI,
		current: version,
		goos:    runtime.GOOS,
		goarch:  runtime.GOARCH,
		exePath: exe,
		force:   force,
		out:     os.Stdout,
	}
	if err := u.run(); err != nil {
		fmt.Fprintf(os.Stderr, "\x1b[31mupgrade failed: %v\x1b[0m\n", err)
		os.Exit(1)
	}
}

// resolveExecutable returns the real file behind the running binary so a
// symlinked install (for example ~/bin/skills-pick -> /opt/...) replaces the
// target instead of turning the link into a regular file.
func resolveExecutable() (string, error) {
	p, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate running binary: %w", err)
	}
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", fmt.Errorf("resolve running binary %s: %w", p, err)
	}
	return r, nil
}

func (u *upgrader) run() error {
	rel, err := u.fetchLatest()
	if err != nil {
		return err
	}
	latest := strings.TrimPrefix(rel.TagName, "v")
	latestVer, ok := parseVersion(latest)
	if !ok {
		return fmt.Errorf("latest release has unexpected tag %q", rel.TagName)
	}

	currentVer, currentOK := parseVersion(u.current)
	switch {
	case !currentOK && !u.force:
		return fmt.Errorf("running a development build (version %q); rerun with --force to replace it with release %s", u.current, latest)
	case currentOK && compareVersions(currentVer, latestVer) == 0 && !u.force:
		fmt.Fprintf(u.out, "skills-pick %s is already the latest release.\n", u.current)
		return nil
	case currentOK && compareVersions(currentVer, latestVer) > 0 && !u.force:
		fmt.Fprintf(u.out, "skills-pick %s is newer than the latest release %s; nothing to do.\n", u.current, latest)
		return nil
	}

	archiveName, binName := releaseAssetNames(latest, u.goos, u.goarch)
	archiveAsset, ok := findAsset(rel.Assets, archiveName)
	if !ok {
		return fmt.Errorf("release %s has no asset %s for %s/%s", rel.TagName, archiveName, u.goos, u.goarch)
	}
	sumsAsset, ok := findAsset(rel.Assets, "checksums.txt")
	if !ok {
		return fmt.Errorf("release %s has no checksums.txt", rel.TagName)
	}

	fmt.Fprintf(u.out, "Upgrading %s from %s to %s...\n", u.exePath, u.current, latest)

	sums, err := u.download(sumsAsset.URL, maxMetadataBytes)
	if err != nil {
		return err
	}
	want, err := checksumFor(sums, archiveName)
	if err != nil {
		return err
	}
	archive, err := u.download(archiveAsset.URL, maxArchiveBytes)
	if err != nil {
		return err
	}
	got := sha256.Sum256(archive)
	if hex.EncodeToString(got[:]) != want {
		return fmt.Errorf("checksum mismatch for %s", archiveName)
	}

	bin, err := extractBinary(archive, archiveName, binName)
	if err != nil {
		return err
	}
	if err := installBinary(u.exePath, bin, latest, u.goos); err != nil {
		return err
	}
	fmt.Fprintf(u.out, "Upgraded to skills-pick %s.\n", latest)
	return nil
}

func (u *upgrader) fetchLatest() (*releaseInfo, error) {
	body, err := u.download(u.apiURL, maxMetadataBytes)
	if err != nil {
		return nil, fmt.Errorf("fetch latest release: %w", err)
	}
	var rel releaseInfo
	if err := json.Unmarshal(body, &rel); err != nil {
		return nil, fmt.Errorf("parse latest release: %w", err)
	}
	return &rel, nil
}

func (u *upgrader) download(url string, limit int64) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "skills-pick/"+u.current)
	req.Header.Set("Accept", "application/vnd.github+json, application/octet-stream")
	resp, err := u.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: HTTP %d", url, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("download %s: response exceeds %d bytes", url, limit)
	}
	return data, nil
}

// releaseAssetNames mirrors the archive name_template in .goreleaser.yaml.
func releaseAssetNames(ver, goos, goarch string) (archive, binary string) {
	archive = fmt.Sprintf("skills-pick_%s_%s_%s", ver, goos, goarch)
	if goos == "windows" {
		return archive + ".zip", "skills-pick.exe"
	}
	return archive + ".tar.gz", "skills-pick"
}

func findAsset(assets []releaseAsset, name string) (releaseAsset, bool) {
	for _, a := range assets {
		if a.Name == name {
			return a, true
		}
	}
	return releaseAsset{}, false
}

func checksumFor(sums []byte, name string) (string, error) {
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 2 && fields[1] == name {
			sum := strings.ToLower(fields[0])
			if b, err := hex.DecodeString(sum); err != nil || len(b) != sha256.Size {
				return "", fmt.Errorf("checksums.txt has a malformed entry for %s", name)
			}
			return sum, nil
		}
	}
	return "", fmt.Errorf("checksums.txt has no entry for %s", name)
}

func extractBinary(archive []byte, archiveName, binName string) ([]byte, error) {
	if strings.HasSuffix(archiveName, ".zip") {
		return extractFromZip(archive, binName)
	}
	return extractFromTarGz(archive, binName)
}

func extractFromTarGz(archive []byte, binName string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("open archive: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("archive does not contain %s", binName)
		}
		if err != nil {
			return nil, fmt.Errorf("read archive: %w", err)
		}
		if hdr.Name == binName && hdr.Typeflag == tar.TypeReg {
			return readLimited(tr, binName)
		}
	}
}

func extractFromZip(archive []byte, binName string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, fmt.Errorf("open archive: %w", err)
	}
	for _, f := range zr.File {
		if f.Name == binName && f.Mode().IsRegular() {
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("read archive: %w", err)
			}
			defer rc.Close()
			return readLimited(rc, binName)
		}
	}
	return nil, fmt.Errorf("archive does not contain %s", binName)
}

func readLimited(r io.Reader, name string) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxBinaryBytes+1))
	if err != nil {
		return nil, fmt.Errorf("extract %s: %w", name, err)
	}
	if len(data) > maxBinaryBytes {
		return nil, fmt.Errorf("extract %s: binary exceeds %d bytes", name, maxBinaryBytes)
	}
	return data, nil
}

// installBinary writes bin next to exePath, checks that it runs and reports
// wantVersion, then swaps it in by rename. The temporary file lives in the
// same directory so the rename stays on one filesystem; a rename also gives
// the new binary a fresh inode, which macOS requires for code signatures.
func installBinary(exePath string, bin []byte, wantVersion, goos string) (err error) {
	info, err := os.Stat(exePath)
	if err != nil {
		return fmt.Errorf("inspect %s: %w", exePath, err)
	}
	dir := filepath.Dir(exePath)
	pattern := ".skills-pick-upgrade-*"
	if goos == "windows" {
		pattern += ".exe"
	}
	tmp, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return fmt.Errorf("cannot write to %s (try again with sufficient permissions): %w", dir, err)
	}
	tmpPath := tmp.Name()
	defer func() {
		if err != nil {
			os.Remove(tmpPath)
		}
	}()

	if _, err = tmp.Write(bin); err != nil {
		tmp.Close()
		return fmt.Errorf("write %s: %w", tmpPath, err)
	}
	if err = tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync %s: %w", tmpPath, err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmpPath, err)
	}
	if err = os.Chmod(tmpPath, info.Mode().Perm()|0o111); err != nil {
		return fmt.Errorf("chmod %s: %w", tmpPath, err)
	}
	if err = verifyBinaryVersion(tmpPath, wantVersion); err != nil {
		return err
	}
	return replaceFile(tmpPath, exePath, goos)
}

func verifyBinaryVersion(path, want string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--version").Output()
	if err != nil {
		return fmt.Errorf("downloaded binary failed to run: %w", err)
	}
	if got := strings.TrimSpace(string(out)); got != "skills-pick "+want {
		return fmt.Errorf("downloaded binary reports %q, want %q", got, "skills-pick "+want)
	}
	return nil
}

// replaceFile moves src over dst. Windows refuses to overwrite a running
// executable but allows renaming it, so the old binary is moved aside to
// dst+".old" first; it is deleted by the next upgrade.
func replaceFile(src, dst, goos string) error {
	if goos != "windows" {
		if err := os.Rename(src, dst); err != nil {
			return fmt.Errorf("replace %s: %w", dst, err)
		}
		return nil
	}
	old := dst + ".old"
	if err := os.Remove(old); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale %s: %w", old, err)
	}
	if err := os.Rename(dst, old); err != nil {
		return fmt.Errorf("move %s aside: %w", dst, err)
	}
	if err := os.Rename(src, dst); err != nil {
		if rerr := os.Rename(old, dst); rerr != nil {
			return fmt.Errorf("replace %s: %w (restoring the previous binary from %s also failed: %v)", dst, err, old, rerr)
		}
		return fmt.Errorf("replace %s: %w", dst, err)
	}
	return nil
}

func parseVersion(s string) ([3]int, bool) {
	var v [3]int
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || p != strconv.Itoa(n) {
			return v, false
		}
		v[i] = n
	}
	return v, true
}

func compareVersions(a, b [3]int) int {
	for i := range a {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}
