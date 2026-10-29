package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func fullSums(version string) Sums {
	sums := Sums{}
	for i, p := range Platforms {
		sums[ArchiveName(version, p)] = strings.Repeat(string(rune('a'+i)), 64)
	}
	return sums
}

func TestParseSums(t *testing.T) {
	good := strings.Repeat("a", 64)
	sums, err := ParseSums(strings.NewReader(good + "  viva_v1.0.0_linux_amd64.tar.gz\n\n" + good + " *viva_v1.0.0_windows_amd64.zip\n"))
	if err != nil {
		t.Fatal(err)
	}
	if sums["viva_v1.0.0_linux_amd64.tar.gz"] != good || sums["viva_v1.0.0_windows_amd64.zip"] != good {
		t.Errorf("%v", sums)
	}
	for name, input := range map[string]string{
		"short digest": "abc  file\n",
		"upper case":   strings.Repeat("A", 64) + "  file\n",
		"no name":      good + "\n",
		"duplicate":    good + "  f\n" + good + "  f\n",
	} {
		if _, err := ParseSums(strings.NewReader(input)); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestValidVersion(t *testing.T) {
	for v, want := range map[string]bool{
		"v0.1.3": true, "v10.20.30": true, "v0.2.0-rc.1": true,
		"0.1.3": false, "v1.2": false, "v1.2.x": false, "": false, "v1.2.3-": false, "v1.2.3/../x": false, "v1.2.3 ": false,
	} {
		if got := ValidVersion(v); got != want {
			t.Errorf("ValidVersion(%q) = %v, want %v", v, got, want)
		}
	}
}

func TestFormulaNamesTheDigestOfEveryUnixArchive(t *testing.T) {
	const version = "v0.1.3"
	sums := fullSums(version)
	formula, err := Formula(version, sums)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range Platforms {
		name := ArchiveName(version, p)
		inFormula := strings.Contains(formula, DownloadURL(version, name)) && strings.Contains(formula, sums[name])
		if p.OS == "windows" {
			if strings.Contains(formula, name) {
				t.Errorf("Homebrew formula lists the Windows archive %s", name)
			}
		} else if !inFormula {
			t.Errorf("formula lacks %s with its digest:\n%s", name, formula)
		}
	}
	for _, want := range []string{`version "0.1.3"`, `bin.install "viva"`, "on_macos do", "on_linux do", `"#{bin}/viva version"`} {
		if !strings.Contains(formula, want) {
			t.Errorf("formula lacks %q", want)
		}
	}
}

func TestScoopManifestIsValidJSONWithTheWindowsDigests(t *testing.T) {
	const version = "v0.1.3"
	sums := fullSums(version)
	out, err := Scoop(version, sums)
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Version      string
		Bin          string
		Architecture map[string]struct{ URL, Hash string }
	}
	if err := json.Unmarshal([]byte(out), &manifest); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if manifest.Version != "0.1.3" || manifest.Bin != "viva.exe" {
		t.Errorf("%+v", manifest)
	}
	for arch, key := range map[string]string{"amd64": "64bit", "arm64": "arm64"} {
		name := ArchiveName(version, Platform{"windows", arch})
		got := manifest.Architecture[key]
		if got.URL != DownloadURL(version, name) || got.Hash != sums[name] {
			t.Errorf("%s: %+v", key, got)
		}
	}
}

func TestManifestsRefuseWhatTheyCouldNotVouchFor(t *testing.T) {
	const version = "v0.1.3"
	partial := fullSums(version)
	delete(partial, ArchiveName(version, Platform{"linux", "arm64"}))
	for name, call := range map[string]func() error{
		"formula with a missing archive": func() error { _, err := Formula(version, partial); return err },
		"scoop with a missing archive":   func() error { _, err := Scoop(version, partial); return err },
		"bad version":                    func() error { _, err := Formula("main", fullSums("main")); return err },
		"prerelease formula":             func() error { _, err := Formula("v0.2.0-rc.1", fullSums("v0.2.0-rc.1")); return err },
		"prerelease scoop":               func() error { _, err := Scoop("v0.2.0-rc.1", fullSums("v0.2.0-rc.1")); return err },
	} {
		if call() == nil {
			t.Errorf("%s was rendered", name)
		}
	}
	if _, err := Formula(version, partial); err == nil || !strings.Contains(err.Error(), "linux_arm64") {
		t.Errorf("the error must name the missing archive: %v", err)
	}
}
