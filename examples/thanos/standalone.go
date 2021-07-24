// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"syscall"

	"github.com/efficientgo/e2e"
	e2edb "github.com/efficientgo/e2e/db"
	e2einteractive "github.com/efficientgo/e2e/interactive"
	"github.com/efficientgo/tools/core/pkg/merrors"
	"github.com/oklog/run"
)

func newThanosQuerier(env e2e.Environment, name string, endpointsAddresses ...string) *e2e.InstrumentedRunnable {
	ports := map[string]int{
		"http": 9090,
		"grpc": 9091,
	}

	args := e2e.BuildArgs(map[string]string{
		"--debug.name":           name,
		"--grpc-address":         fmt.Sprintf(":%d", ports["grpc"]),
		"--http-address":         fmt.Sprintf(":%d", ports["http"]),
		"--query.replica-label":  "replica",
		"--log.level":            "info",
		"--query.max-concurrent": "1",
	})

	for _, e := range endpointsAddresses {
		args = append(args, "--store="+e)
	}
	return e2e.NewInstrumentedRunnable(env, name, ports, "http", e2e.StartOptions{
		Image:     "quay.io/thanos/thanos:v0.21.1",
		Command:   e2e.NewCommand("query", args...),
		Readiness: e2e.NewHTTPReadinessProbe("http", "/-/ready", 200, 200),
		User:      strconv.Itoa(os.Getuid()),
	})
}

func newThanosSidecar(env e2e.Environment, name string, prom e2e.Linkable) *e2e.InstrumentedRunnable {
	ports := map[string]int{
		"http": 9090,
		"grpc": 9091,
	}
	return e2e.NewInstrumentedRunnable(env, name, ports, "http", e2e.StartOptions{
		Image: "quay.io/thanos/thanos:v0.21.1",
		Command: e2e.NewCommand("sidecar", e2e.BuildArgs(map[string]string{
			"--debug.name":     name,
			"--grpc-address":   fmt.Sprintf(":%d", ports["grpc"]),
			"--http-address":   fmt.Sprintf(":%d", ports["http"]),
			"--prometheus.url": "http://" + prom.InternalEndpoint(e2edb.AccessPortName),
			"--log.level":      "info",
		})...),
		Readiness: e2e.NewHTTPReadinessProbe("http", "/-/ready", 200, 200),
		User:      strconv.Itoa(os.Getuid()),
	})
}

func deploy(ctx context.Context) error {
	// Start isolated environment with given ref.
	e, err := e2e.NewDockerEnvironment("e2e_example")
	if err != nil {
		return err
	}
	// Make sure resources (e.g docker containers, network, dir) are cleaned.
	defer e.Close()

	// Create structs for Prometheus containers scraping itself.
	p1, err := e2edb.NewPrometheus(e, "prometheus-1")
	if err != nil {
		return err
	}
	s1 := newThanosSidecar(e, "sidecar-1", p1)

	p2, err := e2edb.NewPrometheus(e, "prometheus-2")
	if err != nil {
		return err
	}
	s2 := newThanosSidecar(e, "sidecar-2", p2)

	// Create Thanos Query container. We can point the peer network addresses of both Prometheus instance
	// using InternalEndpoint methods, even before they started.
	t1 := newThanosQuerier(e, "query-1", s1.InternalEndpoint("grpc"), s2.InternalEndpoint("grpc"))

	// Start them.
	if err := e2e.StartAndWaitReady(p1, s1, p2, s2, t1); err != nil {
		return err
	}

	if err := merrors.New(
		// To ensure query should have access we can check its Prometheus metric using WaitSumMetrics method. Since the metric we are looking for
		// only appears after init, we add option to wait for it.
		t1.WaitSumMetricsWithOptions(e2e.Equals(2), []string{"thanos_store_nodes_grpc_connections"}, e2e.WaitMissingMetrics()),
		// To ensure Prometheus scraped already something ensure number of scrapes.
		p1.WaitSumMetrics(e2e.Greater(50), "prometheus_tsdb_head_samples_appended_total"),
		p2.WaitSumMetrics(e2e.Greater(50), "prometheus_tsdb_head_samples_appended_total"),
	).Err(); err != nil {
		return err
	}

	// We can now open Thanos query UI in our browser, why not! We can use its host address thanks to Endpoint method.
	if err := e2einteractive.OpenInBrowser("http://" + t1.Endpoint("http")); err != nil {
		return err
	}
	// For interactive mode, wait until user interrupt.
	fmt.Println("Waiting on user interrupt (e.g Ctrl+C")
	<-ctx.Done()
	return nil
}

// In order to run it, invoke make run-example from repo root or just go run it.
func main() {
	g := &run.Group{}
	g.Add(run.SignalHandler(context.Background(), syscall.SIGINT, syscall.SIGTERM))
	{
		ctx, cancel := context.WithCancel(context.Background())
		g.Add(func() error { return deploy(ctx) }, func(error) { cancel() })
	}
	if err := g.Run(); err != nil {
		log.Fatal(err)
	}
}
