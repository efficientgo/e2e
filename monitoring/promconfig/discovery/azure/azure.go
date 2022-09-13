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

package azure

import (
	config_util "github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
)

// SDConfig is the configuration for Azure based service discovery.
type SDConfig struct {
	Environment     string             `yaml:"environment,omitempty"`
	Port            int                `yaml:"port"`
	SubscriptionID  string             `yaml:"subscription_id"`
	TenantID        string             `yaml:"tenant_id,omitempty"`
	ClientID        string             `yaml:"client_id,omitempty"`
	ClientSecret    config_util.Secret `yaml:"client_secret,omitempty"`
	RefreshInterval model.Duration     `yaml:"refresh_interval,omitempty"`
}
