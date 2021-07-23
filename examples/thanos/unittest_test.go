package main

import (
	"testing"

	"github.com/efficientgo/e2e"
	e2edb "github.com/efficientgo/e2e/db"
	"github.com/efficientgo/tools/core/pkg/testutil"
)

func TestExampleThanos(t *testing.T) {
	// Start isolated environment with given ref.
	e, err := e2e.NewDockerEnvironment("e2e_example")
	testutil.Ok(t, err)
	// Make sure resources (e.g docker containers, network, dir) are cleaned.
	t.Cleanup(e.Close)

	// Create structs for Prometheus containers scraping itself.
	p1, err := e2edb.NewPrometheus(e, "prometheus-1")
	testutil.Ok(t, err)
	p2, err := e2edb.NewPrometheus(e, "prometheus-2")
	testutil.Ok(t, err)

	// Create Thanos Query container.
	t1 :=

		// Start them.
		testutil.Ok(t, e2e.StartAndWaitReady(p1, p2))

	// Feel free to reach any of the containers from host using Endpoint method.

	// Stop first Prometheus.
	testutil.Ok(t, p1.Stop())

}
