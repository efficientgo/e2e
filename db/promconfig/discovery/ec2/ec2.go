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

package ec2

import (
	"github.com/prometheus/common/model"

	config_util "github.com/prometheus/common/config"
)

// Filter is the configuration for filtering EC2 instances.
type Filter struct {
	Name   string   `yaml:"name"`
	Values []string `yaml:"values"`
}

// SDConfig is the configuration for EC2 based service discovery.
type SDConfig struct {
	Endpoint        string             `yaml:"endpoint"`
	Region          string             `yaml:"region"`
	AccessKey       string             `yaml:"access_key,omitempty"`
	SecretKey       config_util.Secret `yaml:"secret_key,omitempty"`
	Profile         string             `yaml:"profile,omitempty"`
	RoleARN         string             `yaml:"role_arn,omitempty"`
	RefreshInterval model.Duration     `yaml:"refresh_interval,omitempty"`
	Port            int                `yaml:"port"`
	Filters         []*Filter          `yaml:"filters"`
}
