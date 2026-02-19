package runtime

import (
	"context"
	"io"
	"path/filepath"

	"github.com/cruciblehq/go-utils/crex"
)

// Creates a directory inside the container, including parents.
func (c *Container) MkdirAll(ctx context.Context, path string) error {
	return c.mustExec(ctx, "mkdir", nil, nil, "mkdir", "-p", path)
}

// Copies a tar stream into the container's filesystem.
//
// The contents of r are extracted into destDir by piping them to "tar xf - -C
// destDir" inside the container.
func (c *Container) CopyTo(ctx context.Context, r io.Reader, destDir string) error {
	return c.mustExec(ctx, "tar extract", r, nil, "tar", "xf", "-", "-C", destDir)
}

// Copies a path from the container's filesystem as a tar stream.
//
// The file or directory at path is archived by running "tar cf - -C <dir>
// <base>" inside the container and streaming the output to w.
func (c *Container) CopyFrom(ctx context.Context, w io.Writer, path string) error {
	return c.mustExec(ctx, "tar archive", nil, w, "tar", "cf", "-", "-C", filepath.Dir(path), filepath.Base(path))
}

// Helper method that runs a command inside the container, returning an error
// that includes desc if the process exits with a non-zero code.
func (c *Container) mustExec(ctx context.Context, desc string, stdin io.Reader, stdout io.Writer, args ...string) error {
	exitCode, stderr, err := c.execCommand(ctx, stdin, stdout, nil, "", args...)
	if err != nil {
		return err
	}
	if exitCode != 0 {
		return crex.Wrapf(ErrRuntime, "%s failed with exit code %d (%s)", desc, exitCode, stderr)
	}
	return nil
}
