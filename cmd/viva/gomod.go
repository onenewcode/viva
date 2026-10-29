package main

import (
	"strings"
)

const vivaModule = "github.com/vivaui/viva"

// vivaDependency is what a project's go.mod says about Viva.
type vivaDependency struct {
	Version  string // the required version, "" when the module is not required
	Replaced bool   // a replace directive points the module somewhere else
	Tool     bool   // a tool directive pins the viva command to this module's version
}

// parseVivaDependency reads the Viva lines of a go.mod. It understands the
// single-line and block forms of require, replace and tool, which is all a
// go.mod written by the go command contains.
func parseVivaDependency(goMod string) vivaDependency {
	var dep vivaDependency
	block := ""
	for _, line := range strings.Split(goMod, "\n") {
		if comment := strings.Index(line, "//"); comment >= 0 {
			line = line[:comment]
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if block != "" {
			if fields[0] == ")" {
				block = ""
				continue
			}
			dep.apply(block, fields)
			continue
		}
		if len(fields) == 2 && fields[1] == "(" {
			block = fields[0]
			continue
		}
		if len(fields) >= 2 {
			dep.apply(fields[0], fields[1:])
		}
	}
	return dep
}

func (dep *vivaDependency) apply(directive string, fields []string) {
	switch directive {
	case "require":
		if len(fields) >= 2 && fields[0] == vivaModule {
			dep.Version = fields[1]
		}
	case "replace":
		if fields[0] == vivaModule {
			dep.Replaced = true
		}
	case "tool":
		if fields[0] == vivaModule+"/cmd/viva" {
			dep.Tool = true
		}
	}
}
