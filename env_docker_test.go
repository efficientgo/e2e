package e2e_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/efficientgo/e2e"
	"github.com/efficientgo/tools/core/pkg/testutil"
	"github.com/pkg/errors"
)

func promConfig(name string, replica int, remoteWriteEntry, ruleFile string, scrapeTargets ...string) string {
	targets := "localhost:9090"
	if len(scrapeTargets) > 0 {
		targets = strings.Join(scrapeTargets, ",")
	}
	config := fmt.Sprintf(`
global:
  external_labels:
    prometheus: %v
    replica: %v
scrape_configs:
- job_name: 'myself'
  # Quick scrapes for test purposes.
  scrape_interval: 1s
  scrape_timeout: 1s
  static_configs:
  - targets: [%s]
  relabel_configs:
  - source_labels: ['__address__']
    regex: '^.+:80$'
    action: drop
`, name, replica, targets)

	if remoteWriteEntry != "" {
		config = fmt.Sprintf(`
%s
%s
`, config, remoteWriteEntry)
	}

	if ruleFile != "" {
		config = fmt.Sprintf(`
%s
rule_files:
-  "%s"
`, config, ruleFile)
	}

	return config
}

func newPrometheus(env e2e.Environment, name string) (*e2e.InstrumentedRunnable, error) {
	ports := map[string]int{"http": 9090}

	f := e2e.NewFutureInstrumentedRunnable(env, name, ports, "http")
	if err := os.MkdirAll(f.HostDir(), 0750); err != nil {
		return nil, errors.Wrap(err, "create prometheus dir")
	}

	config := fmt.Sprintf(`
global:
  external_labels:
    prometheus: %v
scrape_configs:
- job_name: 'myself'
  # Quick scrapes for test purposes.
  scrape_interval: 1s
  scrape_timeout: 1s
  static_configs:
  - targets: [%s]
  relabel_configs:
  - source_labels: ['__address__']
    regex: '^.+:80$'
    action: drop
`, name, f.NetworkEndpoint("http"))

	if err := ioutil.WriteFile(filepath.Join(f.HostDir(), "prometheus.yml"), []byte(config), 0600); err != nil {
		return nil, errors.Wrap(err, "creating prom config failed")
	}

	args := e2e.BuildArgs(map[string]string{
		"--config.file":                     filepath.Join(f.LocalDir(), "prometheus.yml"),
		"--storage.tsdb.path":               f.LocalDir(),
		"--storage.tsdb.max-block-duration": "2h",
		"--log.level":                       "info",
		"--web.listen-address":              fmt.Sprintf(":%d", ports["http"]),
	})

	return f.Init(e2e.StartOptions{
		Image:     "quay.io/prometheus/prometheus:v2.27.0",
		Command:   e2e.NewCommandWithoutEntrypoint("prometheus", args...),
		Readiness: e2e.NewHTTPReadinessProbe("http", "/-/ready", 200, 200),
		User:      strconv.Itoa(os.Getuid()),
	}), nil
}

func TestDockerEnvLifecycle(t *testing.T) {
	e, err := e2e.NewDockerEnvironment(e2e.WithEnvironmentName("e2e_lifecycle"))
	testutil.Ok(t, err)

	var closed bool
	t.Cleanup(func() {
		if !closed {
			e.Close()
		}
	})

	p1, err := newPrometheus(e, "prometheus-1")
	testutil.Ok(t, err)
	p2, err := newPrometheus(e, "prometheus-2")
	testutil.Ok(t, err)

	testutil.Ok(t, e2e.StartAndWaitReady(p1, p2))
	testutil.Ok(t, p1.WaitReady())
	testutil.Ok(t, p1.WaitReady())

	//testutil.NotOk(t, p1.Start()) // Starting ok, should fail.

	o, err := p1.Metrics()
	testutil.Ok(t, err)
	fmt.Println(o)
	testutil.Ok(t, p1.WaitSumMetrics(e2e.Greater(50), "prometheus_tsdb_head_samples_appended_total"))
}
