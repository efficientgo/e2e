package exampleapp

import (
	"fmt"
	"github.com/efficientgo/e2e"
	e2edb "github.com/efficientgo/e2e/db"
	e2einteractive "github.com/efficientgo/e2e/interactive"
	e2emonitoring "github.com/efficientgo/e2e/monitoring"
	"github.com/efficientgo/tools/core/pkg/testutil"
	"testing"
)

func TestExampleApp(t *testing.T) {
	// Start a new Docker environment.
	e, err := e2e.NewDockerEnvironment("e2e_example_app")
	testutil.Ok(t, err)
	// Make sure resources are cleaned up.
	t.Cleanup(e.Close)

	fmt.Println("=== Start example application...")
	app := e2e.NewInstrumentedRunnable(e, "example_app").
		WithPorts(map[string]int{"http": 8080}, "http").
		Init(e2e.StartOptions{
			Image: "quay.io/brancz/prometheus-example-app:v0.3.0",
		})
	testutil.Ok(t, app.Start())

	fmt.Println("=== Start 2 Prometheis")
	// Create 2 Prometheis.
	p1 := e2edb.NewPrometheus(e, "prometheus-1")
	p2 := e2edb.NewPrometheus(e, "prometheus-2")

	testutil.Ok(t, e2e.StartAndWaitReady(p1, p2))

	// Ensure that Prometheus already scraped something.
	testutil.Ok(t, p1.WaitSumMetrics(e2e.Greater(50), "prometheus_tsdb_head_samples_appended_total"))
	testutil.Ok(t, p2.WaitSumMetrics(e2e.Greater(50), "prometheus_tsdb_head_samples_appended_total"))

	mon, err := e2emonitoring.Start(e)
	// Open Prometheus UI.
	testutil.Ok(t, mon.OpenUserInterfaceInBrowser())

	// Open example app url
	exampleAppURL := fmt.Sprintf("http://%s", app.Endpoint("http"))
	testutil.Ok(t, e2einteractive.OpenInBrowser(exampleAppURL))

	fmt.Println("=== Setup finished!")
	fmt.Printf("=== Example application: %s\n", app.Endpoint("http"))

	testutil.Ok(t, e2einteractive.RunUntilEndpointHit())
}
