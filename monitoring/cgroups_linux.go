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
			return filepath.Dir(fields[4]), nil
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

	mgr, err := cgroups.LoadManager(mountpoint, filepath.Join(cgroupSubGroup, env.Name()))
	if err != nil {
		return nil, errors.Wrap(err, "new v2 manager")
	}

	if err := mgr.AddProc(uint64(pid)); err != nil {
		return nil, errors.Wrap(err, "add proc")
	}

	//// Try to setup test cgroup to check if we have perms.
	//{
	//	c, err := cgroups.New(cgroups.V1, cgroups.StaticPath(filepath.Join(cgroupSubGroup, "__test__")), &specs.LinuxResources{})
	//	if err != nil {
	//		if !os.IsPermission(err) {
	//			return nil, errors.Wrap(err, "new test cgroup")
	//		}
	//
	//		uid := os.Getuid()
	//
	//		var cmds []string
	//
	//		ss, cerr := cgroups.V1()
	//		if cerr != nil {
	//			return nil, errors.Wrap(cerr, "access v1 test")
	//		}
	//
	//		for _, s := range ss {
	//			cmds = append(cmds, fmt.Sprintf("sudo mkdir -p %s && sudo chown -R %d %s",
	//				filepath.Join(mountpoint, string(s.Name()), cgroupSubGroup),
	//				uid,
	//				filepath.Join(mountpoint, string(s.Name()), cgroupSubGroup),
	//			))
	//		}
	//		return nil, errors.Errorf("e2e does not have permissions, run following command: %q; err: %v", strings.Join(cmds, " && "), err)
	//	}
	//	if err := c.Delete(); err != nil {
	//		return nil, errors.Wrap(err, "delete test")
	//	}
	//}
	//
	//// Delete previous cgroup if it exists.
	//root, err := cgroups.Load(cgroups.V1, cgroups.RootPath)
	//if err != nil {
	//	return nil, errors.Wrap(err, "load root")
	//}
	//
	//l, err := cgroups.Load(cgroups.V1, cgroups.StaticPath(filepath.Join(cgroupSubGroup, env.Name())))
	//if err != nil {
	//	if err != cgroups.ErrCgroupDeleted {
	//		return nil, err
	//	}
	//} else {
	//	if err := l.MoveTo(root); err != nil {
	//		return nil, err
	//	}
	//	if err := l.Delete(); err != nil {
	//		return nil, err
	//	}
	//}
	//
	//// Create cgroup that will contain our process.
	//c, err := cgroups.New(cgroups.V1, cgroups.StaticPath(filepath.Join(cgroupSubGroup, env.Name())), &specs.LinuxResources{})
	//if err != nil {
	//	return nil, err
	//}
	//if err := c.Add(cgroups.Process{Pid: pid}); err != nil {
	//	return nil, err
	//}
	//env.AddCloser(func() {
	//	l, err := cgroups.Load(cgroups.V1, cgroups.StaticPath(filepath.Join(cgroupSubGroup, env.Name())))
	//	if err != nil {
	//		if err != cgroups.ErrCgroupDeleted {
	//			// All good.
	//			return
	//		}
	//		fmt.Println("Failed to load cgroup", err)
	//	}
	//	if err := l.MoveTo(root); err != nil {
	//		fmt.Println("Failed to move all processes", err)
	//	}
	//	if err := c.Delete(); err != nil {
	//		// TODO(bwplotka): This never works, but not very important, fix it.
	//		fmt.Println("Failed to delete cgroup", err)
	//	}
	//})

	return []string{filepath.Join("/", cgroupSubGroup, env.Name())}, nil
}
