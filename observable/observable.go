// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

package e2eobs

import (
	"github.com/efficientgo/core/backoff"
	"github.com/efficientgo/e2e"
	e2emon "github.com/efficientgo/e2e/monitoring"
	e2eprof "github.com/efficientgo/e2e/profiling"
	"github.com/efficientgo/e2e/profiling/parcaconfig"
)

// Observable represents a runnable that is both instrumented and profiled. Typically, all well-written Go services
// are observable.
type Observable struct {
	e2e.Runnable
	e2emon.Instrumented
	e2eprof.Profiled
}

type opt struct {
	instrOpts []e2emon.InstrumentedOption
	profOpts  []e2eprof.ProfiledOption
}

// WithScheme allows adding a customized scheme. "http" or "https" values are allowed. "http" by default.
// If "https" is specified, insecure TLS will be performed.
func WithScheme(scheme string) Option {
	return func(o *opt) {
		o.instrOpts = append(o.instrOpts, e2emon.WithInstrumentedScheme(scheme))
		o.profOpts = append(o.profOpts, e2eprof.WithProfiledScheme(scheme))
	}
}

// WithMetricPath sets a custom path for metrics page. "/metrics" by default.
func WithMetricPath(metricPath string) Option {
	return func(o *opt) {
		o.instrOpts = append(o.instrOpts, e2emon.WithInstrumentedMetricPath(metricPath))
	}
}

// WithProfiledConfig sets a custom parca ProfilingConfig entry about this runnable. Empty by default (Parca defaults apply).
func WithProfiledConfig(config parcaconfig.ProfilingConfig) Option {
	return func(o *opt) {
		o.profOpts = append(o.profOpts, e2eprof.WithProfiledConfig(config))
	}
}

// WithInstrumentedWaitBackoff allows customizing wait backoff when accessing metric endpoint.
func WithInstrumentedWaitBackoff(waitBackoff *backoff.Backoff) Option {
	return func(o *opt) {
		o.instrOpts = append(o.instrOpts, e2emon.WithInstrumentedWaitBackoff(waitBackoff))
	}
}

type Option func(*opt)

// AsObservable wraps e2e.Runnable with Observable.
// If runnable is running during invocation AsObservable panics.
// NOTE(bwplotka): Caller is expected to discard passed `r` runnable and use returned Observable.Runnable instead.
func AsObservable(r e2e.Runnable, instrumentedAndProfiledPortName string, opts ...Option) *Observable {
	if r.IsRunning() {
		panic("can't use AsObservable with running runnable")
	}

	opt := opt{}
	for _, o := range opts {
		o(&opt)
	}

	instr := e2emon.AsInstrumented(r, instrumentedAndProfiledPortName, opt.instrOpts...)
	prof := e2eprof.AsProfiled(instr, instrumentedAndProfiledPortName, opt.profOpts...)
	return &Observable{
		Runnable:     prof.Runnable, // Make sure to not use discarded `r` as `As*` API specify.
		Instrumented: instr,
		Profiled:     prof,
	}
}
