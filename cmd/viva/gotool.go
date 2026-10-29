package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
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
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// viva drives the go tool for build, package, dev and init. When the machine has
// no Go at all, or one too old for Viva's go.mod, viva fetches the official
// toolchain once into the user cache, checks it against the digest go.dev
// publishes, and puts it first on its own PATH. The program being built never
// sees any of this; it is a development convenience, not part of what ships.
//
//	VIVA_GO            a go binary (or the directory of a Go installation) to use
//	VIVA_GO_DOWNLOAD   0 forbids downloading; the command fails with instructions
//	VIVA_GO_MIRROR     base URL of the download index instead of https://go.dev/dl/,
//	                   for example https://golang.google.cn/dl/
//
// Downloads honour HTTP_PROXY/HTTPS_PROXY like any Go program.

// minGoMinor is the oldest 1.N the repository's go.mod accepts; a test keeps it
// equal to the go directive.
const minGoMinor = 26

const defaultGoMirror = "https://go.dev/dl/"

// goArchiveLimit bounds a toolchain download and what it unpacks to.
const goArchiveLimit = 1 << 30

var goReady struct {
	sync.Once
	err error
}

// requireGo makes sure the go command viva runs is new enough, downloading a
// toolchain when it has to. It is cheap after the first call.
func requireGo(errorOutput io.Writer) error {
	goReady.Do(func() { goReady.err = prepareGo(errorOutput) })
	return goReady.err
}

func prepareGo(errorOutput io.Writer) error {
	if given := os.Getenv("VIVA_GO"); given != "" {
		root, err := goRootOf(given)
		if err != nil {
			return fmt.Errorf("VIVA_GO=%s: %w", given, err)
		}
		return useGoRoot(root)
	}
	if path, err := exec.LookPath("go"); err == nil {
		if version, ok := localGoVersion(path); ok && goVersionAtLeast(version, minGoMinor) {
			return nil
		}
	}
	if root := cachedToolchain(goCacheDirectory()); root != "" {
		return useGoRoot(root)
	}
	if os.Getenv("VIVA_GO_DOWNLOAD") == "0" {
		return fmt.Errorf("Go 1.%d or newer was not found and downloading is off (VIVA_GO_DOWNLOAD=0); install it from https://go.dev/dl/ or set VIVA_GO", minGoMinor)
	}
	source := goSource{base: os.Getenv("VIVA_GO_MIRROR"), goos: runtime.GOOS, goarch: runtime.GOARCH}
	root, err := source.install(goCacheDirectory(), errorOutput)
	if err != nil {
		return fmt.Errorf("Go 1.%d or newer was not found and fetching it failed: %w\n"+
			"Install Go from https://go.dev/dl/, or set VIVA_GO_MIRROR (for example https://golang.google.cn/dl/) or HTTPS_PROXY and run again", minGoMinor, err)
	}
	return useGoRoot(root)
}

// goRootOf accepts a go binary or the directory of an installation.
func goRootOf(given string) (string, error) {
	info, err := os.Stat(given)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		if _, err := os.Stat(filepath.Join(given, "bin", goExecutable())); err != nil {
			return "", errors.New("no bin/go inside")
		}
		return given, nil
	}
	return filepath.Dir(filepath.Dir(given)), nil
}

func goExecutable() string { return goExecutableFor(runtime.GOOS) }

func goExecutableFor(goos string) string {
	if goos == "windows" {
		return "go.exe"
	}
	return "go"
}

// useGoRoot puts the installation's go first for this process and its children.
func useGoRoot(root string) error {
	if err := os.Setenv("GOROOT", root); err != nil {
		return err
	}
	return os.Setenv("PATH", filepath.Join(root, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// localGoVersion asks a go binary for its own version, ignoring any toolchain
// switch a go.mod in the working directory would cause.
func localGoVersion(path string) (string, bool) {
	command := exec.Command(path, "env", "GOVERSION")
	command.Env = append(os.Environ(), "GOTOOLCHAIN=local")
	out, err := command.Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(out)), true
}

// parseGoVersion reads "go1.26.1", "go1.26rc1" and "go1.27-devel" as major and minor.
func parseGoVersion(version string) (major, minor int, ok bool) {
	version = strings.TrimPrefix(strings.TrimSpace(version), "devel ")
	version = strings.TrimPrefix(version, "go")
	majorText, rest, found := strings.Cut(version, ".")
	if !found {
		return 0, 0, false
	}
	end := 0
	for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
		end++
	}
	major, err := strconv.Atoi(majorText)
	if err != nil || end == 0 {
		return 0, 0, false
	}
	minor, _ = strconv.Atoi(rest[:end])
	return major, minor, true
}

func goVersionAtLeast(version string, minor int) bool {
	major, got, ok := parseGoVersion(version)
	return ok && (major > 1 || major == 1 && got >= minor)
}

// goCacheDirectory is where downloaded toolchains live: <user cache>/viva/go.
func goCacheDirectory() string {
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, "viva", "go")
}

// cachedToolchain returns the newest complete installation under cache that is
// new enough, or "".
func cachedToolchain(cache string) string {
	entries, err := os.ReadDir(cache)
	if err != nil {
		return ""
	}
	best, bestMinor, bestPatch := "", -1, -1
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "go1.") {
			continue
		}
		if !goVersionAtLeast(entry.Name(), minGoMinor) {
			continue
		}
		if _, err := os.Stat(filepath.Join(cache, entry.Name(), "bin", goExecutable())); err != nil {
			continue
		}
		_, minor, _ := parseGoVersion(entry.Name())
		patch := goPatch(entry.Name())
		if minor > bestMinor || minor == bestMinor && patch > bestPatch {
			best, bestMinor, bestPatch = entry.Name(), minor, patch
		}
	}
	if best == "" {
		return ""
	}
	return filepath.Join(cache, best)
}

// goPatch is the third number of "go1.26.3", 0 when there is none.
func goPatch(version string) int {
	parts := strings.SplitN(strings.TrimPrefix(version, "go"), ".", 3)
	if len(parts) < 3 {
		return 0
	}
	end := 0
	for end < len(parts[2]) && parts[2][end] >= '0' && parts[2][end] <= '9' {
		end++
	}
	patch, _ := strconv.Atoi(parts[2][:end])
	return patch
}

// goSource is where toolchains come from: an index in go.dev's JSON format and
// the archives it names.
type goSource struct {
	base   string // index URL; empty means go.dev
	goos   string
	goarch string
	client *http.Client
}

type goRelease struct {
	Version string   `json:"version"`
	Stable  bool     `json:"stable"`
	Files   []goFile `json:"files"`
}

type goFile struct {
	Filename string `json:"filename"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
	Kind     string `json:"kind"`
}

func (source goSource) baseURL() string {
	base := source.base
	if base == "" {
		base = defaultGoMirror
	}
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	return base
}

func (source goSource) httpClient() *http.Client {
	if source.client != nil {
		return source.client
	}
	return &http.Client{Timeout: 15 * time.Minute}
}

// choose picks the newest stable release that is new enough and has an archive
// for this platform.
func (source goSource) choose() (goRelease, goFile, error) {
	response, err := source.httpClient().Get(source.baseURL() + "?mode=json")
	if err != nil {
		return goRelease{}, goFile{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return goRelease{}, goFile{}, fmt.Errorf("%s: %s", source.baseURL(), response.Status)
	}
	var releases []goRelease
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<20)).Decode(&releases); err != nil {
		return goRelease{}, goFile{}, fmt.Errorf("reading the release index: %w", err)
	}
	sort.SliceStable(releases, func(i, j int) bool {
		_, mi, _ := parseGoVersion(releases[i].Version)
		_, mj, _ := parseGoVersion(releases[j].Version)
		if mi != mj {
			return mi > mj
		}
		return goPatch(releases[i].Version) > goPatch(releases[j].Version)
	})
	for _, release := range releases {
		if !release.Stable || !goVersionAtLeast(release.Version, minGoMinor) {
			continue
		}
		for _, file := range release.Files {
			if file.Kind == "archive" && file.OS == source.goos && file.Arch == source.goarch && file.SHA256 != "" {
				return release, file, nil
			}
		}
	}
	return goRelease{}, goFile{}, fmt.Errorf("the index lists no Go 1.%d or newer archive for %s/%s", minGoMinor, source.goos, source.goarch)
}

// install downloads the toolchain into cache/<version> and returns that directory.
func (source goSource) install(cache string, errorOutput io.Writer) (string, error) {
	release, file, err := source.choose()
	if err != nil {
		return "", err
	}
	destination := filepath.Join(cache, release.Version)
	if _, err := os.Stat(filepath.Join(destination, "bin", goExecutableFor(source.goos))); err == nil {
		return destination, nil
	}
	if err := os.MkdirAll(cache, 0o755); err != nil {
		return "", err
	}
	fmt.Fprintf(errorOutput, "viva: Go 1.%d or newer was not found; downloading %s (%d MB) from %s into %s\n",
		minGoMinor, release.Version, file.Size>>20, source.baseURL(), cache)

	archive, err := os.CreateTemp(cache, ".download-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(archive.Name())
	defer archive.Close()
	if err := source.download(archive, source.baseURL()+file.Filename, file); err != nil {
		return "", err
	}

	staging, err := os.MkdirTemp(cache, ".extract-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(staging)
	if strings.HasSuffix(file.Filename, ".zip") {
		err = extractZip(archive.Name(), staging)
	} else {
		err = extractTarGz(archive, staging)
	}
	if err != nil {
		return "", fmt.Errorf("unpacking %s: %w", file.Filename, err)
	}
	if _, err := os.Stat(filepath.Join(staging, "go", "bin", goExecutableFor(source.goos))); err != nil {
		return "", fmt.Errorf("%s has no go/bin/%s", file.Filename, goExecutableFor(source.goos))
	}
	if err := os.Rename(filepath.Join(staging, "go"), destination); err != nil {
		// A concurrent viva may have finished the same install first.
		if _, statErr := os.Stat(filepath.Join(destination, "bin", goExecutableFor(source.goos))); statErr != nil {
			return "", err
		}
	}
	fmt.Fprintf(errorOutput, "viva: %s is ready\n", release.Version)
	return destination, nil
}

// download writes url to out and fails unless its SHA-256 is the published one.
func (source goSource) download(out *os.File, url string, file goFile) error {
	response, err := source.httpClient().Get(url)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", url, response.Status)
	}
	digest := sha256.New()
	limit := int64(goArchiveLimit)
	written, err := io.Copy(io.MultiWriter(out, digest), io.LimitReader(response.Body, limit+1))
	if err != nil {
		return err
	}
	if written > limit {
		return fmt.Errorf("%s is larger than %d MB", url, limit>>20)
	}
	if got := hex.EncodeToString(digest.Sum(nil)); got != file.SHA256 {
		return fmt.Errorf("%s does not match the published SHA-256 (got %s, want %s)", file.Filename, got, file.SHA256)
	}
	_, err = out.Seek(0, io.SeekStart)
	return err
}

// inside resolves an archive entry name under root, refusing anything that
// would land outside it.
func inside(root, name string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(name))
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("entry %q leaves the extraction directory", name)
	}
	return filepath.Join(root, clean), nil
}

func extractTarGz(source io.Reader, root string) error {
	compressed, err := gzip.NewReader(source)
	if err != nil {
		return err
	}
	defer compressed.Close()
	reader := tar.NewReader(compressed)
	var total int64
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		path, err := inside(root, header.Name)
		if err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(path, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			total += header.Size
			if total > goArchiveLimit*2 {
				return errors.New("archive unpacks to more than the allowed size")
			}
			if err := writeExtracted(path, reader, os.FileMode(header.Mode)&0o777|0o200); err != nil {
				return err
			}
		case tar.TypeSymlink:
			// Only links that stay inside the tree are made.
			target := header.Linkname
			resolved := target
			if !filepath.IsAbs(target) {
				resolved = filepath.Join(filepath.Dir(header.Name), target)
			}
			if _, err := inside(root, resolved); err != nil || filepath.IsAbs(target) {
				continue
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(target, path); err != nil && !errors.Is(err, os.ErrExist) {
				return err
			}
		}
	}
}

func extractZip(archivePath, root string) error {
	archive, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer archive.Close()
	var total int64
	for _, entry := range archive.File {
		path, err := inside(root, entry.Name)
		if err != nil {
			return err
		}
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(path, 0o755); err != nil {
				return err
			}
			continue
		}
		if !entry.Mode().IsRegular() {
			continue
		}
		total += int64(entry.UncompressedSize64)
		if total > goArchiveLimit*2 {
			return errors.New("archive unpacks to more than the allowed size")
		}
		opened, err := entry.Open()
		if err != nil {
			return err
		}
		err = writeExtracted(path, opened, entry.Mode()&0o777|0o200)
		opened.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func writeExtracted(path string, source io.Reader, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(file, source); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}
