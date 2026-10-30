package app

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestFileIconRejectsRequestsItCannotAnswer(t *testing.T) {
	// "/Applications" is not absolute on Windows, which has no drive for it.
	bundle := filepath.Join(t.TempDir(), "Safari.app")
	for _, test := range []struct {
		name, path string
		pixels     int
		want       string
	}{
		{"relative path", "Safari.app", 32, "not absolute"},
		{"NUL in path", bundle + "\x00", 32, "NUL"},
		{"zero size", bundle, 0, "outside"},
		{"negative size", bundle, -1, "outside"},
		{"huge size", bundle, MaxFileIconPixels + 1, "outside"},
	} {
		if _, err := FileIcon(test.path, test.pixels); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: err = %v, want one containing %q", test.name, err, test.want)
		}
	}
}
