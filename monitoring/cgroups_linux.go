// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

package e2emonitoring

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
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

func setupPIDAsContainer(env e2e.Environment, currCgroup string, pid int) ([]string, error) {
	mountpoint, err := v2MountPoint()
	if err != nil {
		return nil, errors.Wrap(err, "find v2 mountpoint")
	}

	fmt.Println(os.Getuid())
	// TODO(bwplotka): Make sure multiple runners would work.
	if err := os.MkdirAll(filepath.Join(mountpoint, cgroupSubGroup, env.Name()), os.ModePerm); err != nil {
		if !os.IsPermission(err) {
			return nil, err
		}

		// sudo mkdir /sys/fs/cgroup/e2e && sudo chown -R 1000 /sys/fs/cgroup/e2e
		fmt.Println("perms")
		return nil, err

	}
	// Enable controllers, see https://www.kernel.org/doc/html/latest/admin-guide/cgroup-v2.html
	if err := ioutil.WriteFile(filepath.Join(mountpoint, cgroupSubGroup, "cgroup.subtree_control"), []byte("+cpu +memory"), os.ModeAppend); err != nil {
		return nil, err
	}
	fmt.Println("PUD", pid)
	//if err := ioutil.WriteFile(filepath.Join(mountpoint, cgroupSubGroup, "cgroup.procs"), []byte(fmt.Sprintf("%v", pid)), os.ModeAppend); err != nil {
	//	return nil, err
	//}
	//if err := ioutil.WriteFile(filepath.Join(mountpoint, cgroupSubGroup, env.Name(), "cgroup.procs"), []byte(fmt.Sprintf("%v", pid)), os.ModeAppend); err != nil {
	//	return nil, err
	//}

	env.AddCloser(func() {
		if err := ioutil.WriteFile(filepath.Join(mountpoint, currCgroup, "cgroup.procs"), []byte(fmt.Sprintf("%v", pid)), os.ModeAppend); err != nil {
			fmt.Println(err) // Best effort.
		}
		if err := os.RemoveAll(filepath.Join(mountpoint, cgroupSubGroup, env.Name())); err != nil {
			fmt.Println(err) // Best effort.
		}
	})

	return []string{filepath.Join(cgroupSubGroup, env.Name())}, nil
}
