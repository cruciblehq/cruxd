package internal

import (
	"strconv"
	"sync/atomic"
)

var (
	rawQuiet = "false" // Build-time ldflags string for quiet mode.
	rawDebug = "false" // Build-time ldflags string for debug mode.

	quietMode atomic.Bool // Parsed quiet mode flag, safe for concurrent reads.
	debugMode atomic.Bool // Parsed debug mode flag, safe for concurrent reads.
)

// Parses the linker flags into usable runtime variables.
//
// rawQuiet and rawDebug should be set via ldflags during the build process.
// If not set, they default to "false".
func init() {
	if v, err := strconv.ParseBool(rawQuiet); err == nil {
		quietMode.Store(v)
	}
	if v, err := strconv.ParseBool(rawDebug); err == nil {
		debugMode.Store(v)
	}
}

// Whether quiet mode is enabled.
func IsQuiet() bool {
	return quietMode.Load()
}

// Whether debug mode is enabled.
func IsDebug() bool {
	return debugMode.Load()
}
