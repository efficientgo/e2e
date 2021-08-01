// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

package promconfig

import (
	"net/url"
	"regexp"
	"strings"

	sdconfig "github.com/efficientgo/e2e/monitoring/promconfig/discovery/config"
	"github.com/pkg/errors"
	config_util "github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
)

// NOTE(bwplotka): Stripped out Prometheus configuration from: https://github.com/prometheus/prometheus/blob/master/config/config.go
// We cannot import them directly easily as config package has cosmic number of dependencies (50+), dependency hell.
// TODO(bwplotka): Contribute and strip deps in Prometheus upstream once we agree as Prometheus maintainers if that use case we want to be support.

// Config is the top-level configuration for Prometheus's config files.
type Config struct {
	GlobalConfig   GlobalConfig    `yaml:"global"`
	AlertingConfig AlertingConfig  `yaml:"alerting,omitempty"`
	RuleFiles      []string        `yaml:"rule_files,omitempty"`
	ScrapeConfigs  []*ScrapeConfig `yaml:"scrape_configs,omitempty"`

	RemoteWriteConfigs []*RemoteWriteConfig `yaml:"remote_write,omitempty"`
	RemoteReadConfigs  []*RemoteReadConfig  `yaml:"remote_read,omitempty"`
}

// GlobalConfig configures values that are used across other configuration
// objects.
type GlobalConfig struct {
	// How frequently to scrape targets by default.
	ScrapeInterval model.Duration `yaml:"scrape_interval,omitempty"`
	// The default timeout when scraping targets.
	ScrapeTimeout model.Duration `yaml:"scrape_timeout,omitempty"`
	// How frequently to evaluate rules by default.
	EvaluationInterval model.Duration `yaml:"evaluation_interval,omitempty"`
	// The labels to add to any timeseries that this Prometheus instance scrapes.
	ExternalLabels model.LabelSet `yaml:"external_labels,omitempty"`
}

// ScrapeConfig configures a scraping unit for Prometheus.
type ScrapeConfig struct {
	// The job name to which the job label is set by default.
	JobName string `yaml:"job_name"`
	// Indicator whether the scraped metrics should remain unmodified.
	HonorLabels bool `yaml:"honor_labels,omitempty"`
	// A set of query parameters with which the target is scraped.
	Params url.Values `yaml:"params,omitempty"`
	// How frequently to scrape the targets of this scrape config.
	ScrapeInterval model.Duration `yaml:"scrape_interval,omitempty"`
	// The timeout for scraping targets of this config.
	ScrapeTimeout model.Duration `yaml:"scrape_timeout,omitempty"`
	// The HTTP resource path on which to fetch metrics from targets.
	MetricsPath string `yaml:"metrics_path,omitempty"`
	// The URL scheme with which to fetch metrics from targets.
	Scheme string `yaml:"scheme,omitempty"`
	// More than this many samples post metric-relabelling will cause the scrape to fail.
	SampleLimit uint `yaml:"sample_limit,omitempty"`

	// We cannot do proper Go type embedding below as the parser will then parse
	// values arbitrarily into the overflow maps of further-down types.

	ServiceDiscoveryConfig sdconfig.ServiceDiscoveryConfig `yaml:",inline"`
	HTTPClientConfig       config_util.HTTPClientConfig    `yaml:",inline"`

	// List of target relabel configurations.
	RelabelConfigs []*RelabelConfig `yaml:"relabel_configs,omitempty"`
	// List of metric relabel configurations.
	MetricRelabelConfigs []*RelabelConfig `yaml:"metric_relabel_configs,omitempty"`
}

// AlertingConfig configures alerting and alertmanager related configs.
type AlertingConfig struct {
	AlertRelabelConfigs []*RelabelConfig      `yaml:"alert_relabel_configs,omitempty"`
	AlertmanagerConfigs []*AlertmanagerConfig `yaml:"alertmanagers,omitempty"`
}

// AlertmanagerConfig configures how Alertmanagers can be discovered and communicated with.
type AlertmanagerConfig struct {
	// We cannot do proper Go type embedding below as the parser will then parse
	// values arbitrarily into the overflow maps of further-down types.

	ServiceDiscoveryConfig sdconfig.ServiceDiscoveryConfig `yaml:",inline"`
	HTTPClientConfig       config_util.HTTPClientConfig    `yaml:",inline"`

	// The URL scheme to use when talking to Alertmanagers.
	Scheme string `yaml:"scheme,omitempty"`
	// Path prefix to add in front of the push endpoint path.
	PathPrefix string `yaml:"path_prefix,omitempty"`
	// The timeout used when sending alerts.
	Timeout model.Duration `yaml:"timeout,omitempty"`

	// List of Alertmanager relabel configurations.
	RelabelConfigs []*RelabelConfig `yaml:"relabel_configs,omitempty"`
}

// ClientCert contains client cert credentials.
type ClientCert struct {
	Cert string             `yaml:"cert"`
	Key  config_util.Secret `yaml:"key"`
}

// FileSDConfig is the configuration for file based discovery.
type FileSDConfig struct {
	Files           []string       `yaml:"files"`
	RefreshInterval model.Duration `yaml:"refresh_interval,omitempty"`
}

// RelabelAction is the action to be performed on relabeling.
type RelabelAction string

const (
	// RelabelReplace performs a regex replacement.
	RelabelReplace RelabelAction = "replace"
	// RelabelKeep drops targets for which the input does not match the regex.
	RelabelKeep RelabelAction = "keep"
	// RelabelDrop drops targets for which the input does match the regex.
	RelabelDrop RelabelAction = "drop"
	// RelabelHashMod sets a label to the modulus of a hash of labels.
	RelabelHashMod RelabelAction = "hashmod"
	// RelabelLabelMap copies labels to other labelnames based on a regex.
	RelabelLabelMap RelabelAction = "labelmap"
	// RelabelLabelDrop drops any label matching the regex.
	RelabelLabelDrop RelabelAction = "labeldrop"
	// RelabelLabelKeep drops any label not matching the regex.
	RelabelLabelKeep RelabelAction = "labelkeep"
)

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (a *RelabelAction) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}
	switch act := RelabelAction(strings.ToLower(s)); act {
	case RelabelReplace, RelabelKeep, RelabelDrop, RelabelHashMod, RelabelLabelMap, RelabelLabelDrop, RelabelLabelKeep:
		*a = act
		return nil
	}
	return errors.Errorf("unknown relabel action %q", s)
}

// RelabelConfig is the configuration for relabeling of target label sets.
type RelabelConfig struct {
	// A list of labels from which values are taken and concatenated
	// with the configured separator in order.
	SourceLabels model.LabelNames `yaml:"source_labels,flow,omitempty"`
	// Separator is the string between concatenated values from the source labels.
	Separator string `yaml:"separator,omitempty"`
	// Regex against which the concatenation is matched.
	Regex Regexp `yaml:"regex,omitempty"`
	// Modulus to take of the hash of concatenated values from the source labels.
	Modulus uint64 `yaml:"modulus,omitempty"`
	// TargetLabel is the label to which the resulting string is written in a replacement.
	// Regexp interpolation is allowed for the replace action.
	TargetLabel string `yaml:"target_label,omitempty"`
	// Replacement is the regex replacement pattern to be used.
	Replacement string `yaml:"replacement,omitempty"`
	// Action is the action to be performed for the relabeling.
	Action RelabelAction `yaml:"action,omitempty"`
}

// Regexp encapsulates a regexp.Regexp and makes it YAML marshallable.
type Regexp struct {
	*regexp.Regexp
	original string
}

// NewRegexp creates a new anchored Regexp and returns an error if the
// passed-in regular expression does not compile.
func NewRegexp(s string) (Regexp, error) {
	regex, err := regexp.Compile("^(?:" + s + ")$")
	return Regexp{
		Regexp:   regex,
		original: s,
	}, err
}

// MustNewRegexp works like NewRegexp, but panics if the regular expression does not compile.
func MustNewRegexp(s string) Regexp {
	re, err := NewRegexp(s)
	if err != nil {
		panic(err)
	}
	return re
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (re *Regexp) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}
	r, err := NewRegexp(s)
	if err != nil {
		return err
	}
	*re = r
	return nil
}

// MarshalYAML implements the yaml.Marshaler interface.
func (re Regexp) MarshalYAML() (interface{}, error) {
	if re.original != "" {
		return re.original, nil
	}
	return nil, nil
}

// RemoteWriteConfig is the configuration for writing to remote storage.
type RemoteWriteConfig struct {
	URL                 *config_util.URL `yaml:"url"`
	RemoteTimeout       model.Duration   `yaml:"remote_timeout,omitempty"`
	WriteRelabelConfigs []*RelabelConfig `yaml:"write_relabel_configs,omitempty"`

	// We cannot do proper Go type embedding below as the parser will then parse
	// values arbitrarily into the overflow maps of further-down types.
	HTTPClientConfig config_util.HTTPClientConfig `yaml:",inline"`
	QueueConfig      QueueConfig                  `yaml:"queue_config,omitempty"`
}

// QueueConfig is the configuration for the queue used to write to remote
// storage.
type QueueConfig struct {
	// Number of samples to buffer per shard before we start dropping them.
	Capacity int `yaml:"capacity,omitempty"`

	// Max number of shards, i.e. amount of concurrency.
	MaxShards int `yaml:"max_shards,omitempty"`

	// Maximum number of samples per send.
	MaxSamplesPerSend int `yaml:"max_samples_per_send,omitempty"`

	// Maximum time sample will wait in buffer.
	BatchSendDeadline model.Duration `yaml:"batch_send_deadline,omitempty"`

	// Max number of times to retry a batch on recoverable errors.
	MaxRetries int `yaml:"max_retries,omitempty"`

	// On recoverable errors, backoff exponentially.
	MinBackoff model.Duration `yaml:"min_backoff,omitempty"`
	MaxBackoff model.Duration `yaml:"max_backoff,omitempty"`
}

// RemoteReadConfig is the configuration for reading from remote storage.
type RemoteReadConfig struct {
	URL           *config_util.URL `yaml:"url"`
	RemoteTimeout model.Duration   `yaml:"remote_timeout,omitempty"`
	ReadRecent    bool             `yaml:"read_recent,omitempty"`
	// We cannot do proper Go type embedding below as the parser will then parse
	// values arbitrarily into the overflow maps of further-down types.
	HTTPClientConfig config_util.HTTPClientConfig `yaml:",inline"`

	// RequiredMatchers is an optional list of equality matchers which have to
	// be present in a selector to query the remote read endpoint.
	RequiredMatchers model.LabelSet `yaml:"required_matchers,omitempty"`
}
