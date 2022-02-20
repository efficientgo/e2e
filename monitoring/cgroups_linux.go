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
	"strings"

	cgroups "github.com/containerd/cgroups/v2"
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

func setupPIDAsContainer(env e2e.Environment, pid int) ([]string, error) {
	mountpoint, err := v2MountPoint()
	if err != nil {
		return nil, errors.Wrap(err, "find v2 mountpoint")
	}

	// Try to create test cgroup to check if we have permission.
	{
		mgr, err := cgroups.NewManager(mountpoint, "/"+filepath.Join(cgroupSubGroup, "__test__"), &cgroups.Resources{})
		if err != nil {
			if !os.IsPermission(err) {
				return nil, errors.Wrap(err, "new test cgroup")
			}

			uid := os.Getuid()
			cmds := fmt.Sprintf("sudo mkdir -p %s && sudo chown -R %d %s",
				filepath.Join(mountpoint, cgroupSubGroup),
				uid,
				filepath.Join(mountpoint, cgroupSubGroup),
			)
			return nil, errors.Errorf("e2e does not have permissions, run following command: %q; err: %v", cmds, err)
		}
		if err := mgr.Delete(); err != nil {
			return nil, errors.Wrap(err, "delete test")
		}
	}

	// Delete previous cgroup if it exists.
	mgr, err := cgroups.LoadManager(mountpoint, "/"+filepath.Join(cgroupSubGroup, env.Name()))
	if err != nil {
		// Deleted?
		return nil, errors.Wrap(err, "load cgroup")
	} else {
		if err := mgr.Freeze(); err != nil {
			return nil, errors.Wrap(err, "freeze")
		}
		if err := mgr.Delete(); err != nil {
			return nil, errors.Wrap(err, "delete")
		}
	}

	// Create cgroup that will contain our process.
	mgr, err = cgroups.NewManager(mountpoint, "/"+filepath.Join(cgroupSubGroup, env.Name()), &cgroups.Resources{})
	if err != nil {
		return nil, errors.Wrap(err, "new v2 manager")
	}

	if err := mgr.AddProc(uint64(pid)); err != nil {
		return nil, errors.Wrap(err, "add proc")
	}

	env.AddCloser(func() {
		mgr, err := cgroups.LoadManager(mountpoint, "/"+filepath.Join(cgroupSubGroup, env.Name()))
		if err != nil {
			// Deleted?
			fmt.Println("Failed to load cgroup", err)
			return
		}
		if err := mgr.Delete(); err != nil {
			// TODO(bwplotka): This never works, but not very important, fix it.
			fmt.Println("Failed to delete cgroup", err)
		}
	})

	return []string{filepath.Join("/", cgroupSubGroup, env.Name())}, nil
}
