// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

package e2edb

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"

	"github.com/efficientgo/e2e"
	"github.com/pkg/errors"
)

type Prometheus struct {
	*e2e.InstrumentedRunnable
}

func NewPrometheus(env e2e.Environment, name string, opts ...Option) (*Prometheus, error) {
	o := options{image: "quay.io/prometheus/prometheus:v2.27.0"}
	for _, opt := range opts {
		opt(&o)
	}

	ports := map[string]int{"http": 9090}

	f := e2e.NewFutureInstrumentedRunnable(env, name, ports, "http")
	if err := os.MkdirAll(f.Dir(), 0750); err != nil {
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
`, name, f.InternalEndpoint("http"))
	if err := ioutil.WriteFile(filepath.Join(f.Dir(), "prometheus.yml"), []byte(config), 0600); err != nil {
		return nil, errors.Wrap(err, "creating prom config failed")
	}

	args := e2e.BuildArgs(map[string]string{
		"--config.file":                     filepath.Join(f.InternalDir(), "prometheus.yml"),
		"--storage.tsdb.path":               f.InternalDir(),
		"--storage.tsdb.max-block-duration": "2h",
		"--log.level":                       "info",
		"--web.listen-address":              fmt.Sprintf(":%d", ports["http"]),
	})

	return &Prometheus{InstrumentedRunnable: f.Init(e2e.StartOptions{
		Image:     o.image,
		Command:   e2e.NewCommandWithoutEntrypoint("prometheus", args...),
		Readiness: e2e.NewHTTPReadinessProbe("http", "/-/ready", 200, 200),
		User:      strconv.Itoa(os.Getuid()),
	})}, nil
}

func (p *Prometheus) SetConfig(config string) error {
	if err := ioutil.WriteFile(filepath.Join(p.Dir(), "prometheus.yml"), []byte(config), 0600); err != nil {
		return errors.Wrap(err, "creating prom config failed")
	}

	if p.IsRunning() {
		_, _, err := p.Exec(e2e.NewCommand("kill", "-SIGHUP", "1"))
		return err
	}
	return nil
}
