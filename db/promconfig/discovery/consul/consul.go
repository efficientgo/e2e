// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

// Copyright (c) bwplotka/mimic Authors
// Licensed under the Apache License 2.0.

// Copyright 2015 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package consul

import (
	config_util "github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
)

// SDConfig is the configuration for Consul service discovery.
type SDConfig struct {
	Server       string             `yaml:"server,omitempty"`
	Token        config_util.Secret `yaml:"token,omitempty"`
	Datacenter   string             `yaml:"datacenter,omitempty"`
	TagSeparator string             `yaml:"tag_separator,omitempty"`
	Scheme       string             `yaml:"scheme,omitempty"`
	Username     string             `yaml:"username,omitempty"`
	Password     config_util.Secret `yaml:"password,omitempty"`

	// See https://www.consul.io/docs/internals/consensus.html#consistency-modes,
	// stale reads are a lot cheaper and are a necessity if you have >5k targets.
	AllowStale bool `yaml:"allow_stale"`
	// By default use blocking queries (https://www.consul.io/api/index.html#blocking-queries)
	// but allow users to throttle updates if necessary. This can be useful because of "bugs" like
	// https://github.com/hashicorp/consul/issues/3712 which cause an un-necessary
	// amount of requests on consul.
	RefreshInterval model.Duration `yaml:"refresh_interval,omitempty"`

	// See https://www.consul.io/api/catalog.html#list-services
	// The list of services for which targets are discovered.
	// Defaults to all services if empty.
	Services []string `yaml:"services,omitempty"`
	// An optional tag used to filter instances inside a service. A single tag is supported
	// here to match the Consul API.
	ServiceTag string `yaml:"tag,omitempty"`
	// Desired node metadata.
	NodeMeta map[string]string `yaml:"node_meta,omitempty"`

	TLSConfig config_util.TLSConfig `yaml:"tls_config,omitempty"`
}
