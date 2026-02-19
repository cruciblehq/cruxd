// Package runtime manages containers backed by containerd.
//
// A [Runtime] connects to a containerd daemon and provides image import
// and container creation. OCI archives are imported, tagged with a
// deterministic content hash, unpacked for the target platform, and used
// to create containers with overlayfs snapshots.
//
// Each [Container] wraps a running containerd task. Commands can be
// executed inside the container, files can be copied in and out as tar
// streams, and the final filesystem state can be committed and exported
// as a new OCI archive. When the container is no longer needed it should
// be destroyed to release its snapshot and task resources.
//
// Example usage:
//
//	rt, err := runtime.New("/run/containerd/containerd.sock", "crucible")
//	if err != nil {
//	    return err
//	}
//	defer rt.Close()
//
//	ctr, err := rt.StartContainer(ctx, "image.tar", "build-1", "linux/amd64")
//	if err != nil {
//	    return err
//	}
//	defer ctr.Destroy(ctx)
//
//	result, err := ctr.Exec(ctx, "/bin/sh", "echo hello", nil, "")
//	if err != nil {
//	    return err
//	}
//
//	if err := ctr.Export(ctx, "output", []string{"/entrypoint"}); err != nil {
//	    return err
//	}
package runtime
