// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

package parcaconfig

import (
	"net/url"

	"github.com/efficientgo/e2e/monitoring/promconfig"
	sdconfig "github.com/efficientgo/e2e/monitoring/promconfig/discovery/config"
	config_util "github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
)

// ScrapeConfig configures a scraping unit for conprof.
type ScrapeConfig struct {
	// Name of the section in the config
	JobName string `yaml:"job_name"`
	// A set of query parameters with which the target is scraped.
	Params url.Values `yaml:"params,omitempty"`
	// How frequently to scrape the targets of this scrape config.
	ScrapeInterval model.Duration `yaml:"scrape_interval,omitempty"`
	// The timeout for scraping targets of this config.
	ScrapeTimeout model.Duration `yaml:"scrape_timeout,omitempty"`
	// The URL scheme with which to fetch metrics from targets.
	Scheme string `yaml:"scheme,omitempty"`

	ServiceDiscoveryConfig sdconfig.ServiceDiscoveryConfig `yaml:",inline"`
	ProfilingConfig        *ProfilingConfig                `yaml:"profiling_config,omitempty"`
	HTTPClientConfig       config_util.HTTPClientConfig    `yaml:",inline"`
	RelabelConfigs         []*promconfig.RelabelConfig     `yaml:"relabel_configs,omitempty"`
}

type ProfilingConfig struct {
	PprofConfig PprofConfig `yaml:"pprof_config,omitempty"`
	PprofPrefix string      `yaml:"path_prefix,omitempty"`
}

type PprofConfig map[string]*PprofProfilingConfig

type PprofProfilingConfig struct {
	Enabled *bool  `yaml:"enabled,omitempty"`
	Path    string `yaml:"path,omitempty"`
	Delta   bool   `yaml:"delta,omitempty"`
}
