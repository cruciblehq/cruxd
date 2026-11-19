package server

// Build identity reported during handshake.
type Build struct {
	Version string // cruxd build version.
	Commit  string // cruxd git commit.
}
