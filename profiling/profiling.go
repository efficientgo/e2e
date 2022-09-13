// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

package e2eprofiling

import (
	"fmt"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/efficientgo/core/errors"
	"github.com/efficientgo/e2e"
	e2edb "github.com/efficientgo/e2e/db"
	"github.com/efficientgo/e2e/db/promconfig/discovery/config"
	e2einteractive "github.com/efficientgo/e2e/interactive"
	e2emonitoring "github.com/efficientgo/e2e/monitoring"
	"github.com/efficientgo/e2e/monitoring/promconfig/discovery/config"
	"github.com/efficientgo/e2e/monitoring/promconfig/discovery/targetgroup"
	"github.com/efficientgo/e2e/profiling/parcaconfig"
	"github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	"gopkg.in/yaml.v2"
)

var metaKey = struct{}{}

type Parca struct {
	e2e.Runnable
	e2emonitoring.Instrumented

	configHeader string
}

func NewParca(env e2e.Environment, name string, image string, flagOverride map[string]string) *Parca {
	if image == "" {
		image = "ghcr.io/parca-dev/parca:main-4e20a666"
	}

	f := env.Runnable(name).WithPorts(map[string]int{"http": 7070}).Future()

	args := map[string]string{
		"--config-path": filepath.Join(f.InternalDir(), "data", "parca.yml"),
	}
	if flagOverride != nil {
		args = e2e.MergeFlagsWithoutRemovingEmpty(args, flagOverride)
	}

	config := `
object_storage:
  bucket:
    type: "FILESYSTEM"
    config:
      directory: "./data"
scrape_configs:
`
	if err := os.WriteFile(filepath.Join(f.Dir(), "data", "parca.yml"), []byte(config), 0600); err != nil {
		return &Parca{Runnable: e2e.NewErrorer(name, errors.Wrap(err, "create prometheus config failed"))}
	}

	p := e2emonitoring.AsInstrumented(f.Init(e2e.StartOptions{
		Image:     image,
		Command:   e2e.NewCommand("/parca", e2e.BuildArgs(args)...),
		User:      strconv.Itoa(os.Getuid()),
		Readiness: e2e.NewTCPReadinessProbe("http"),
	}), "http")

	return &Parca{
		configHeader: config,
		Runnable:     p,
		Instrumented: p,
	}
}

// SetScrapeConfigs updates Parca with new configuration  marsh
func (p *Parca) SetScrapeConfigs(scrapeJobs []parcaconfig.ScrapeConfig) error {
	c := p.configHeader

	b, err := yaml.Marshal(struct {
		ScrapeConfigs []parcaconfig.ScrapeConfig `yaml:"scrape_configs,omitempty"`
	}{ScrapeConfigs: scrapeJobs})
	if err != nil {
		return err
	}

	config := fmt.Sprintf("%v\n%v", c, b)
	if err := os.WriteFile(filepath.Join(p.Dir(), "data", "parca.yml"), []byte(config), 0600); err != nil {
		return errors.Wrap(err, "creating parca config failed")
	}

	if p.IsRunning() {
		// Reload configuration.
		return p.Exec(e2e.NewCommand("kill", "-SIGHUP", "1"))
	}
	return nil
}

type Service struct {
	p Parca
}

type listener struct {
	p Parca

	localAddr      string
	scrapeInterval time.Duration
}

func (l *listener) updateConfig(started map[string]instrumented) error {
	//  - job_name: "labeler"
	//    scrape_interval: "15s"
	//    static_configs:
	//  - targets: [ '` + labeler.InternalEndpoint("http") + `' ]
	//    profiling_config:
	//    pprof_config:
	//    fgprof:
	//    enabled: true
	//    path: /debug/fgprof/profile
	//    delta: true
	var scfgs []parcaconfig.ScrapeConfig
	add := func(name string, instr instrumented) {
		scfg := parcaconfig.ScrapeConfig{
			JobName:                name,
			ServiceDiscoveryConfig: sdconfig.sdconfig{},
			HTTPClientConfig: config.HTTPClientConfig{
				TLSConfig: config.TLSConfig{
					// TODO(bwplotka): Allow providing certs?
					// Allow insecure TLS. We are in benchmark/test that is focused on gathering data on all cost.
					InsecureSkipVerify: true,
				},
			},
		}

		for _, t := range instr.ProfilingTargets() {
			g := &targetgroup.Group{
				Targets: []model.LabelSet{map[model.LabelName]model.LabelValue{
					model.AddressLabel: model.LabelValue(t.InternalEndpoint),
				}},
				Labels: map[model.LabelName]model.LabelValue{
					model.SchemeLabel: model.LabelValue(strings.ToLower(t.Scheme)),
				},
			}
			scfg.ProfilingConfig = t.Config
			scfg.ServiceDiscoveryConfig.StaticConfigs = append(scfg.ServiceDiscoveryConfig.StaticConfigs, g)
		}
		scfgs = append(scfgs, scfg)
	}

	// Register local address.
	scfg := parcaconfig.ScrapeConfig{
		JobName: "local",
		ServiceDiscoveryConfig: sdconfig.ServiceDiscoveryConfig{StaticConfigs: []*targetgroup.Group{{
			Targets: []model.LabelSet{
				map[model.LabelName]model.LabelValue{
					model.AddressLabel: model.LabelValue(l.localAddr),
				},
			},
		}}},
	}
	scfgs = append(scfgs, scfg)

	for name, s := range started {
		add(name, s)
	}

	return l.p.SetScrapeConfigs(scfgs)
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
	scrapeInterval   time.Duration
	customParcaImage string
}

// WithScrapeInterval changes how often metrics are scrape by Prometheus. 5s by default.
func WithScrapeInterval(interval time.Duration) func(*opt) {
	return func(o *opt) {
		o.scrapeInterval = interval
	}
}

// WithParcaImage allows injecting custom Parca docker image to use as scraper and queryable.
func WithParcaImage(image string) func(*opt) {
	return func(o *opt) {
		o.customParcaImage = image
	}
}

type Option func(*opt)

// Start deploys monitoring service which deploys Parca that monitors all registered
// InstrumentedServices in environment.
func Start(env e2e.Environment, opts ...Option) (_ *Service, err error) {
	opt := opt{scrapeInterval: 5 * time.Second}
	for _, o := range opts {
		o(&opt)
	}

	m := http.NewServeMux()
	m.HandleFunc("/debug/pprof/", pprof.Index)

	o := sync.Once{}
	scraped := make(chan struct{})
	m.Handle("/debug/pprof/profile", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		o.Do(func() { close(scraped) })
		pprof.Profile(w, req)
	}))

	// Listen on all addresses, since we need to connect to it from docker container.
	list, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		return nil, err
	}
	s := http.Server{Handler: m}

	go func() { _ = s.Serve(list) }()
	env.AddCloser(func() { _ = s.Close() })

	var dbOpts []e2edb.Option
	if opt.customParcaImage != "" {
		dbOpts = append(dbOpts, e2edb.WithImage(opt.customParcaImage))
	}
	p := e2edb.NewParca(env, "monitoring", dbOpts...)

	_, port, err := net.SplitHostPort(list.Addr().String())
	if err != nil {
		return nil, err
	}
	l := &listener{p: p, localAddr: net.JoinHostPort(env.HostAddr(), port), scrapeInterval: opt.scrapeInterval}
	if err := l.updateConfig(map[string]instrumented{}); err != nil {
		return nil, err
	}
	env.AddListener(l)

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

func (s *Service) OpenUserInterfaceInBrowser(paths ...string) error {
	return e2einteractive.OpenInBrowser("http://" + s.p.Endpoint(e2edb.AccessPortName) + strings.Join(paths, "/"))
}
