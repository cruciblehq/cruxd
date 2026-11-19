package identity

import "github.com/cruciblehq/utils-go/crex"

var (
	ErrNoEnrollment    = crex.New("no enrollment data")
	ErrAlreadyEnrolled = crex.New("already enrolled")
	ErrInvalidState    = crex.New("invalid identity state")
	ErrInvalidCert     = crex.New("invalid agent certificate")
)
