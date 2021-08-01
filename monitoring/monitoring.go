// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

package e2emonitoring

import (
	"fmt"
	"time"

	"github.com/efficientgo/e2e"
	e2edb "github.com/efficientgo/e2e/db"
	e2einteractive "github.com/efficientgo/e2e/interactive"
	"github.com/efficientgo/e2e/monitoring/promconfig"
	sdconfig "github.com/efficientgo/e2e/monitoring/promconfig/discovery/config"
	"github.com/efficientgo/e2e/monitoring/promconfig/discovery/targetgroup"
	"github.com/prometheus/common/model"
	"gopkg.in/yaml.v2"
)

type Service struct {
	p *e2edb.Prometheus
}

type listener struct {
	p *e2edb.Prometheus
}

func (l *listener) updateConfig(started map[string]e2e.Instrumented) error {
	cfg := promconfig.Config{
		GlobalConfig: promconfig.GlobalConfig{
			ExternalLabels: map[model.LabelName]model.LabelValue{"prometheus": model.LabelValue(l.p.Name())},
			ScrapeInterval: model.Duration(15 * time.Second),
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
			fmt.Printf("NOT INSTRUMENTABLE %s %T\n", r.Name(), r) // To fix.
			continue
		}
		s[r.Name()] = instr
	}

	return l.updateConfig(s)
}

// Start deploys monitoring service which deploys Prometheus that monitors all registered InstrumentedServices
// in environment.
func Start(env e2e.Environment) (*Service, error) {
	p := e2edb.NewPrometheus(env, "monitoring")
	l := &listener{p: p}
	if err := l.updateConfig(map[string]e2e.Instrumented{}); err != nil {
		return nil, err
	}
	env.AddListener(l)

	if err := newCadvisor(env, "cadvisor").Start(); err != nil {
		return nil, err
	}

	// TODO(bwplotka): Run cadvisor.
	return &Service{p: p}, e2e.StartAndWaitReady(p)
}

func (s *Service) OpenUserInterfaceInBrowser() error {
	return e2einteractive.OpenInBrowser("http://" + s.p.Endpoint(e2edb.AccessPortName))
}

func newCadvisor(env e2e.Environment, name string) *e2e.InstrumentedRunnable {
	return e2e.NewInstrumentedRunnable(env, name, map[string]int{"http": 8080}, "http").Init(e2e.StartOptions{
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
