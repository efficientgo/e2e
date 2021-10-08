// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

//go:build linux
// +build linux

package e2emonitoring

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/containerd/cgroups"
	"github.com/efficientgo/e2e"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pkg/errors"
)

const (
	mountpoint     = "/sys/fs/cgroup"
	cgroupSubGroup = "e2e"
)

func setupPIDAsContainer(env e2e.Environment, pid int) ([]string, error) {
	// Try to setup test cgroup to check if we have perms.
	{
		c, err := cgroups.New(cgroups.V1, cgroups.StaticPath(filepath.Join(cgroupSubGroup, "__test__")), &specs.LinuxResources{})
		if err != nil {
			if !os.IsPermission(err) {
				return nil, err
			}

			uid := os.Getuid()

			var cmds []string

			ss, cerr := cgroups.V1()
			if cerr != nil {
				return nil, cerr
			}

			for _, s := range ss {
				cmds = append(cmds, fmt.Sprintf("sudo mkdir -p %s && sudo chown -R %d %s",
					filepath.Join(mountpoint, string(s.Name()), cgroupSubGroup),
					uid,
					filepath.Join(mountpoint, string(s.Name()), cgroupSubGroup),
				))
			}
			return nil, errors.Errorf("e2e does not have permissions, run following command: %q; err: %v", strings.Join(cmds, " && "), err)
		}
		if err := c.Delete(); err != nil {
			return nil, err
		}
	}

	// Delete previous cgroup if it exists.
	root, err := cgroups.Load(cgroups.V1, cgroups.RootPath)
	if err != nil {
		return nil, err
	}

	l, err := cgroups.Load(cgroups.V1, cgroups.StaticPath(filepath.Join(cgroupSubGroup, env.Name())))
	if err != nil {
		if err != cgroups.ErrCgroupDeleted {
			return nil, err
		}
	} else {
		if err := l.MoveTo(root); err != nil {
			return nil, err
		}
		if err := l.Delete(); err != nil {
			return nil, err
		}
	}

	// Create cgroup that will contain our process.
	c, err := cgroups.New(cgroups.V1, cgroups.StaticPath(filepath.Join(cgroupSubGroup, env.Name())), &specs.LinuxResources{})
	if err != nil {
		return nil, err
	}
	if err := c.Add(cgroups.Process{Pid: pid}); err != nil {
		return nil, err
	}
	env.AddCloser(func() {
		l, err := cgroups.Load(cgroups.V1, cgroups.StaticPath(filepath.Join(cgroupSubGroup, env.Name())))
		if err != nil {
			if err != cgroups.ErrCgroupDeleted {
				// All good.
				return
			}
			fmt.Println("Failed to load cgroup", err)
		}
		if err := l.MoveTo(root); err != nil {
			fmt.Println("Failed to move all processes", err)
		}
		if err := c.Delete(); err != nil {
			// TODO(bwplotka): This never works, but not very important, fix it.
			fmt.Println("Failed to delete cgroup", err)
		}
	})

	return []string{filepath.Join("/", cgroupSubGroup, env.Name())}, nil
}
