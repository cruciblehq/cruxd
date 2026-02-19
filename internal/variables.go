package internal

import (
	"fmt"
	"runtime"
	"strings"
)

const (

	// String to indicate an undefined variable
	defaultUndefined = "(undefined)"

	// String to indicate a local (non-pipeline) build
	defaultLocalBuild = "(local)"

	// Main branch name used in version strings
	mainBranch = "main"
)

var (
	version   = "" // Version number (e.g., "1.2.3")
	stage     = "" // Development stage or git branch (e.g., "staging", "main")
	gitCommit = "" // Git commit hash (e.g., "a1b2c3d4")

	rawQuiet   = "false" // Whether to enable quiet mode
	rawDebug   = "false" // Whether to enable debug mode
	rawVerbose = "false" // Whether to enable verbose logging
)

// Returns the current version.
//
// If the version is not set, returns "(undefined)". If the version includes a
// "v" or "V" prefix (e.g., "v1.0.0"), it is stripped.
func Version() string {
	v := strings.TrimSpace(version)
	if v == "" {
		return defaultUndefined
	}

	v = strings.ToLower(v)
	v = strings.TrimPrefix(v, "v")

	return v
}

// Returns the development stage (e.g., "alpha").
//
// The development should correspond to the git branch name used during the
// build. If it is not set, returns "(undefined)".
func Stage() string {
	s := strings.TrimSpace(stage)
	if s == "" {
		return defaultUndefined
	}
	return strings.ToLower(s)
}

// Returns the git commit hash.
//
// If the commit hash is not set, returns "(undefined)".
func GitCommit() string {
	c := strings.TrimSpace(gitCommit)
	if c == "" {
		return defaultUndefined
	}
	return c
}

// Returns the build architecture.
func Arch() string {
	return runtime.GOARCH
}

// Returns true if this is a local (non-pipeline) build.
//
// A build is considered local if any of the version, git commit, or stage
// variables are unset. Pipeline builds should set all three variables via
// linker flags.
func IsLocal() bool {
	return strings.TrimSpace(version) == "" ||
		strings.TrimSpace(gitCommit) == "" ||
		strings.TrimSpace(stage) == ""
}

// Returns a detailed version string.
//
// If this is a local build, returns "(local)". Otherwise, returns a string
// formatted as "<version>+<stage> <git-commit> [<arch>]".
func VersionString() string {
	if IsLocal() {
		return defaultLocalBuild
	}

	s := Stage()
	if s == mainBranch {
		s = ""
	} else {
		s = "+" + s
	}

	return fmt.Sprintf("%s%s %s [%s]", Version(), s, GitCommit(), Arch())
}
