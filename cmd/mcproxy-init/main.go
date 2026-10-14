package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

const appUID = 65532

func main() {
	if err := prepareDataDir("/data"); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "mcproxy init: prepare data directory: %v\n", err)
		os.Exit(1)
	}
	// The image normally starts as nonroot now. Keep the privilege-drop path
	// for legacy/root deployments, but do not require a nonroot process to
	// chown or setuid itself (both operations are rejected by the kernel).
	if os.Geteuid() == 0 {
		if err := syscall.Setgid(appUID); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "mcproxy init: drop group privileges: %v\n", err)
			os.Exit(1)
		}
		if err := syscall.Setuid(appUID); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "mcproxy init: drop user privileges: %v\n", err)
			os.Exit(1)
		}
	}

	args := append([]string{"/app/mcproxy"}, os.Args[1:]...)
	if err := syscall.Exec(args[0], args, os.Environ()); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "mcproxy init: exec application: %v\n", err)
		os.Exit(1)
	}
}

func prepareDataDir(root string) error {
	if os.Geteuid() != 0 {
		// Nonroot images cannot repair ownership on mounted volumes. The image
		// layer is already owned by appUID; mounted volumes must be provisioned
		// with compatible ownership by the deployment (or fsGroup in Kubernetes).
		return nil
	}
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if err := os.Chown(path, appUID, appUID); err != nil {
			return err
		}
		return nil
	})
}
