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

package marathon

import (
	config_util "github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
)

// SDConfig is the configuration for services running on Marathon.
type SDConfig struct {
	Servers          []string                     `yaml:"servers,omitempty"`
	RefreshInterval  model.Duration               `yaml:"refresh_interval,omitempty"`
	AuthToken        config_util.Secret           `yaml:"auth_token,omitempty"`
	AuthTokenFile    string                       `yaml:"auth_token_file,omitempty"`
	HTTPClientConfig config_util.HTTPClientConfig `yaml:",inline"`
}
