package exampleapp

import (
	"fmt"
	"testing"
	"time"

	"github.com/efficientgo/e2e"
	e2edb "github.com/efficientgo/e2e/db"
	e2einteractive "github.com/efficientgo/e2e/interactive"
	"github.com/efficientgo/tools/core/pkg/backoff"
	"github.com/efficientgo/tools/core/pkg/testutil"
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
	testutil.Ok(t, e2e.StartAndWaitReady(app))

	config := fmt.Sprintf(`
global:
  external_labels:
    prometheus: prometheus-example-app
scrape_configs:
- job_name: 'example-app'
  scrape_interval: 1s
  scrape_timeout: 1s
  static_configs:
  - targets: [%s]
  relabel_configs:
  - source_labels: ['__address__']
    regex: '^.+:80$'
    action: drop
`, app.InternalEndpoint("http"))

	fmt.Println("=== Start Prometheus")
	// Create Prometheus instance and wait for it to be ready.
	p1 := e2edb.NewPrometheus(e, "prometheus-1")
	testutil.Ok(t, p1.SetConfig(config))
	testutil.Ok(t, e2e.StartAndWaitReady(p1))

	// Ensure that Prometheis already scraped something.
	testutil.Ok(t, p1.WaitSumMetrics(e2e.Greater(50), "prometheus_tsdb_head_samples_appended_total"))

	// Open example in browser.
	exampleAppURL := fmt.Sprintf("http://%s", app.Endpoint("http"))
	testutil.Ok(t, e2einteractive.OpenInBrowser(exampleAppURL))

	fmt.Println("=== I need at least 5 requests!")
	testutil.Ok(t, app.WaitSumMetricsWithOptions(
		e2e.GreaterOrEqual(5),
		[]string{"http_requests_total"},
		e2e.WithWaitBackoff(
			&backoff.Config{
				Min:        1 * time.Second,
				Max:        10 * time.Second,
				MaxRetries: 100,
			})),
	)

	// Now opening Prometheus in browser as well.
	prometheusURL := fmt.Sprintf("http://%s", p1.Endpoint("http"))
	testutil.Ok(t, e2einteractive.OpenInBrowser(prometheusURL))

	// We're all done!
	fmt.Println("=== Setup finished!")
	fmt.Printf("=== Example application: %s\n", app.Endpoint("http"))

	testutil.Ok(t, e2einteractive.RunUntilEndpointHit())
}
