package e2eprofiling

import (
	"github.com/efficientgo/core/errors"
	"github.com/efficientgo/e2e"
	"github.com/efficientgo/e2e/profiling/parcaconfig"
)

type Target struct {
	InternalEndpoint string
	Scheme           string // "http" by default.
	Config           *parcaconfig.ProfilingConfig
}

type Profiled interface {
	ProfileTargets() []Target
}

type Runnable struct {
	e2e.Runnable

	pprofPort string
	scheme    string
	config    *parcaconfig.ProfilingConfig
}

type runnableOpt struct {
	config *parcaconfig.ProfilingConfig
	scheme string
}

// WithRunnableConfig sets a custom parca ProfilingConfig entry about this runnable. Empty by default (Parca defaults apply).
func WithRunnableConfig(config parcaconfig.ProfilingConfig) RunnableOption {
	return func(o *runnableOpt) {
		o.config = &config
	}
}

// WithRunnableScheme allows adding customized scheme. "http" or "https" values allowed. "http" by default.
// If "https" is specified, insecure TLS will be performed.
func WithRunnableScheme(scheme string) RunnableOption {
	return func(o *runnableOpt) {
		o.scheme = scheme
	}
}

type RunnableOption func(*runnableOpt)

// AsProfiled wraps e2e.Runnable with Runnable that satisfies both Profiled and e2e.Runnable
// that represents runnable with pprof HTTP handlers exposed.
// NOTE(bwplotka): Caller is expected to discard passed `r` runnable and use returned Runnable.Runnable instead.
func AsProfiled(r e2e.Runnable, pprofPortName string, opts ...RunnableOption) *Runnable {
	opt := runnableOpt{
		scheme: "http",
	}
	for _, o := range opts {
		o(&opt)
	}

	if r.InternalEndpoint(pprofPortName) == "" {
		return &Runnable{Runnable: e2e.NewErrorer(r.Name(), errors.Newf("pporf port name %v does not exists in given runnable ports", pprofPortName))}
	}

	instr := &Runnable{
		Runnable:  r,
		pprofPort: pprofPortName,
		scheme:    opt.scheme,
		config:    opt.config,
	}
	r.SetMetadata(metaKey, Profiled(instr))
	return instr
}

func (r *Runnable) ProfileTargets() []Target {
	return []Target{{Scheme: r.scheme, Config: r.config, InternalEndpoint: r.InternalEndpoint(r.pprofPort)}}
}
