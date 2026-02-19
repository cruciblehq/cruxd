package internal

import (
	"strconv"
	"sync/atomic"
)

var (
	quietMode   atomic.Bool // Indicates whether quiet mode is enabled.
	debugMode   atomic.Bool // Indicates whether debug logging is enabled.
	verboseMode atomic.Bool // Indicates whether verbose logging is enabled.
)

// Parses the linker flags into usable runtime variables.
//
// The rawQuiet, rawDebug, and rawVerbose variables should be set via ldflags
// during the build process. If not set, they default to "false".
func init() {
	if v, err := strconv.ParseBool(rawQuiet); err == nil {
		quietMode.Store(v)
	}
	if v, err := strconv.ParseBool(rawDebug); err == nil {
		debugMode.Store(v)
	}
	if v, err := strconv.ParseBool(rawVerbose); err == nil {
		verboseMode.Store(v)
	}
}

// Enables or disables quiet mode.
func SetQuiet(enabled bool) {
	quietMode.Store(enabled)
}

// Returns true if quiet mode is enabled.
func IsQuiet() bool {
	return quietMode.Load()
}

// Enables or disables debug mode.
func SetDebug(enabled bool) {
	debugMode.Store(enabled)
}

// Returns true if debug mode is enabled.
func IsDebug() bool {
	return debugMode.Load()
}

// Enables or disables verbose logging.
func SetVerbose(enabled bool) {
	verboseMode.Store(enabled)
}

// Returns true if verbose logging is enabled.
func IsVerbose() bool {
	return verboseMode.Load()
}
