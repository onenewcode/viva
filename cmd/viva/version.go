package main

import (
	"flag"
	"fmt"
	"io"
	"runtime"
	"runtime/debug"
	"strings"
)

// version is set by release builds with -ldflags "-X main.version=v0.1.3". A
// binary installed with go install carries the module version in its build info
// instead; one built from a checkout is "devel".
var version string

// cliVersion is the Viva version this command line was built from.
func cliVersion() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "devel"
}

// releaseVersion reports whether v is a tagged release such as v0.1.2 or
// v0.2.0-rc.1, as opposed to "devel" or the pseudo-version and +dirty suffix Go
// stamps on a build from a checkout. Only a release is compared with a project's
// Viva version: a development build has no version to match.
func releaseVersion(v string) bool {
	if !strings.HasPrefix(v, "v") || strings.Contains(v, "+") {
		return false
	}
	core, pre, hasPre := strings.Cut(v[1:], "-")
	if len(strings.Split(core, ".")) != 3 {
		return false
	}
	for _, part := range strings.Split(core, ".") {
		if part == "" || strings.Trim(part, "0123456789") != "" {
			return false
		}
	}
	// v0.1.3-0.20261028045640-b491d88cbece is a pseudo-version, not a prerelease.
	if hasPre && strings.Count(pre, "-") >= 1 && strings.HasPrefix(pre, "0.") {
		return false
	}
	return true
}

func runVersion(arguments []string, output io.Writer) error {
	c, _ := findCommand("version")
	rest, helped, err := parseFlags(c, flag.NewFlagSet("version", flag.ContinueOnError), arguments, output)
	if helped || err != nil {
		return err
	}
	if len(rest) != 0 {
		return fmt.Errorf("version takes no arguments; usage: viva version")
	}
	fmt.Fprintf(output, "viva %s (%s %s/%s)\n", cliVersion(), runtime.Version(), runtime.GOOS, runtime.GOARCH)
	return nil
}
