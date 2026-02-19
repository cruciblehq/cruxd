// Parses flags and configures logging for the cruxd daemon.
//
// The daemon accepts the following flags:
//
//	-q, --quiet     Suppress informational output.
//	-v, --verbose   Enable verbose output.
//	-d, --debug     Enable debug output.
//	-s, --socket    Unix socket path.
//
// Flags override build-time defaults set via linker flags. After parsing, the
// global logger is reconfigured to reflect the final level and verbosity before
// the server starts.
package cli
