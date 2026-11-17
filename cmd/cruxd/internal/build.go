package internal

import (
	"strconv"

	"github.com/cruciblehq/utils-go/buildinfo"
)

// Values set at link time with -ldflags "-X".
var (
	version   = ""      // Version number (e.g., "1.2.3").
	stage     = ""      // Development stage or git branch (e.g., "main").
	gitCommit = ""      // Git commit hash (e.g., "a1b2c3d4").
	debug     = "false" // Whether debug mode is enabled.
)

// Build identity of this binary.
var Build = buildinfo.New(version, stage, gitCommit)

// Whether debug mode was enabled at link time. Values that do not parse are false.
var Debug, _ = strconv.ParseBool(debug)
