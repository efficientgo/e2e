// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

package e2eprof

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
	e2einteractive "github.com/efficientgo/e2e/interactive"
	e2emon "github.com/efficientgo/e2e/monitoring"
	sdconfig "github.com/efficientgo/e2e/monitoring/promconfig/discovery/config"
	"github.com/efficientgo/e2e/monitoring/promconfig/discovery/targetgroup"
	"github.com/efficientgo/e2e/profiling/parcaconfig"
	"github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	"gopkg.in/yaml.v2"
)

type metaKeyType struct{}

var metaKey = metaKeyType{}

type Parca struct {
	e2e.Runnable
	e2emon.Instrumented

	configHeader string
}

func NewParca(env e2e.Environment, name string, image string, flagOverride map[string]string) *Parca {
	if image == "" {
		image = "ghcr.io/parca-dev/parca:main-4e20a666"
	}

	f := env.Runnable(name).WithPorts(map[string]int{"http": 7070}).Future()

	args := map[string]string{
		"--config-path": filepath.Join(f.InternalDir(), "parca.yml"),
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
`
	if err := os.WriteFile(filepath.Join(f.Dir(), "parca.yml"), []byte(config), 0600); err != nil {
		return &Parca{Runnable: e2e.NewFailedRunnable(name, errors.Wrap(err, "create Parca config failed"))}
	}

	p := e2emon.AsInstrumented(f.Init(e2e.StartOptions{
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

// SetScrapeConfigs updates Parca with new configuration.
func (p *Parca) SetScrapeConfigs(scrapeJobs []parcaconfig.ScrapeConfig) error {
	if p.BuildErr() != nil {
		return p.BuildErr()
	}

	c := p.configHeader

	b, err := yaml.Marshal(struct {
		ScrapeConfigs []parcaconfig.ScrapeConfig `yaml:"scrape_configs,omitempty"`
	}{ScrapeConfigs: scrapeJobs})
	if err != nil {
		return err
	}

	config := fmt.Sprintf("%v\n%s", c, b)
	if err := os.WriteFile(filepath.Join(p.Dir(), "parca.yml"), []byte(config), 0600); err != nil {
		return errors.Wrap(err, "creating Parca config failed")
	}
	return nil
}

type Service struct {
	p *Parca
}

type listener struct {
	p *Parca

	localAddr      string
	scrapeInterval time.Duration
}

func (l *listener) updateConfig(started map[string]Profiled) error {
	var scfgs []parcaconfig.ScrapeConfig

	// Register local address.
	scfg := parcaconfig.ScrapeConfig{
		JobName:        "local",
		ScrapeInterval: model.Duration(l.scrapeInterval),
		ServiceDiscoveryConfig: sdconfig.ServiceDiscoveryConfig{StaticConfigs: []*targetgroup.Group{{
			Targets: []model.LabelSet{
				map[model.LabelName]model.LabelValue{
					model.AddressLabel: model.LabelValue(l.localAddr),
				},
			},
		}}},
	}
	scfgs = append(scfgs, scfg)

	for _, s := range started {
		for _, t := range s.ProfileTargets() {
			scfg := parcaconfig.ScrapeConfig{
				JobName:        t.Name,
				ScrapeInterval: model.Duration(l.scrapeInterval),
				ServiceDiscoveryConfig: sdconfig.ServiceDiscoveryConfig{
					StaticConfigs: []*targetgroup.Group{{
						Targets: []model.LabelSet{map[model.LabelName]model.LabelValue{
							model.AddressLabel: model.LabelValue(t.InternalEndpoint),
						}},
						Labels: map[model.LabelName]model.LabelValue{
							model.SchemeLabel: model.LabelValue(strings.ToLower(t.Scheme)),
						},
					}},
				},
				HTTPClientConfig: config.HTTPClientConfig{
					TLSConfig: config.TLSConfig{
						// TODO(bwplotka): Allow providing certs?
						// Allow insecure TLS. We are in benchmark/test that is focused on gathering data on all cost.
						InsecureSkipVerify: true,
					},
				},
			}
			scfg.ProfilingConfig = t.Config
			scfgs = append(scfgs, scfg)
		}
	}
	return l.p.SetScrapeConfigs(scfgs)
}

func (l *listener) OnRunnableChange(started []e2e.Runnable) error {
	s := map[string]Profiled{}
	for _, r := range started {
		instr, ok := r.GetMetadata(metaKey)
		if !ok {
			continue
		}
		s[r.Name()] = instr.(Profiled)
	}

	return l.updateConfig(s)
}

type opt struct {
	scrapeInterval   time.Duration
	customParcaImage string
}

// WithScrapeInterval changes how often profiles are collected by Parca. 5s by default.
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

// Start deploys monitoring service which deploys Parca that collects profiles from all
// ProfiledRunnable instances in environment created with AsProfiled.
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

	p := NewParca(env, "profiling", opt.customParcaImage, nil)

	_, port, err := net.SplitHostPort(list.Addr().String())
	if err != nil {
		return nil, err
	}
	l := &listener{p: p, localAddr: net.JoinHostPort(env.HostAddr(), port), scrapeInterval: opt.scrapeInterval}
	if err := l.updateConfig(map[string]Profiled{}); err != nil {
		return nil, err
	}
	env.AddListener(l)

	if err := e2e.StartAndWaitReady(p); err != nil {
		return nil, errors.Wrap(err, "starting profiling and waiting until ready")
	}

	select {
	case <-time.After(2 * time.Minute):
		return nil, errors.New("Parca failed to collect profiles from local process after 2 minutes, check profiling Parca logs")
	case <-scraped:
	}

	return &Service{p: p}, nil
}

func (s *Service) OpenUserInterfaceInBrowser(paths ...string) error {
	return e2einteractive.OpenInBrowser("http://" + s.p.Endpoint("http") + strings.Join(paths, "/"))
}
