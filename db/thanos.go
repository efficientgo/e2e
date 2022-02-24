// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

package e2edb

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/efficientgo/e2e"
)

func NewThanosQuerier(env e2e.Environment, name string, endpointsAddresses []string, opts ...Option) *e2e.InstrumentedRunnable {
	o := options{image: "quay.io/thanos/thanos:v0.21.1"}
	for _, opt := range opts {
		opt(&o)
	}

	ports := map[string]int{
		"http": 9090,
		"grpc": 9091,
	}

	args := map[string]string{
		"--debug.name":           name,
		"--grpc-address":         fmt.Sprintf(":%d", ports["grpc"]),
		"--http-address":         fmt.Sprintf(":%d", ports["http"]),
		"--query.replica-label":  "replica",
		"--log.level":            "info",
		"--query.max-concurrent": "1",
	}
	if len(endpointsAddresses) > 0 {
		args["--store"] = strings.Join(endpointsAddresses, ",")
	}
	if o.flagOverride != nil {
		args = e2e.MergeFlagsWithoutRemovingEmpty(args, o.flagOverride)
	}

	return e2e.NewInstrumentedRunnable(env, name, ports, "http").Init(e2e.StartOptions{
		Image:     o.image,
		Command:   e2e.NewCommand("query", e2e.BuildKingpinArgs(args)...),
		Readiness: e2e.NewHTTPReadinessProbe("http", "/-/ready", 200, 200),
		User:      strconv.Itoa(os.Getuid()),
	})
}

func NewThanosSidecar(env e2e.Environment, name string, prom e2e.Linkable, opts ...Option) *e2e.InstrumentedRunnable {
	o := options{image: "quay.io/thanos/thanos:v0.21.1"}
	for _, opt := range opts {
		opt(&o)
	}

	ports := map[string]int{
		"http": 9090,
		"grpc": 9091,
	}

	args := map[string]string{
		"--debug.name":     name,
		"--grpc-address":   fmt.Sprintf(":%d", ports["grpc"]),
		"--http-address":   fmt.Sprintf(":%d", ports["http"]),
		"--prometheus.url": "http://" + prom.InternalEndpoint(AccessPortName),
		"--log.level":      "info",
	}
	if o.flagOverride != nil {
		args = e2e.MergeFlagsWithoutRemovingEmpty(args, o.flagOverride)
	}

	return e2e.NewInstrumentedRunnable(env, name, ports, "http").Init(e2e.StartOptions{
		Image:     o.image,
		Command:   e2e.NewCommand("sidecar", e2e.BuildKingpinArgs(args)...),
		Readiness: e2e.NewHTTPReadinessProbe("http", "/-/ready", 200, 200),
		User:      strconv.Itoa(os.Getuid()),
	})
}

func NewThanosStore(env e2e.Environment, name string, bktConfigYaml []byte, opts ...Option) *e2e.InstrumentedRunnable {
	o := options{image: "quay.io/thanos/thanos:v0.21.1"}
	for _, opt := range opts {
		opt(&o)
	}

	ports := map[string]int{
		"http": 9090,
		"grpc": 9091,
	}

	f := e2e.NewInstrumentedRunnable(env, name, ports, "http")
	args := map[string]string{
		"--debug.name":      name,
		"--grpc-address":    fmt.Sprintf(":%d", ports["grpc"]),
		"--http-address":    fmt.Sprintf(":%d", ports["http"]),
		"--log.level":       "info",
		"--data-dir":        f.InternalDir(),
		"--objstore.config": string(bktConfigYaml),
		// Accelerated sync time for quicker test (3m by default).
		//"--sync-block-duration":               "3s",
		"--block-sync-concurrency":            "1",
		"--store.grpc.series-max-concurrency": "1",
		"--consistency-delay":                 "30m",
	}
	if o.flagOverride != nil {
		args = e2e.MergeFlagsWithoutRemovingEmpty(args, o.flagOverride)
	}
	return f.Init(e2e.StartOptions{
		Image:     o.image,
		Command:   e2e.NewCommand("store", e2e.BuildKingpinArgs(args)...),
		Readiness: e2e.NewHTTPReadinessProbe("http", "/-/ready", 200, 200),
		User:      strconv.Itoa(os.Getuid()),
	})
}
