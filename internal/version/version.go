package version

import (
	"runtime/debug"
	"strings"
)

var (
	Tag    string
	Commit string
)

func String() string {
	tag := strings.TrimSpace(Tag)
	commit := shortHash(strings.TrimSpace(Commit))
	if tag != "" && commit != "" {
		return tag + "+" + commit
	}
	if commit != "" {
		return commit
	}

	buildTag, buildCommit := fromBuildInfo()
	if buildTag != "" && buildCommit != "" {
		return buildTag + "+" + buildCommit
	}
	if buildCommit != "" {
		return buildCommit
	}

	if tag != "" {
		return tag
	}
	return "dev"
}

func fromBuildInfo() (string, string) {
	info, ok := debug.ReadBuildInfo()
	if !ok || info == nil {
		return "", ""
	}
	var tag string
	var commit string
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.tag":
			tag = strings.TrimSpace(setting.Value)
		case "vcs.revision":
			commit = shortHash(strings.TrimSpace(setting.Value))
		}
	}
	return tag, commit
}

func shortHash(s string) string {
	const short = 7
	if len(s) <= short {
		return s
	}
	return s[:short]
}
