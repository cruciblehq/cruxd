package runtime

import "errors"

var (
	ErrRuntime        = errors.New("runtime error")
	ErrEmptyArchive   = errors.New("no images found in archive")
	ErrMultipleImages = errors.New("archive contains multiple images")
)
