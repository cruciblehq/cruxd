package paths

import (
	"os"
	"path/filepath"

	"github.com/adrg/xdg"
)

const (

	// Name used for directory and file naming.
	daemonName = "cruxd"

	// Default permission mode for directories.
	DefaultDirMode os.FileMode = 0755

	// Default permission mode for files.
	DefaultFileMode os.FileMode = 0644
)

// Path to the directory for runtime files (sockets, PIDs).
//
//	Linux:   $XDG_RUNTIME_DIR/cruxd or /run/user/<uid>/cruxd
//	macOS:   ~/Library/Caches/cruxd/run
func Runtime() string {
	if xdg.RuntimeDir != "" {
		return filepath.Join(xdg.RuntimeDir, daemonName)
	}
	return filepath.Join(xdg.CacheHome, daemonName, "run")
}

// Default path to the Unix domain socket for CLI-to-daemon communication.
//
//	Linux:   $XDG_RUNTIME_DIR/cruxd/cruxd.sock
//	macOS:   ~/Library/Caches/cruxd/run/cruxd.sock
func Socket() string {
	return filepath.Join(Runtime(), "cruxd.sock")
}

// Default path to the PID file.
//
//	Linux:   $XDG_RUNTIME_DIR/cruxd/cruxd.pid
//	macOS:   ~/Library/Caches/cruxd/run/cruxd.pid
func PIDFile() string {
	return filepath.Join(Runtime(), "cruxd.pid")
}
