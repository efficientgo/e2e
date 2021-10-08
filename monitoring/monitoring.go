// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

package e2emonitoring

import (
	"net"
	"net/http"
	"os"
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
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/model"
	"github.com/prometheus/common/version"
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

func (l *listener) updateConfig(started map[string]e2e.Instrumented) error {
	// TODO(bwplotka): Scrape our process metrics too?

	cfg := promconfig.Config{
		GlobalConfig: promconfig.GlobalConfig{
			ExternalLabels: map[model.LabelName]model.LabelValue{"prometheus": model.LabelValue(l.p.Name())},
			ScrapeInterval: model.Duration(l.scrapeInterval),
		},
	}

	add := func(name string, instr e2e.Instrumented) {
		scfg := &promconfig.ScrapeConfig{
			JobName:                name,
			ServiceDiscoveryConfig: sdconfig.ServiceDiscoveryConfig{StaticConfigs: []*targetgroup.Group{{}}},
		}
		for _, t := range instr.MetricTargets() {
			scfg.ServiceDiscoveryConfig.StaticConfigs[0].Targets = append(scfg.ServiceDiscoveryConfig.StaticConfigs[0].Targets, map[model.LabelName]model.LabelValue{
				model.AddressLabel: model.LabelValue(t.InternalEndpoint),
			})

			if t.MetricPath != "/metrics" {
				// TODO(bwplotka) Add relabelling rule to change `__path__`.
				panic("Different metrics endpoints are not implemented yet")
			}
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

func (l *listener) OnRunnableChange(started []e2e.Runnable) error {
	s := map[string]e2e.Instrumented{}
	for _, r := range started {
		instr, ok := r.(e2e.Instrumented)
		if !ok {
			continue
		}
		s[r.Name()] = instr
	}

	return l.updateConfig(s)
}

type opt struct {
	currentProcessAsContainer bool
	scrapeInterval            time.Duration
}

// WithCurrentProcessAsContainer makes Start put current process PID into cgroups and organize
// them in a way that makes cadvisor to watch those as it would be any other container.
// NOTE: This option requires a manual on-off per machine/restart setup that will be printed on first start (permissions).
func WithCurrentProcessAsContainer() func(*opt) {
	return func(o *opt) {
		o.currentProcessAsContainer = true
	}
}

// WithScrapeInterval changes how often metrics are scrape by Prometheus. 5s by default.
func WithScrapeInterval(interval time.Duration) func(*opt) {
	return func(o *opt) {
		o.scrapeInterval = interval
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
	metrics := prometheus.NewRegistry()
	metrics.MustRegister(
		version.NewCollector("thanos"),
		prometheus.NewGoCollector(),
		prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}),
	)

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
	if err := l.updateConfig(map[string]e2e.Instrumented{}); err != nil {
		return nil, err
	}
	env.AddListener(l)

	var path []string
	if opt.currentProcessAsContainer {
		// Do cgroup magic allowing us to monitor current PID as container.
		path, err = setupPIDAsContainer(env, os.Getpid())
		if err != nil {
			return nil, err
		}
	}

	if err := newCadvisor(env, "cadvisor", path...).Start(); err != nil {
		return nil, err
	}

	if err := e2e.StartAndWaitReady(p); err != nil {
		return nil, err
	}

	select {
	case <-time.After(2 * time.Minute):
		return nil, errors.New("Prometheus failed to scrape local endpoint after 2 minutes, check monitoring Prometheus logs")
	case <-scraped:
	}

	return &Service{p: p}, nil
}

func (s *Service) OpenUserInterfaceInBrowser() error {
	return e2einteractive.OpenInBrowser("http://" + s.p.Endpoint(e2edb.AccessPortName))
}

func newCadvisor(env e2e.Environment, name string, cgroupPrefixes ...string) *e2e.InstrumentedRunnable {
	return e2e.NewInstrumentedRunnable(env, name, map[string]int{"http": 8080}, "http").Init(e2e.StartOptions{
		// See https://github.com/google/cadvisor/blob/master/docs/runtime_options.md.
		Command: e2e.NewCommand(
			// TODO(bwplotka): Add option to scope to dockers only from this network.
			"--docker_only=true",
			"--raw_cgroup_prefix_whitelist="+strings.Join(cgroupPrefixes, ","),
		),
		Image: "gcr.io/cadvisor/cadvisor:v0.37.5",
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
