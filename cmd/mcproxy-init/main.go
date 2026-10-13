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
	if err := syscall.Setgid(appUID); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "mcproxy init: drop group privileges: %v\n", err)
		os.Exit(1)
	}
	if err := syscall.Setuid(appUID); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "mcproxy init: drop user privileges: %v\n", err)
		os.Exit(1)
	}

	args := append([]string{"/app/mcproxy"}, os.Args[1:]...)
	if err := syscall.Exec(args[0], args, os.Environ()); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "mcproxy init: exec application: %v\n", err)
		os.Exit(1)
	}
}

func prepareDataDir(root string) error {
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
