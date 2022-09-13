// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

// Copyright (c) bwplotka/mimic Authors
// Licensed under the Apache License 2.0.

// Copyright 2016 The Prometheus Authors
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

package kubernetes

import (
	config_util "github.com/prometheus/common/config"
)

// Role is role of the service in Kubernetes.
type Role string

// The valid options for Role.
const (
	RoleNode     Role = "node"
	RolePod      Role = "pod"
	RoleService  Role = "service"
	RoleEndpoint Role = "endpoints"
	RoleIngress  Role = "ingress"
)

// SDConfig is the configuration for Kubernetes service discovery.
type SDConfig struct {
	APIServer          config_util.URL        `yaml:"api_server,omitempty"`
	Role               Role                   `yaml:"role"`
	BasicAuth          *config_util.BasicAuth `yaml:"basic_auth,omitempty"`
	BearerToken        config_util.Secret     `yaml:"bearer_token,omitempty"`
	BearerTokenFile    string                 `yaml:"bearer_token_file,omitempty"`
	TLSConfig          config_util.TLSConfig  `yaml:"tls_config,omitempty"`
	NamespaceDiscovery NamespaceDiscovery     `yaml:"namespaces,omitempty"`
}

// NamespaceDiscovery is the configuration for discovering
// Kubernetes namespaces.
type NamespaceDiscovery struct {
	Names []string `yaml:"names"`
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (c *NamespaceDiscovery) UnmarshalYAML(unmarshal func(interface{}) error) error {
	*c = NamespaceDiscovery{}
	type plain NamespaceDiscovery
	return unmarshal((*plain)(c))
}
