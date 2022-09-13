// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

package main

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/efficientgo/core/testutil"
	"github.com/efficientgo/e2e"
	e2edb "github.com/efficientgo/e2e/db"
	e2e2 "github.com/efficientgo/e2e/monitoring"
	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

func TestExample(t *testing.T) {
	t.Parallel() // We can run those tests in parallel (as long as host has enough CPU time).

	// Start isolated environment with given ref.
	e, err := e2e.New()
	testutil.Ok(t, err)
	// Make sure resources (e.g docker containers, network, dir) are cleaned.
	t.Cleanup(e.Close)

	// Create structs for Prometheus containers scraping itself.
	p1 := e2edb.NewPrometheus(e, "prometheus-1")
	s1 := e2edb.NewThanosSidecar(e, "sidecar-1", p1)

	p2 := e2edb.NewPrometheus(e, "prometheus-2")
	s2 := e2edb.NewThanosSidecar(e, "sidecar-2", p2)

	// Create Thanos Query container. We can point the peer network addresses of both Prometheus instance
	// using InternalEndpoint methods, even before they started.
	t1 := e2edb.NewThanosQuerier(e, "query-1", []string{s1.InternalEndpoint("grpc"), s2.InternalEndpoint("grpc")})

	// Start them.
	testutil.Ok(t, e2e.StartAndWaitReady(p1, s1, p2, s2, t1))

	// To ensure query should have access we can check its Prometheus metric using WaitSumMetrics method. Since the metric we are looking for
	// only appears after init, we add option to wait for it.
	testutil.Ok(t, t1.WaitSumMetricsWithOptions(e2e2.Equals(2), []string{"thanos_store_nodes_grpc_connections"}, e2e2.WaitMissingMetrics()))

	// To ensure Prometheus scraped already something ensure number of scrapes.
	testutil.Ok(t, p1.WaitSumMetrics(e2e2.Greater(50), "prometheus_tsdb_head_samples_appended_total"))
	testutil.Ok(t, p2.WaitSumMetrics(e2e2.Greater(50), "prometheus_tsdb_head_samples_appended_total"))

	// We can now query Thanos Querier directly from here, using it's host address thanks to Endpoint method.
	a, err := api.NewClient(api.Config{Address: "http://" + t1.Endpoint("http")})
	testutil.Ok(t, err)

	{
		now := model.Now()
		v, w, err := v1.NewAPI(a).Query(context.Background(), "up{}", now.Time())
		testutil.Ok(t, err)
		testutil.Equals(t, 0, len(w))
		testutil.Equals(
			t,
			fmt.Sprintf(`up{instance="%v", job="myself", prometheus="prometheus-1"} => 1 @[%v]
up{instance="%v", job="myself", prometheus="prometheus-2"} => 1 @[%v]`, p1.InternalEndpoint(e2edb.AccessPortName), now, p2.InternalEndpoint(e2edb.AccessPortName), now),
			v.String(),
		)
	}

	// Stop first Prometheus and sidecar.
	testutil.Ok(t, s1.Stop())
	testutil.Ok(t, p1.Stop())

	// Wait a bit until Thanos drops connection to stopped Prometheus.
	testutil.Ok(t, t1.WaitSumMetricsWithOptions(e2e2.Equals(1), []string{"thanos_store_nodes_grpc_connections"}, e2e2.WaitMissingMetrics()))

	{
		now := model.Now()
		v, w, err := v1.NewAPI(a).Query(context.Background(), "up{}", now.Time())
		testutil.Ok(t, err)
		testutil.Equals(t, 0, len(w))
		testutil.Equals(
			t,
			fmt.Sprintf(`up{instance="%v", job="myself", prometheus="prometheus-2"} => 1 @[%v]`, p2.InternalEndpoint(e2edb.AccessPortName), now),
			v.String(),
		)
	}

	// Batch job example.
	batch := e.Runnable("batch").Init(e2e.StartOptions{Image: "ubuntu:20.04", Command: e2e.NewCommandRunUntilStop()})
	testutil.Ok(t, batch.Start())

	var out bytes.Buffer
	testutil.Ok(t, batch.Exec(e2e.NewCommand("echo", "it works"), e2e.WithExecOptionStdout(&out)))
	testutil.Equals(t, "it works\n", out.String())
}
