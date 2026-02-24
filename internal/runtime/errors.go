package runtime

import "errors"

var (
	ErrRuntime    = errors.New("runtime error")
	ErrEmptyIndex = errors.New("empty image index")
)
