// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

package e2emon

import (
	"math"
	"time"

	"github.com/efficientgo/core/backoff"
	"github.com/efficientgo/e2e/monitoring/matchers"
	io_prometheus_client "github.com/prometheus/client_model/go"
)

// GetMetricValueFunc defined the signature of a function used to get the metric value.
type getMetricValueFunc func(m *io_prometheus_client.Metric) float64

// MetricsOption defined the signature of a function used to manipulate options.
type MetricsOption func(*metricsOptions)

// metricsOptions is the structure holding all options.
type metricsOptions struct {
	getValue           getMetricValueFunc
	labelMatchers      []*matchers.Matcher
	waitMissingMetrics bool
	skipMissingMetrics bool
	waitBackoff        *backoff.Config
}

// WithWaitBackoff is an option to configure a backoff when waiting on a metric value.
func WithWaitBackoff(backoffConfig *backoff.Config) MetricsOption {
	return func(o *metricsOptions) {
		o.waitBackoff = backoffConfig
	}
}

// WithMetricCount is an option to get the histogram/summary count as metric value.
func WithMetricCount() MetricsOption {
	return func(o *metricsOptions) {
		o.getValue = getMetricCount
	}
}

// WithLabelMatchers is an option to filter only matching series.
func WithLabelMatchers(matchers ...*matchers.Matcher) MetricsOption {
	return func(o *metricsOptions) {
		o.labelMatchers = matchers
	}
}

// WaitMissingMetrics is an option to wait whenever an expected metric is missing. If this
// option is not enabled, will return error on missing metrics.
func WaitMissingMetrics() MetricsOption {
	return func(o *metricsOptions) {
		o.waitMissingMetrics = true
	}
}

// SkipMissingMetrics is an option to skip/ignore whenever an expected metric is missing.
func SkipMissingMetrics() MetricsOption {
	return func(o *metricsOptions) {
		o.skipMissingMetrics = true
	}
}

func buildMetricsOptions(opts []MetricsOption) metricsOptions {
	result := metricsOptions{
		getValue: getMetricValue,
		waitBackoff: &backoff.Config{
			Min:        300 * time.Millisecond,
			Max:        600 * time.Millisecond,
			MaxRetries: 50,
		},
	}
	for _, opt := range opts {
		opt(&result)
	}
	return result
}

func getMetricValue(m *io_prometheus_client.Metric) float64 {
	if m.GetGauge() != nil {
		return m.GetGauge().GetValue()
	} else if m.GetCounter() != nil {
		return m.GetCounter().GetValue()
	} else if m.GetHistogram() != nil {
		return m.GetHistogram().GetSampleSum()
	} else if m.GetSummary() != nil {
		return m.GetSummary().GetSampleSum()
	} else {
		return 0
	}
}

func getMetricCount(m *io_prometheus_client.Metric) float64 {
	if m.GetHistogram() != nil {
		return float64(m.GetHistogram().GetSampleCount())
	} else if m.GetSummary() != nil {
		return float64(m.GetSummary().GetSampleCount())
	} else {
		return 0
	}
}

func getValues(metrics []*io_prometheus_client.Metric, opts metricsOptions) []float64 {
	values := make([]float64, 0, len(metrics))
	for _, m := range metrics {
		values = append(values, opts.getValue(m))
	}
	return values
}

func filterMetrics(metrics []*io_prometheus_client.Metric, opts metricsOptions) []*io_prometheus_client.Metric {
	// If no label matcher is configured, then no filtering should be done.
	if len(opts.labelMatchers) == 0 {
		return metrics
	}
	if len(metrics) == 0 {
		return metrics
	}

	filtered := make([]*io_prometheus_client.Metric, 0, len(metrics))

	for _, m := range metrics {
		metricLabels := map[string]string{}
		for _, lp := range m.GetLabel() {
			metricLabels[lp.GetName()] = lp.GetValue()
		}

		matches := true
		for _, matcher := range opts.labelMatchers {
			if !matcher.Matches(metricLabels[matcher.Name]) {
				matches = false
				break
			}
		}

		if !matches {
			continue
		}

		filtered = append(filtered, m)
	}

	return filtered
}

func SumValues(values []float64) float64 {
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum
}

func EqualsSingle(expected float64) func(float64) bool {
	return func(v float64) bool {
		return v == expected || (math.IsNaN(v) && math.IsNaN(expected))
	}
}

type MetricValueExpectation func(sums ...float64) bool

// Equals is an MetricValueExpectation function for WaitSumMetrics that returns true if given single sum is equals to given value.
func Equals(value float64) MetricValueExpectation {
	return func(sums ...float64) bool {
		if len(sums) != 1 {
			panic("equals: expected one value")
		}
		return sums[0] == value || math.IsNaN(sums[0]) && math.IsNaN(value)
	}
}

// Greater is an isExpected function for WaitSumMetrics that returns true if given single sum is greater than given value.
func Greater(value float64) MetricValueExpectation {
	return func(sums ...float64) bool {
		if len(sums) != 1 {
			panic("greater: expected one value")
		}
		return sums[0] > value
	}
}

// GreaterOrEqual is an isExpected function for WaitSumMetrics that returns true if given single sum is greater or equal than given value.
func GreaterOrEqual(value float64) MetricValueExpectation {
	return func(sums ...float64) bool {
		if len(sums) != 1 {
			panic("greater: expected one value")
		}
		return sums[0] >= value
	}
}

// Less is an isExpected function for WaitSumMetrics that returns true if given single sum is less than given value.
func Less(value float64) MetricValueExpectation {
	return func(sums ...float64) bool {
		if len(sums) != 1 {
			panic("less: expected one value")
		}
		return sums[0] < value
	}
}

// Between is a MetricValueExpectation function for WaitSumMetrics that returns true if given single sum is between
// the lower and upper bounds (non-inclusive, as in `lower < x < upper`).
func Between(lower, upper float64) MetricValueExpectation {
	return func(sums ...float64) bool {
		if len(sums) != 1 {
			panic("between: expected one value")
		}
		return sums[0] > lower && sums[0] < upper
	}
}

// EqualsAmongTwo is an isExpected function for WaitSumMetrics that returns true if first sum is equal to the second.
// NOTE: Be careful on scrapes in between of process that changes two metrics. Those are
// usually not atomic.
func EqualsAmongTwo(sums ...float64) bool {
	if len(sums) != 2 {
		panic("equalsAmongTwo: expected two values")
	}
	return sums[0] == sums[1]
}

// GreaterAmongTwo is an isExpected function for WaitSumMetrics that returns true if first sum is greater than second.
// NOTE: Be careful on scrapes in between of process that changes two metrics. Those are
// usually not atomic.
func GreaterAmongTwo(sums ...float64) bool {
	if len(sums) != 2 {
		panic("greaterAmongTwo: expected two values")
	}
	return sums[0] > sums[1]
}

// LessAmongTwo is an isExpected function for WaitSumMetrics that returns true if first sum is smaller than second.
// NOTE: Be careful on scrapes in between of process that changes two metrics. Those are
// usually not atomic.
func LessAmongTwo(sums ...float64) bool {
	if len(sums) != 2 {
		panic("lessAmongTwo: expected two values")
	}
	return sums[0] < sums[1]
}
