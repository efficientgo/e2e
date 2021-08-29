// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

package main

import (
	"context"
	"fmt"
	"log"
	"syscall"

	"github.com/efficientgo/e2e"
	e2edb "github.com/efficientgo/e2e/db"
	e2einteractive "github.com/efficientgo/e2e/interactive"
	e2emonitoring "github.com/efficientgo/e2e/monitoring"
	"github.com/efficientgo/tools/core/pkg/merrors"
	"github.com/oklog/run"
	"github.com/pkg/errors"
)

func deployWithMonitoring(ctx context.Context) error {
	// Start isolated environment with given ref.
	e, err := e2e.NewDockerEnvironment("e2e_example")
	if err != nil {
		return err
	}
	// Make sure resources (e.g docker containers, network, dir) are cleaned.
	defer e.Close()

	// NOTE: This will error out on first run, demanding to setup permissions for cgroups.
	// Remove `WithCurrentProcessAsContainer` to avoid that. This will also descope monitoring current process itself
	// and focus on scheduled containers only.
	mon, err := e2emonitoring.Start(e, e2emonitoring.WithCurrentProcessAsContainer())
	if err != nil {
		return err
	}

	// Setup Jaeger for example purposes, on how easy is to setup tracing pipeline in e2e framework.
	j := e.Runnable("tracing").
		WithPorts(
			map[string]int{
				"http.front":    16686,
				"jaeger.thrift": 14268,
			}).
		Init(e2e.StartOptions{Image: "jaegertracing/all-in-one:1.25"})

	jaegerConfig := fmt.Sprintf(
		`type: JAEGER
config:
  service_name: thanos
  sampler_type: const
  sampler_param: 1
  endpoint: http://%s/api/traces`, j.InternalEndpoint("jaeger.thrift"),
	)

	// Create structs for Prometheus containers scraping itself.
	p1 := e2edb.NewPrometheus(e, "prometheus-1")
	s1 := e2edb.NewThanosSidecar(e, "sidecar-1", p1, e2edb.WithFlagOverride(map[string]string{"--tracing.config": jaegerConfig}))

	p2 := e2edb.NewPrometheus(e, "prometheus-2")
	s2 := e2edb.NewThanosSidecar(e, "sidecar-2", p2, e2edb.WithFlagOverride(map[string]string{"--tracing.config": jaegerConfig}))

	// Create Thanos Query container. We can point the peer network addresses of both Prometheus instance
	// using InternalEndpoint methods, even before they started.
	t1 := e2edb.NewThanosQuerier(e, "query-1", []string{s1.InternalEndpoint("grpc"), s2.InternalEndpoint("grpc")}, e2edb.WithFlagOverride(map[string]string{"--tracing.config": jaegerConfig}))

	// Start them.
	if err := e2e.StartAndWaitReady(j, p1, s1, p2, s2, t1); err != nil {
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
		return errors.Wrap(err, "open Thanos UI in browser")
	}
	// Open monitoring page with all metrics.
	if err := mon.OpenUserInterfaceInBrowser(); err != nil {
		return errors.Wrap(err, "open monitoring UI in browser")
	}
	// Open jaeger UI.
	if err := e2einteractive.OpenInBrowser("http://" + j.Endpoint("http.front")); err != nil {
		return errors.Wrap(err, "open Jaeger UI in browser")
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
		g.Add(func() error { return deployWithMonitoring(ctx) }, func(error) { cancel() })
	}
	if err := g.Run(); err != nil {
		log.Fatal(err)
	}
}
