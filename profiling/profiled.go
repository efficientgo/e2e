// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

package e2eprof

import (
	"github.com/efficientgo/core/errors"
	"github.com/efficientgo/e2e"
	"github.com/efficientgo/e2e/profiling/parcaconfig"
)

type Target struct {
	Name             string // Represents runnable job and will be used for scrape job name.
	InternalEndpoint string
	Scheme           string // "http" by default.
	Config           *parcaconfig.ProfilingConfig
}

type Profiled interface {
	ProfileTargets() []Target
}

// ProfiledRunnable represents runnable with pprof HTTP handlers exposed.
type ProfiledRunnable struct {
	e2e.Runnable

	pprofPort string
	scheme    string
	config    *parcaconfig.ProfilingConfig
}

type rOpt struct {
	config *parcaconfig.ProfilingConfig
	scheme string
}

// WithProfiledConfig sets a custom parca ProfilingConfig entry about this runnable. Empty by default (Parca defaults apply).
func WithProfiledConfig(config parcaconfig.ProfilingConfig) ProfiledOption {
	return func(o *rOpt) {
		o.config = &config
	}
}

// WithProfiledScheme allows adding customized scheme. "http" or "https" values allowed. "http" by default.
// If "https" is specified, insecure TLS will be performed.
func WithProfiledScheme(scheme string) ProfiledOption {
	return func(o *rOpt) {
		o.scheme = scheme
	}
}

type ProfiledOption func(*rOpt)

// AsProfiled wraps e2e.Runnable with ProfiledRunnable.
// If runnable is running during invocation AsProfiled panics.
// NOTE(bwplotka): Caller is expected to discard passed `r` runnable and use returned ProfiledRunnable.Runnable instead.
func AsProfiled(r e2e.Runnable, pprofPortName string, opts ...ProfiledOption) *ProfiledRunnable {
	if r.IsRunning() {
		panic("can't use AsProfiled with running runnable")
	}

	opt := rOpt{
		scheme: "http",
	}
	for _, o := range opts {
		o(&opt)
	}

	if r.InternalEndpoint(pprofPortName) == "" {
		return &ProfiledRunnable{Runnable: e2e.NewFailedRunnable(r.Name(), errors.Newf("pprof port name %v does not exist in given runnable ports", pprofPortName))}
	}

	instr := &ProfiledRunnable{
		Runnable:  r,
		pprofPort: pprofPortName,
		scheme:    opt.scheme,
		config:    opt.config,
	}
	r.SetMetadata(metaKey, Profiled(instr))
	return instr
}

func (r *ProfiledRunnable) ProfileTargets() []Target {
	return []Target{{Name: r.Name(), Scheme: r.scheme, Config: r.config, InternalEndpoint: r.InternalEndpoint(r.pprofPort)}}
}
