// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

package e2emonitoring

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/efficientgo/e2e"
	e2edb "github.com/efficientgo/e2e/db"
	e2einteractive "github.com/efficientgo/e2e/interactive"
	"github.com/efficientgo/e2e/monitoring/promconfig"
	sdconfig "github.com/efficientgo/e2e/monitoring/promconfig/discovery/config"
	"github.com/efficientgo/e2e/monitoring/promconfig/discovery/targetgroup"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	"gopkg.in/yaml.v2"
)

type Service struct {
	p *e2edb.Prometheus
}

type listener struct {
	p *e2edb.Prometheus

	localAddr      string
	scrapeInterval time.Duration
}

func (l *listener) updateConfig(started map[string]instrumented) error {
	// TODO(bwplotka): Scrape our process metrics too?

	cfg := promconfig.Config{
		GlobalConfig: promconfig.GlobalConfig{
			ExternalLabels: map[model.LabelName]model.LabelValue{"prometheus": model.LabelValue(l.p.Name())},
			ScrapeInterval: model.Duration(l.scrapeInterval),
		},
	}
	add := func(name string, instr instrumented) {
		scfg := &promconfig.ScrapeConfig{
			JobName:                name,
			ServiceDiscoveryConfig: sdconfig.ServiceDiscoveryConfig{},
			HTTPClientConfig: config.HTTPClientConfig{
				TLSConfig: config.TLSConfig{
					// TODO(bwplotka): Allow providing certs?
					// Allow insecure TLS. We are in benchmark/test that is focused on gathering data on all cost.
					InsecureSkipVerify: true,
				},
			},
		}

		for _, t := range instr.MetricTargets() {
			g := &targetgroup.Group{
				Targets: []model.LabelSet{map[model.LabelName]model.LabelValue{
					model.AddressLabel: model.LabelValue(t.InternalEndpoint),
				}},
				Labels: map[model.LabelName]model.LabelValue{
					model.SchemeLabel:      model.LabelValue(strings.ToLower(t.Scheme)),
					model.MetricsPathLabel: model.LabelValue(t.MetricPath),
				},
			}
			scfg.ServiceDiscoveryConfig.StaticConfigs = append(scfg.ServiceDiscoveryConfig.StaticConfigs, g)
		}
		cfg.ScrapeConfigs = append(cfg.ScrapeConfigs, scfg)
	}

	// Register local address.
	scfg := &promconfig.ScrapeConfig{
		JobName: "local",
		ServiceDiscoveryConfig: sdconfig.ServiceDiscoveryConfig{StaticConfigs: []*targetgroup.Group{{
			Targets: []model.LabelSet{
				map[model.LabelName]model.LabelValue{
					model.AddressLabel: model.LabelValue(l.localAddr),
				},
			},
		}}},
	}
	cfg.ScrapeConfigs = append(cfg.ScrapeConfigs, scfg)

	add("e2emonitoring-prometheus", l.p)
	for name, s := range started {
		add(name, s)
	}

	o, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return l.p.SetConfig(string(o))
}

type instrumented interface {
	MetricTargets() []e2e.MetricTarget
}

func (l *listener) OnRunnableChange(started []e2e.Runnable) error {
	s := map[string]instrumented{}
	for _, r := range started {
		instr, ok := r.(instrumented)
		if !ok {
			continue
		}
		s[r.Name()] = instr
	}

	return l.updateConfig(s)
}

type opt struct {
	scrapeInterval time.Duration
	customRegistry *prometheus.Registry
}

// WithScrapeInterval changes how often metrics are scrape by Prometheus. 5s by default.
func WithScrapeInterval(interval time.Duration) func(*opt) {
	return func(o *opt) {
		o.scrapeInterval = interval
	}
}

// WithCustomRegistry allows injecting a custom registry to use for this process metrics.
// NOTE(bwplotka): Injected registry will be used as is, while the default registry
// will have prometheus.NewGoCollector() and prometheus.NewProcessCollector(..) registered.
func WithCustomRegistry(reg *prometheus.Registry) func(*opt) {
	return func(o *opt) {
		o.customRegistry = reg
	}
}

type Option func(*opt)

// Start deploys monitoring service which deploys Prometheus that monitors all registered InstrumentedServices
// in environment.
func Start(env e2e.Environment, opts ...Option) (_ *Service, err error) {
	opt := opt{scrapeInterval: 5 * time.Second}
	for _, o := range opts {
		o(&opt)
	}

	// Expose metrics from the current process.
	metrics := opt.customRegistry
	if metrics == nil {
		metrics = prometheus.NewRegistry()
		metrics.MustRegister(
			collectors.NewGoCollector(),
			collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		)
	}

	m := http.NewServeMux()
	h := promhttp.HandlerFor(metrics, promhttp.HandlerOpts{})
	o := sync.Once{}
	scraped := make(chan struct{})
	m.Handle("/metrics", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		o.Do(func() { close(scraped) })
		h.ServeHTTP(w, req)
	}))

	// Listen on all addresses, since we need to connect to it from docker container.
	list, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		return nil, err
	}
	s := http.Server{Handler: m}

	go func() { _ = s.Serve(list) }()
	env.AddCloser(func() { _ = s.Close() })

	p := e2edb.NewPrometheus(env, "monitoring")

	_, port, err := net.SplitHostPort(list.Addr().String())
	if err != nil {
		return nil, err
	}
	l := &listener{p: p, localAddr: net.JoinHostPort(env.HostAddr(), port), scrapeInterval: opt.scrapeInterval}
	if err := l.updateConfig(map[string]instrumented{}); err != nil {
		return nil, err
	}
	env.AddListener(l)

	c := newCadvisor(env, "cadvisor")
	if err := e2e.StartAndWaitReady(c, p); err != nil {
		return nil, err
	}

	select {
	case <-time.After(2 * time.Minute):
		return nil, errors.New("Prometheus failed to scrape local endpoint after 2 minutes, check monitoring Prometheus logs")
	case <-scraped:
	}

	return &Service{p: p}, nil
}

func (s *Service) OpenUserInterfaceInBrowser(paths ...string) error {
	return e2einteractive.OpenInBrowser("http://" + s.p.Endpoint(e2edb.AccessPortName) + strings.Join(paths, "/"))
}

func newCadvisor(env e2e.Environment, name string, cgroupPrefixes ...string) e2e.InstrumentedRunnable {
	return e2e.NewInstrumentedRunnable(env, name).WithPorts(map[string]int{"http": 8080}, "http").Init(e2e.StartOptions{
		// See https://github.com/google/cadvisor/blob/master/docs/runtime_options.md.
		Command: e2e.NewCommand(
			// TODO(bwplotka): Add option to scope to dockers only from this network.
			"--docker_only=true",
			"--raw_cgroup_prefix_whitelist="+strings.Join(cgroupPrefixes, ","),
		),
		Image: "gcr.io/cadvisor/cadvisor:v0.39.3",
		// See https://github.com/google/cadvisor/blob/master/docs/running.md.
		Volumes: []string{
			"/:/rootfs:ro",
			"/var/run:/var/run:rw",
			"/sys:/sys:ro",
			"/var/lib/docker/:/var/lib/docker:ro",
		},
		UserNs:     "host",
		Privileged: true,
	})
}
