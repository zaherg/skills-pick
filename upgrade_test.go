package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
)

const originalBinary = "#!/bin/sh\necho 'skills-pick 0.1.0'\n"

// fakeRelease serves a GitHub-style latest-release document plus its assets.
type fakeRelease struct {
	tag            string
	archiveName    string
	archive        []byte
	checksums      string
	archiveFetches atomic.Int32
}

func (f *fakeRelease) serve(t *testing.T) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			rel := releaseInfo{TagName: f.tag, Assets: []releaseAsset{
				{Name: f.archiveName, URL: srv.URL + "/archive"},
				{Name: "checksums.txt", URL: srv.URL + "/checksums"},
			}}
			_ = json.NewEncoder(w).Encode(rel)
		case "/archive":
			f.archiveFetches.Add(1)
			_, _ = w.Write(f.archive)
		case "/checksums":
			_, _ = io.WriteString(w, f.checksums)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func tarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(tw, body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sha(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// newUpgradeFixture installs a fake current binary and serves a release whose
// archive contains a shell script reporting reportedVersion.
func newUpgradeFixture(t *testing.T, current, tag, reportedVersion string) (*upgrader, *fakeRelease, *bytes.Buffer) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake release binaries are shell scripts")
	}
	ver := strings.TrimPrefix(tag, "v")
	archiveName, binName := releaseAssetNames(ver, runtime.GOOS, runtime.GOARCH)
	archive := tarGz(t, map[string]string{
		"README.md": "readme",
		binName:     fmt.Sprintf("#!/bin/sh\necho 'skills-pick %s'\n", reportedVersion),
	})
	rel := &fakeRelease{
		tag:         tag,
		archiveName: archiveName,
		archive:     archive,
		checksums:   fmt.Sprintf("%s  %s\n%s  other.tar.gz\n", sha(archive), archiveName, strings.Repeat("0", 64)),
	}
	srv := rel.serve(t)

	exe := filepath.Join(t.TempDir(), "skills-pick")
	if err := os.WriteFile(exe, []byte(originalBinary), 0o755); err != nil {
		t.Fatal(err)
	}
	out := &bytes.Buffer{}
	u := &upgrader{
		client:  srv.Client(),
		apiURL:  srv.URL + "/latest",
		current: current,
		goos:    runtime.GOOS,
		goarch:  runtime.GOARCH,
		exePath: exe,
		out:     out,
	}
	return u, rel, out
}

func assertBinary(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("binary content = %q, want %q", got, want)
	}
}

func assertNoTempFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".skills-pick-upgrade-") {
			t.Fatalf("leftover temporary file %s", e.Name())
		}
	}
}

func TestUpgradeReplacesOlderBinary(t *testing.T) {
	u, _, out := newUpgradeFixture(t, "0.1.0", "v0.2.0", "0.2.0")
	if err := os.Chmod(u.exePath, 0o750); err != nil {
		t.Fatal(err)
	}

	if err := u.run(); err != nil {
		t.Fatalf("run() error = %v", err)
	}

	assertBinary(t, u.exePath, "#!/bin/sh\necho 'skills-pick 0.2.0'\n")
	info, err := os.Stat(u.exePath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o751 {
		t.Fatalf("mode = %o, want original permissions plus execute bits (751)", got)
	}
	assertNoTempFiles(t, filepath.Dir(u.exePath))
	if !strings.Contains(out.String(), "Upgraded to skills-pick 0.2.0") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestUpgradeSkipsDownloadWhenCurrentOrNewer(t *testing.T) {
	for _, current := range []string{"0.2.0", "0.10.0"} {
		u, rel, out := newUpgradeFixture(t, current, "v0.2.0", "0.2.0")
		if err := u.run(); err != nil {
			t.Fatalf("current %s: run() error = %v", current, err)
		}
		if n := rel.archiveFetches.Load(); n != 0 {
			t.Fatalf("current %s: archive downloaded %d times", current, n)
		}
		assertBinary(t, u.exePath, originalBinary)
		if out.Len() == 0 {
			t.Fatalf("current %s: no status message", current)
		}
	}
}

func TestUpgradeForceReinstallsSameVersion(t *testing.T) {
	u, _, _ := newUpgradeFixture(t, "0.2.0", "v0.2.0", "0.2.0")
	u.force = true
	if err := u.run(); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	assertBinary(t, u.exePath, "#!/bin/sh\necho 'skills-pick 0.2.0'\n")
}

func TestUpgradeRefusesDevBuildWithoutForce(t *testing.T) {
	u, rel, _ := newUpgradeFixture(t, "dev", "v0.2.0", "0.2.0")
	err := u.run()
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("run() error = %v, want refusal mentioning --force", err)
	}
	if rel.archiveFetches.Load() != 0 {
		t.Fatal("archive downloaded for refused dev build")
	}
	assertBinary(t, u.exePath, originalBinary)

	u.force = true
	if err := u.run(); err != nil {
		t.Fatalf("forced run() error = %v", err)
	}
	assertBinary(t, u.exePath, "#!/bin/sh\necho 'skills-pick 0.2.0'\n")
}

func TestUpgradeFailuresLeaveBinaryUntouched(t *testing.T) {
	cases := map[string]struct {
		mutate  func(*fakeRelease)
		version string
		wantErr string
	}{
		"checksum mismatch": {
			mutate:  func(f *fakeRelease) { f.archive = append(append([]byte{}, f.archive...), 0) },
			wantErr: "checksum mismatch",
		},
		"missing checksum entry": {
			mutate:  func(f *fakeRelease) { f.checksums = strings.Repeat("0", 64) + "  other.tar.gz\n" },
			wantErr: "no entry",
		},
		"missing platform asset": {
			mutate:  func(f *fakeRelease) { f.archiveName = "skills-pick_0.2.0_plan9_mips.tar.gz" },
			wantErr: "has no asset",
		},
		"binary reports wrong version": {
			version: "0.1.9",
			wantErr: "reports",
		},
		"archive without binary": {
			mutate: func(f *fakeRelease) {
				f.archive = tarGz(t, map[string]string{"README.md": "readme"})
				f.checksums = fmt.Sprintf("%s  %s\n", sha(f.archive), f.archiveName)
			},
			wantErr: "does not contain",
		},
		"malformed tag": {
			mutate:  func(f *fakeRelease) { f.tag = "latest" },
			wantErr: "unexpected tag",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			version := tc.version
			if version == "" {
				version = "0.2.0"
			}
			u, rel, _ := newUpgradeFixture(t, "0.1.0", "v0.2.0", version)
			if tc.mutate != nil {
				tc.mutate(rel)
			}
			err := u.run()
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("run() error = %v, want containing %q", err, tc.wantErr)
			}
			assertBinary(t, u.exePath, originalBinary)
			assertNoTempFiles(t, filepath.Dir(u.exePath))
		})
	}
}

func TestExtractFromZipFindsWindowsBinary(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range map[string]string{"README.md": "readme", "skills-pick.exe": "MZbinary"} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(w, body); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	got, err := extractBinary(buf.Bytes(), "skills-pick_0.2.0_windows_amd64.zip", "skills-pick.exe")
	if err != nil || string(got) != "MZbinary" {
		t.Fatalf("extractBinary() = %q, %v", got, err)
	}
}

func TestReplaceFileWindowsMovesOldBinaryAside(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "skills-pick.exe")
	src := filepath.Join(dir, "new.exe")
	for path, body := range map[string]string{dst: "old", src: "new", dst + ".old": "stale"} {
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	if err := replaceFile(src, dst, "windows"); err != nil {
		t.Fatal(err)
	}
	assertBinary(t, dst, "new")
	assertBinary(t, dst+".old", "old")
}

func TestReleaseAssetNamesMatchGoReleaserTemplate(t *testing.T) {
	cases := []struct{ goos, goarch, archive, bin string }{
		{"darwin", "arm64", "skills-pick_0.3.6_darwin_arm64.tar.gz", "skills-pick"},
		{"linux", "amd64", "skills-pick_0.3.6_linux_amd64.tar.gz", "skills-pick"},
		{"windows", "arm64", "skills-pick_0.3.6_windows_arm64.zip", "skills-pick.exe"},
	}
	for _, c := range cases {
		archive, bin := releaseAssetNames("0.3.6", c.goos, c.goarch)
		if archive != c.archive || bin != c.bin {
			t.Fatalf("releaseAssetNames(%s/%s) = %s, %s", c.goos, c.goarch, archive, bin)
		}
	}
}

func TestParseAndCompareVersions(t *testing.T) {
	for _, bad := range []string{"dev", "", "1.2", "1.2.3.4", "v1.2.3", "1.2.x", "01.2.3", "-1.2.3", "1.2.3-rc1"} {
		if _, ok := parseVersion(bad); ok {
			t.Fatalf("parseVersion(%q) accepted invalid version", bad)
		}
	}
	cases := []struct {
		a, b string
		want int
	}{
		{"0.3.6", "0.3.6", 0},
		{"0.3.6", "0.3.7", -1},
		{"0.10.0", "0.9.9", 1},
		{"1.0.0", "0.99.99", 1},
	}
	for _, c := range cases {
		a, _ := parseVersion(c.a)
		b, _ := parseVersion(c.b)
		if got := compareVersions(a, b); got != c.want {
			t.Fatalf("compareVersions(%s, %s) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
