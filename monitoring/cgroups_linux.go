// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

//go:build linux
// +build linux

package e2emonitoring

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/efficientgo/e2e"
	"github.com/pkg/errors"
)

const (
	cgroupSubGroup = "e2e"
)

func v2MountPoint() (string, error) {
	f, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return "", err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var (
			text      = scanner.Text()
			fields    = strings.Split(text, " ")
			numFields = len(fields)
		)
		if numFields < 10 {
			return "", fmt.Errorf("mountinfo: bad entry %q", text)
		}
		if fields[numFields-3] == "cgroup2" {
			return fields[4], nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", errors.New("mountpoint does exists")
}

func setupPIDAsContainer(env e2e.Environment, cadvisorRunnable e2e.Runnable, pid int) error {
	mountpoint, err := v2MountPoint()
	if err != nil {
		return errors.Wrap(err, "find v2 mountpoint")
	}

	// Create new nested cgroup and add process to it.
	cmd := fmt.Sprintf("mkdir -p %v && echo %d > %v/cgroup.procs", filepath.Join(mountpoint, cgroupSubGroup, env.Name()), pid, filepath.Join(mountpoint, cgroupSubGroup, env.Name()))

	stdout, stderr, err := cadvisorRunnable.Exec(e2e.NewCommand("sh", "-c", "echo lol"))
	if err != nil {
		return errors.Wrapf(err, "exec: stdout %v; stderr %v", stdout, stderr)
	}

	// Execute it through cadvisor container which has to have all necessary permissions.
	stdout, stderr, err = cadvisorRunnable.Exec(e2e.NewCommand("sh", "-c", strconv.Quote(cmd)))
	if err != nil {
		return errors.Wrapf(err, "exec: stdout %v; stderr %v", stdout, stderr)
	}

	env.AddCloser(func() {
		cmd := fmt.Sprintf("echo %d > %v/cgroup.procs && rmdir %v", pid, filepath.Join(mountpoint, cgroupSubGroup), filepath.Join(mountpoint, cgroupSubGroup, env.Name()))
		stdout, stderr, err := cadvisorRunnable.Exec(e2e.NewCommand("sh", fmt.Sprintf("-c=%v", strconv.Quote(cmd))))
		if err != nil {
			fmt.Println(errors.Wrapf(err, "closer exec: stdout %v; stderr %v", stdout, stderr)) // Best effort.
		}
	})

	return nil
}
