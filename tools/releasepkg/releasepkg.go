// Command releasepkg turns the checksums of a viva release into the manifests
// that package managers read: a Homebrew formula and a Scoop manifest. The
// release workflow runs it after building the archives, so the manifests always
// name the digests of the files that were actually published.
//
//	go run ./tools/releasepkg -version v0.1.3 -sums dist/SHA256SUMS -out dist/pkg
//
// It writes Formula/viva.rb and bucket/viva.json under -out, the layout of the
// vivaui/homebrew-tap repository, which serves both Homebrew and Scoop.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	// Repository is the GitHub repository whose releases carry the archives.
	Repository = "vivaui/viva"
	// Homepage is the project's site.
	Homepage    = "https://vivaui.dev"
	Description = "Build, package and live-reload Viva desktop applications"
	License     = "Apache-2.0"
)

// Platform is one operating system and architecture a release ships for.
type Platform struct{ OS, Arch string }

// Platforms lists every archive a release has, in the order manifests show them.
var Platforms = []Platform{
	{"darwin", "arm64"}, {"darwin", "amd64"},
	{"linux", "arm64"}, {"linux", "amd64"},
	{"windows", "amd64"}, {"windows", "arm64"},
}

// ArchiveName is the file name of the archive for a platform:
// viva_v0.1.3_darwin_arm64.tar.gz, or .zip for Windows.
func ArchiveName(version string, p Platform) string {
	extension := ".tar.gz"
	if p.OS == "windows" {
		extension = ".zip"
	}
	return fmt.Sprintf("viva_%s_%s_%s%s", version, p.OS, p.Arch, extension)
}

// DownloadURL is where a release archive is served from.
func DownloadURL(version, name string) string {
	return fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", Repository, version, name)
}

// Sums maps an archive file name to its SHA-256, as sha256sum prints them.
type Sums map[string]string

// ParseSums reads the output of sha256sum: "<64 hex digits>  <file name>".
func ParseSums(r io.Reader) (Sums, error) {
	sums := Sums{}
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		digest, name, ok := strings.Cut(line, " ")
		name = strings.TrimLeft(name, " *")
		if !ok || name == "" || !validDigest(digest) {
			return nil, fmt.Errorf("not a sha256sum line: %q", line)
		}
		if _, dup := sums[name]; dup {
			return nil, fmt.Errorf("%s is listed twice", name)
		}
		sums[name] = digest
	}
	return sums, scanner.Err()
}

func validDigest(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// ValidVersion accepts a tag such as v0.1.3 or v0.2.0-rc.1.
func ValidVersion(version string) bool {
	if !strings.HasPrefix(version, "v") {
		return false
	}
	core, pre, hasPre := strings.Cut(version[1:], "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" || strings.Trim(part, "0123456789") != "" {
			return false
		}
	}
	return !hasPre || pre != "" && strings.Trim(pre, "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ.-") == ""
}

// require returns the digests of every platform's archive, or an error naming
// the ones the checksums lack.
func require(version string, sums Sums) (map[Platform]string, error) {
	if !ValidVersion(version) {
		return nil, fmt.Errorf("%q is not a version tag such as v0.1.3", version)
	}
	digests := map[Platform]string{}
	var missing []string
	for _, p := range Platforms {
		name := ArchiveName(version, p)
		digest, ok := sums[name]
		if !ok {
			missing = append(missing, name)
			continue
		}
		digests[p] = digest
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, fmt.Errorf("the checksums lack %s", strings.Join(missing, ", "))
	}
	return digests, nil
}

// Formula renders the Homebrew formula. Homebrew installs on macOS and Linux
// only, so the Windows archives are not in it.
func Formula(version string, sums Sums) (string, error) {
	digests, err := require(version, sums)
	if err != nil {
		return "", err
	}
	if strings.Contains(version, "-") {
		return "", errors.New("a prerelease is not published to Homebrew")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "class Viva < Formula\n")
	fmt.Fprintf(&b, "  desc %q\n", Description)
	fmt.Fprintf(&b, "  homepage %q\n", Homepage)
	fmt.Fprintf(&b, "  version %q\n", strings.TrimPrefix(version, "v"))
	fmt.Fprintf(&b, "  license %q\n\n", License)
	branch := func(keyword string, p Platform) {
		fmt.Fprintf(&b, "    %s do\n", keyword)
		fmt.Fprintf(&b, "      url %q\n", DownloadURL(version, ArchiveName(version, p)))
		fmt.Fprintf(&b, "      sha256 %q\n", digests[p])
		fmt.Fprintf(&b, "    end\n")
	}
	b.WriteString("  on_macos do\n")
	branch("on_arm", Platform{"darwin", "arm64"})
	branch("on_intel", Platform{"darwin", "amd64"})
	b.WriteString("  end\n\n  on_linux do\n")
	branch("on_arm", Platform{"linux", "arm64"})
	branch("on_intel", Platform{"linux", "amd64"})
	b.WriteString("  end\n\n")
	b.WriteString("  def install\n    bin.install \"viva\"\n  end\n\n")
	b.WriteString("  test do\n    assert_match version.to_s, shell_output(\"#{bin}/viva version\")\n  end\nend\n")
	return b.String(), nil
}

type scoopArchitecture struct {
	URL  string `json:"url"`
	Hash string `json:"hash"`
}

// Scoop renders the Scoop manifest for the Windows archives.
func Scoop(version string, sums Sums) (string, error) {
	digests, err := require(version, sums)
	if err != nil {
		return "", err
	}
	if strings.Contains(version, "-") {
		return "", errors.New("a prerelease is not published to Scoop")
	}
	entry := func(arch string) scoopArchitecture {
		p := Platform{"windows", arch}
		return scoopArchitecture{URL: DownloadURL(version, ArchiveName(version, p)), Hash: digests[p]}
	}
	manifest := struct {
		Version      string                       `json:"version"`
		Description  string                       `json:"description"`
		Homepage     string                       `json:"homepage"`
		License      string                       `json:"license"`
		Architecture map[string]scoopArchitecture `json:"architecture"`
		Bin          string                       `json:"bin"`
		CheckVersion map[string]string            `json:"checkver"`
		AutoUpdate   map[string]any               `json:"autoupdate"`
	}{
		Version:      strings.TrimPrefix(version, "v"),
		Description:  Description,
		Homepage:     Homepage,
		License:      License,
		Architecture: map[string]scoopArchitecture{"64bit": entry("amd64"), "arm64": entry("arm64")},
		Bin:          "viva.exe",
		CheckVersion: map[string]string{"github": "https://github.com/" + Repository},
		AutoUpdate: map[string]any{"architecture": map[string]any{
			"64bit": map[string]string{"url": fmt.Sprintf("https://github.com/%s/releases/download/v$version/viva_v$version_windows_amd64.zip", Repository)},
			"arm64": map[string]string{"url": fmt.Sprintf("https://github.com/%s/releases/download/v$version/viva_v$version_windows_arm64.zip", Repository)},
		}},
	}
	out, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", err
	}
	return string(out) + "\n", nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "releasepkg:", err)
		os.Exit(1)
	}
}

func run() error {
	version := flag.String("version", "", "release tag such as v0.1.3")
	sumsPath := flag.String("sums", "", "sha256sum file listing every archive")
	out := flag.String("out", "", "directory to write Formula/ and bucket/ into")
	flag.Parse()
	if *version == "" || *sumsPath == "" || *out == "" {
		return fmt.Errorf("usage: releasepkg -version vX.Y.Z -sums SHA256SUMS -out dir")
	}
	file, err := os.Open(*sumsPath)
	if err != nil {
		return err
	}
	defer file.Close()
	sums, err := ParseSums(file)
	if err != nil {
		return err
	}
	formula, err := Formula(*version, sums)
	if err != nil {
		return err
	}
	scoop, err := Scoop(*version, sums)
	if err != nil {
		return err
	}
	for path, content := range map[string]string{
		filepath.Join(*out, "Formula", "viva.rb"):  formula,
		filepath.Join(*out, "bucket", "viva.json"): scoop,
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return err
		}
		fmt.Println(path)
	}
	return nil
}
