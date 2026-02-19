package build

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/cruciblehq/cruxd/internal/runtime"
	"github.com/cruciblehq/go-utils/crex"
)

// Executes a copy operation, transferring files into the container.
//
// The copy string has the format "src dest" for host copies, or "stage:src
// dest" for cross-stage copies. Host sources are resolved relative to the
// build context. Cross-stage sources are read from a named stage container's
// filesystem.
func executeCopy(ctx context.Context, ctr *runtime.Container, copyStr, workdir, buildCtx string, stages map[string]*runtime.Container) error {
	src, dest, err := parseCopy(copyStr, workdir)
	if err != nil {
		return crex.Wrap(ErrCopy, err)
	}

	// Ensure the destination parent directory exists.
	destDir := filepath.Dir(dest)
	if destDir != "" {
		if err := ctr.MkdirAll(ctx, destDir); err != nil {
			return crex.Wrap(ErrCopy, err)
		}
	}

	// Cross-stage copy: "stage:path".
	if stage, path, ok := parseStageCopy(src); ok {
		return executeStageCopy(ctx, ctr, stages, stage, path, dest)
	}

	return executeHostCopy(ctx, ctr, src, dest, buildCtx)
}

// Copies a file or directory from the host into the container.
func executeHostCopy(ctx context.Context, ctr *runtime.Container, src, dest, buildCtx string) error {
	if !filepath.IsAbs(src) {
		src = filepath.Join(buildCtx, src)
	}

	info, err := os.Stat(src)
	if err != nil {
		return crex.Wrap(ErrCopy, err)
	}

	slog.Debug("copy", "src", src, "dest", dest, "dir", info.IsDir())

	pr, pw := io.Pipe()

	go func() {
		tw := tar.NewWriter(pw)
		var writeErr error

		if info.IsDir() {
			writeErr = writeDirToTar(tw, src, filepath.Base(dest))
		} else {
			writeErr = writeFileToTar(tw, src, filepath.Base(dest))
		}

		tw.Close()
		pw.CloseWithError(writeErr)
	}()

	if err := ctr.CopyTo(ctx, pr, filepath.Dir(dest)); err != nil {
		return crex.Wrap(ErrCopy, err)
	}

	return nil
}

// Copies a path from a named stage container into the target container.
//
// The tar stream is piped directly from the source container's CopyFrom
// to the target container's CopyTo.
func executeStageCopy(ctx context.Context, ctr *runtime.Container, stages map[string]*runtime.Container, stage, path, dest string) error {
	srcCtr, ok := stages[stage]
	if !ok {
		return crex.Wrapf(ErrCopy, "unknown stage %q", stage)
	}

	slog.Debug("cross-stage copy", "stage", stage, "src", path, "dest", dest)

	pr, pw := io.Pipe()

	errc := make(chan error, 1)
	go func() {
		errc <- srcCtr.CopyFrom(ctx, pw, path)
		pw.Close()
	}()

	if err := ctr.CopyTo(ctx, pr, filepath.Dir(dest)); err != nil {
		return crex.Wrap(ErrCopy, err)
	}

	if err := <-errc; err != nil {
		return crex.Wrap(ErrCopy, err)
	}

	return nil
}

// Parses a cross-stage copy source of the form "stage:path".
//
// Returns the stage name, the path within the stage, and true if the source
// matches the cross-stage format. Returns false if it is a regular host path.
func parseStageCopy(src string) (stage, path string, ok bool) {
	i := strings.IndexByte(src, ':')
	if i < 1 {
		return "", "", false
	}

	// A colon after a path separator is not a stage prefix (e.g. "/foo:bar").
	if strings.ContainsRune(src[:i], '/') {
		return "", "", false
	}

	return src[:i], src[i+1:], true
}

// Parses a copy string into source and destination paths.
//
// The string must contain exactly two whitespace-separated tokens. If dest
// is not absolute, it is joined with workdir.
func parseCopy(s, workdir string) (src, dest string, err error) {
	parts := strings.Fields(s)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("expected source and destination, got %q", s)
	}

	src = parts[0]
	dest = parts[1]

	if !filepath.IsAbs(dest) {
		if workdir == "" {
			return "", "", fmt.Errorf("relative dest %q requires workdir", dest)
		}
		dest = filepath.Join(workdir, dest)
	}

	return src, dest, nil
}

// Writes a single file to a tar writer with the given archive name.
func writeFileToTar(tw *tar.Writer, hostPath, name string) error {
	info, err := os.Stat(hostPath)
	if err != nil {
		return err
	}

	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return err
	}
	header.Name = name

	if err := tw.WriteHeader(header); err != nil {
		return err
	}

	f, err := os.Open(hostPath)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(tw, f)
	return err
}

// Writes a directory tree to a tar writer rooted at the given archive prefix.
func writeDirToTar(tw *tar.Writer, hostDir, prefix string) error {
	return filepath.WalkDir(hostDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(hostDir, path)
		if err != nil {
			return err
		}

		archivePath := filepath.ToSlash(filepath.Join(prefix, relPath))
		return writeTarEntry(tw, path, archivePath, d)
	})
}

// Writes a single file or directory entry to a tar writer.
func writeTarEntry(tw *tar.Writer, hostPath, archivePath string, d os.DirEntry) error {
	info, err := d.Info()
	if err != nil {
		return err
	}

	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return err
	}
	header.Name = archivePath

	if err := tw.WriteHeader(header); err != nil {
		return err
	}

	if info.Mode().IsRegular() {
		f, err := os.Open(hostPath)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(tw, f)
		return err
	}

	return nil
}
