// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

package e2emon

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/efficientgo/e2e/host"

	"github.com/efficientgo/core/errcapture"
	"github.com/efficientgo/core/errors"
	"github.com/efficientgo/e2e"
	e2einteractive "github.com/efficientgo/e2e/interactive"
	"github.com/efficientgo/e2e/monitoring/promconfig"
	sdconfig "github.com/efficientgo/e2e/monitoring/promconfig/discovery/config"
	"github.com/efficientgo/e2e/monitoring/promconfig/discovery/targetgroup"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	"gopkg.in/yaml.v2"
)

type metaKeyType struct{}

var metaKey = metaKeyType{}

type Prometheus struct {
	e2e.Runnable
	Instrumented
}

func NewPrometheus(env e2e.Environment, name string, image string, flagOverride map[string]string) *Prometheus {
	if image == "" {
		image = "quay.io/prometheus/prometheus:v2.37.0"
	}
	ports := map[string]int{"http": 9090}

	f := env.Runnable(name).WithPorts(ports).Future()
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
	if err := os.WriteFile(filepath.Join(f.Dir(), "prometheus.yml"), []byte(config), 0600); err != nil {
		return &Prometheus{Runnable: e2e.NewFailedRunnable(name, errors.Wrap(err, "create prometheus config failed"))}
	}

	args := map[string]string{
		"--config.file":                     filepath.Join(f.Dir(), "prometheus.yml"),
		"--storage.tsdb.path":               f.Dir(),
		"--storage.tsdb.max-block-duration": "2h", // No compaction - mostly not needed for quick test.
		"--log.level":                       "info",
		"--web.listen-address":              fmt.Sprintf(":%d", ports["http"]),
	}
	if flagOverride != nil {
		args = e2e.MergeFlagsWithoutRemovingEmpty(args, flagOverride)
	}

	p := AsInstrumented(f.Init(e2e.StartOptions{
		Image:     image,
		Command:   e2e.NewCommandWithoutEntrypoint("prometheus", e2e.BuildArgs(args)...),
		Readiness: e2e.NewHTTPReadinessProbe("http", "/-/ready", 200, 200),
		User:      strconv.Itoa(os.Getuid()),
	}), "http")

	return &Prometheus{
		Runnable:     p,
		Instrumented: p,
	}
}

func (p *Prometheus) SetConfigEncoded(config []byte) error {
	if p.BuildErr() != nil {
		return p.BuildErr()
	}

	if err := os.WriteFile(filepath.Join(p.Dir(), "prometheus.yml"), config, 0600); err != nil {
		return errors.Wrap(err, "creating prom config failed")
	}

	if p.IsRunning() {
		// Reload configuration.
		return p.Exec(e2e.NewCommand("kill", "-SIGHUP", "1"))
	}
	return nil
}

func (p *Prometheus) SetConfig(config promconfig.Config) error {
	b, err := yaml.Marshal(config)
	if err != nil {
		return err
	}

	return p.SetConfigEncoded(b)
}

type Service struct {
	p *Prometheus
}

type listener struct {
	p *Prometheus

	localAddr      string
	scrapeInterval time.Duration
}

func (l *listener) updateConfig(started map[string]Instrumented) error {
	cfg := promconfig.Config{
		GlobalConfig: promconfig.GlobalConfig{
			ExternalLabels: map[model.LabelName]model.LabelValue{"prometheus": model.LabelValue(l.p.Name())},
			ScrapeInterval: model.Duration(l.scrapeInterval),
		},
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

	for name, s := range started {
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

		for _, t := range s.MetricTargets() {
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

	return l.p.SetConfig(cfg)
}

func (l *listener) OnRunnableChange(started []e2e.Runnable) error {
	s := map[string]Instrumented{}
	for _, r := range started {
		instr, ok := r.GetMetadata(metaKey)
		if !ok {
			continue
		}
		s[r.Name()] = instr.(Instrumented)
	}

	return l.updateConfig(s)
}

type opt struct {
	scrapeInterval  time.Duration
	customRegistry  *prometheus.Registry
	customPromImage string
	useCadvisor     bool
}

// WithScrapeInterval changes how often metrics are scrape by Prometheus. 5s by default.
func WithScrapeInterval(interval time.Duration) Option {
	return func(o *opt) {
		o.scrapeInterval = interval
	}
}

// WithCustomRegistry allows injecting a custom registry to use for this process metrics.
// NOTE(bwplotka): Injected registry will be used as is, while the default registry
// will have prometheus.NewGoCollector() and prometheus.NewProcessCollector(..) registered.
func WithCustomRegistry(reg *prometheus.Registry) Option {
	return func(o *opt) {
		o.customRegistry = reg
	}
}

// WithPrometheusImage allows injecting custom Prometheus docker image to use as scraper and queryable.
func WithPrometheusImage(image string) Option {
	return func(o *opt) {
		o.customPromImage = image
	}
}

func WithCadvisorDisabled() Option {
	return func(o *opt) {
		o.useCadvisor = false
	}
}

type Option func(*opt)

// Start deploys monitoring service which deploys Prometheus that monitors all
// InstrumentedRunnable instances in an environment created with AsInstrumented.
func Start(env e2e.Environment, opts ...Option) (_ *Service, err error) {
	opt := opt{
		scrapeInterval: 5 * time.Second,
		useCadvisor:    true,
	}
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

	// Listen on all tcp4 addresses, since we need to connect to it from Docker container.
	// For unknown reasons, when using WSL 2, if the network type is "tcp" it will
	// end up only binding to the IPv6 in the WSL host, which later cannot be acessed
	// via IPv4 to confirm Prometheus can scrape the local endpoint.
	// Explicitly asking for an IPv4 listener works.
	networkType := "tcp"
	if host.OSPlatform() == "WSL2" {
		networkType = "tcp4"
	}
	list, err := net.Listen(networkType, "0.0.0.0:0")
	if err != nil {
		return nil, err
	}
	s := http.Server{Handler: m}

	go func() { _ = s.Serve(list) }()
	env.AddCloser(func() { _ = s.Close() })

	p := NewPrometheus(env, "monitoring", opt.customPromImage, nil)

	_, port, err := net.SplitHostPort(list.Addr().String())
	if err != nil {
		return nil, err
	}
	l := &listener{p: p, localAddr: net.JoinHostPort(env.HostAddr(), port), scrapeInterval: opt.scrapeInterval}
	if err := l.updateConfig(map[string]Instrumented{}); err != nil {
		return nil, err
	}
	env.AddListener(l)

	if opt.useCadvisor {
		if host.OSPlatform() == "WSL2" {
			return nil, errors.New("cadvisor is not supported in WSL 2 environments")
		}
		c := newCadvisor(env, "cadvisor")
		if err := e2e.StartAndWaitReady(c); err != nil {
			return nil, errors.Wrap(err, "starting cadvisor and waiting until ready")
		}
	}
	if err := e2e.StartAndWaitReady(p); err != nil {
		return nil, errors.Wrap(err, "starting monitoring and waiting until ready")
	}

	select {
	case <-time.After(2 * time.Minute):
		return nil, errors.New("Prometheus failed to scrape local endpoint after 2 minutes, check monitoring Prometheus logs")
	case <-scraped:
	}

	return &Service{p: p}, nil
}

func (s *Service) OpenUserInterfaceInBrowser(paths ...string) error {
	return e2einteractive.OpenInBrowser("http://" + s.p.Endpoint("http") + strings.Join(paths, "/"))
}

// InstantQuery evaluates instant PromQL queries against monitoring service.
func (s *Service) InstantQuery(query string) (string, error) {
	if !s.p.IsRunning() {
		return "", errors.Newf("%s is not running", s.p.Name())
	}

	res, err := (&http.Client{}).Get("http://" + s.p.Endpoint("http") + "/api/v1/query?query=" + query)
	if err != nil {
		return "", err
	}

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", errors.Newf("unexpected status code %d while fetching metrics", res.StatusCode)
	}
	defer errcapture.ExhaustClose(&err, res.Body, "metrics response")

	body, err := io.ReadAll(res.Body)
	return string(body), err
}

// GetMonitoringRunnable returns a Prometheus monitoring runnable.
func (s *Service) GetMonitoringRunnable() e2e.Runnable {
	return s.p
}

func newCadvisor(env e2e.Environment, name string, cgroupPrefixes ...string) *InstrumentedRunnable {
	return AsInstrumented(env.Runnable(name).WithPorts(map[string]int{"http": 8080}).Init(e2e.StartOptions{
		// See https://github.com/google/cadvisor/blob/master/docs/runtime_options.md.
		Command: e2e.NewCommand(
			// TODO(bwplotka): Add option to scope to dockers only from this network.
			"--docker_only=true",
			"--raw_cgroup_prefix_whitelist="+strings.Join(cgroupPrefixes, ","),
		),
		Image: "gcr.io/cadvisor/cadvisor:v0.45.0",
		// See https://github.com/google/cadvisor/blob/master/docs/running.md.
		Volumes: []string{
			"/:/rootfs:ro",
			"/var/run:/var/run:rw",
			"/sys:/sys:ro",
			"/var/lib/docker/:/var/lib/docker:ro",
		},
		UserNs:     "host",
		Privileged: true,
	}), "http")
}

const nginxImage = "docker.io/nginx:1.21.1-alpine"

// NewStaticMetricsServer creates a new nginx server that serves the content of metrics as /metrics endpoint.
// This is useful for testing different metrics scrapers.
func NewStaticMetricsServer(e e2e.Environment, name string, metrics []byte) *InstrumentedRunnable {
	f := e.Runnable(name).WithPorts(map[string]int{"http": 80}).Future()
	if err := os.MkdirAll(f.Dir(), 0750); err != nil {
		return &InstrumentedRunnable{Runnable: e2e.NewFailedRunnable(name, errors.Wrap(err, "create static metrics dir"))}
	}
	metricsFilePath := filepath.Join(f.Dir(), "metrics.txt")
	if err := os.WriteFile(metricsFilePath, metrics, 0644); err != nil {
		return &InstrumentedRunnable{Runnable: e2e.NewFailedRunnable(name, errors.Wrap(err, "creating static metrics file"))}
	}
	probe := e2e.NewHTTPReadinessProbe("http", "/metrics", 200, 200)
	return AsInstrumented(
		f.Init(e2e.StartOptions{
			Image:     nginxImage,
			Volumes:   []string{metricsFilePath + ":/usr/share/nginx/html/metrics:ro"},
			Readiness: probe,
		}),
		"http",
	)
}
