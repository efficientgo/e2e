// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

package e2emonitoring

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/efficientgo/core/backoff"
	"github.com/efficientgo/core/errcapture"
	"github.com/efficientgo/core/errors"
	"github.com/efficientgo/e2e"
	"github.com/prometheus/common/expfmt"
)

var errMissingMetric = errors.New("metric not found")

type Target struct {
	InternalEndpoint string
	MetricPath       string // "/metrics" by default.
	Scheme           string // "http" by default.
}

type Instrumented interface {
	MetricTargets() []Target
	Metrics() (string, error)
	WaitSumMetrics(expected MetricValueExpectation, metricNames ...string) error
	WaitSumMetricsWithOptions(expected MetricValueExpectation, metricNames []string, opts ...MetricsOption) error
	SumMetrics(metricNames []string, opts ...MetricsOption) ([]float64, error)
	WaitRemovedMetric(metricName string, opts ...MetricsOption) error
}

type InstrumentedRunnable struct {
	e2e.Runnable

	metricPortName string
	metricPath     string
	scheme         string

	waitBackoff *backoff.Backoff
}

type runnableOpt struct {
	metricPath  string
	scheme      string
	waitBackoff *backoff.Backoff
}

// WithRunnableMetricPath sets a custom path for metrics page. "/metrics" by default.
func WithRunnableMetricPath(metricPath string) RunnableOption {
	return func(o *runnableOpt) {
		o.metricPath = metricPath
	}
}

// WithRunnableScheme allows adding customized scheme. "http" or "https" values allowed. "http" by default.
// If "https" is specified, insecure TLS will be performed.
func WithRunnableScheme(scheme string) RunnableOption {
	return func(o *runnableOpt) {
		o.scheme = scheme
	}
}

// WithRunnableWaitBackoff allows adding customized scheme. "http" or "https" values allowed. "http" by default.
// If "https" is specified, insecure TLS will be performed.
func WithRunnableWaitBackoff(waitBackoff *backoff.Backoff) RunnableOption {
	return func(o *runnableOpt) {
		o.waitBackoff = waitBackoff
	}
}

type RunnableOption func(*runnableOpt)

// AsInstrumented wraps e2e.Runnable with InstrumentedRunnable that satisfies both Instrumented and e2e.Runnable
// that represents runnable with instrumented Prometheus metric endpoint on a certain port.
// NOTE(bwplotka): Caller is expected to discard passed `r` runnable and use returned InstrumentedRunnable.InstrumentedRunnable instead.
func AsInstrumented(r e2e.Runnable, instrumentedPortName string, opts ...RunnableOption) *InstrumentedRunnable {
	opt := runnableOpt{
		metricPath: "/metrics",
		scheme:     "http",
		waitBackoff: backoff.New(context.Background(), backoff.Config{
			Min:        300 * time.Millisecond,
			Max:        600 * time.Millisecond,
			MaxRetries: 50, // Sometimes the CI is slow ¯\_(ツ)_/¯
		})}
	for _, o := range opts {
		o(&opt)
	}

	if r.InternalEndpoint(instrumentedPortName) == "" {
		return &InstrumentedRunnable{Runnable: e2e.NewErrorer(r.Name(), errors.Newf("metric port name %v does not exists in given runnable ports", instrumentedPortName))}
	}

	instr := &InstrumentedRunnable{
		Runnable:       r,
		metricPortName: instrumentedPortName,
		metricPath:     opt.metricPath,
		scheme:         opt.scheme,
		waitBackoff:    opt.waitBackoff,
	}
	r.SetMetadata(metaKey, Instrumented(instr))
	return instr
}

func (r *InstrumentedRunnable) MetricTargets() []Target {
	return []Target{{Scheme: r.scheme, MetricPath: r.metricPath, InternalEndpoint: r.InternalEndpoint(r.metricPortName)}}
}

func (r *InstrumentedRunnable) Metrics() (_ string, err error) {
	if !r.IsRunning() {
		return "", errors.Newf("%s is not running", r.Name())
	}

	// Fetch metrics.
	res, err := (&http.Client{Timeout: 5 * time.Second}).Get(fmt.Sprintf("http://%s/metrics", r.Endpoint(r.metricPortName)))
	if err != nil {
		return "", err
	}

	// Check the status code.
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", errors.Newf("unexpected status code %d while fetching metrics", res.StatusCode)
	}
	defer errcapture.ExhaustClose(&err, res.Body, "metrics response")

	body, err := io.ReadAll(res.Body)
	return string(body), err
}

// WaitSumMetrics waits for at least one instance of each given metric names to be present and their sums,
// returning true when passed to given expected(...).
func (r *InstrumentedRunnable) WaitSumMetrics(expected MetricValueExpectation, metricNames ...string) error {
	return r.WaitSumMetricsWithOptions(expected, metricNames)
}

func (r *InstrumentedRunnable) WaitSumMetricsWithOptions(expected MetricValueExpectation, metricNames []string, opts ...MetricsOption) error {
	var (
		sums    []float64
		err     error
		options = buildMetricsOptions(opts)
	)

	metricsWaitBackoff := backoff.New(context.Background(), *options.waitBackoff)
	for metricsWaitBackoff.Reset(); metricsWaitBackoff.Ongoing(); {
		sums, err = r.SumMetrics(metricNames, opts...)
		if options.waitMissingMetrics && errors.Is(err, errMissingMetric) {
			metricsWaitBackoff.Wait()
			continue
		}
		if err != nil {
			return err
		}

		if expected(sums...) {
			return nil
		}

		metricsWaitBackoff.Wait()
	}
	return errors.Newf("unable to find metrics %s with expected values after %d retries. Last error: %v. Last values: %v", metricNames, metricsWaitBackoff.NumRetries(), err, sums)
}

// SumMetrics returns the sum of the values of each given metric names.
func (r *InstrumentedRunnable) SumMetrics(metricNames []string, opts ...MetricsOption) ([]float64, error) {
	options := buildMetricsOptions(opts)
	sums := make([]float64, len(metricNames))

	metrics, err := r.Metrics()
	if err != nil {
		return nil, err
	}

	var tp expfmt.TextParser
	families, err := tp.TextToMetricFamilies(strings.NewReader(metrics))
	if err != nil {
		return nil, err
	}

	for i, m := range metricNames {
		sums[i] = 0.0

		// Get the metric family.
		mf, ok := families[m]
		if !ok {
			if options.skipMissingMetrics {
				continue
			}

			return nil, errors.Wrapf(errMissingMetric, "metric=%s service=%s", m, r.Name())
		}

		// Filter metrics.
		metrics := filterMetrics(mf.GetMetric(), options)
		if len(metrics) == 0 {
			if options.skipMissingMetrics {
				continue
			}

			return nil, errors.Wrapf(errMissingMetric, "metric=%s service=%s", m, r.Name())
		}

		sums[i] = SumValues(getValues(metrics, options))
	}

	return sums, nil
}

// WaitRemovedMetric waits until a metric disappear from the list of metrics exported by the service.
func (r *InstrumentedRunnable) WaitRemovedMetric(metricName string, opts ...MetricsOption) error {
	options := buildMetricsOptions(opts)

	for r.waitBackoff.Reset(); r.waitBackoff.Ongoing(); {
		// Fetch metrics.
		metrics, err := r.Metrics()
		if err != nil {
			return err
		}

		// Parse metrics.
		var tp expfmt.TextParser
		families, err := tp.TextToMetricFamilies(strings.NewReader(metrics))
		if err != nil {
			return err
		}

		// Get the metric family.
		mf, ok := families[metricName]
		if !ok {
			return nil
		}

		// Filter metrics.
		if len(filterMetrics(mf.GetMetric(), options)) == 0 {
			return nil
		}

		r.waitBackoff.Wait()
	}

	return errors.Newf("the metric %s is still exported by %s", metricName, r.Name())
}
