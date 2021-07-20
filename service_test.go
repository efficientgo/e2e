// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

package e2e

import (
	"math"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/efficientgo/tools/core/pkg/backoff"
	"github.com/efficientgo/tools/core/pkg/testutil"
)

func TestWaitSumMetric(t *testing.T) {
	// Listen on a random port before starting the HTTP server, to
	// make sure the port is already open when we'll call WaitSumMetric()
	// the first time (this avoid flaky tests).
	ln, err := net.Listen("tcp", "localhost:0")
	testutil.Ok(t, err)
	defer ln.Close()

	// Get the port.
	_, addrPort, err := net.SplitHostPort(ln.Addr().String())
	testutil.Ok(t, err)

	port, err := strconv.Atoi(addrPort)
	testutil.Ok(t, err)

	// Start an HTTP server exposing the metrics.
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`
# HELP metric_c cheescake
# TYPE metric_c gauge
metric_c 20
# HELP metric_a cheescake
# TYPE metric_a gauge
metric_a 1
metric_a{first="value1"} 10
metric_a{first="value1", something="x"} 4
metric_a{first="value1", something2="a"} 203
metric_a{first="value2"} 2
metric_a{second="value1"} 1
# HELP metric_b cheescake
# TYPE metric_b gauge
metric_b 1000
# HELP metric_b_counter cheescake
# TYPE metric_b_counter counter
metric_b_counter 1020
# HELP metric_b_hist cheescake
# TYPE metric_b_hist histogram
metric_b_hist_count 5
metric_b_hist_sum 124
metric_b_hist_bucket{le="5.36870912e+08"} 1
metric_b_hist_bucket{le="+Inf"} 5
# HELP metric_b_summary cheescake
# TYPE metric_b_summary summary
metric_b_summary_sum 22
metric_b_summary_count 1
`))
		}),
	}
	defer srv.Close()

	go func() {
		_ = srv.Serve(ln)
	}()

	s := &HTTPService{
		httpPort: 0,
		ConcreteService: &ConcreteService{
			networkPortsContainerToLocal: map[int]int{
				0: port,
			},
		},
	}

	s.SetBackoff(backoff.Config{
		Min:        300 * time.Millisecond,
		Max:        600 * time.Millisecond,
		MaxRetries: 50,
	})
	testutil.Ok(t, s.WaitSumMetrics(Equals(221), "metric_a"))

	// No retry.
	s.SetBackoff(backoff.Config{
		Min:        0,
		Max:        0,
		MaxRetries: 1,
	})
	testutil.NotOk(t, s.WaitSumMetrics(Equals(16), "metric_a"))

	testutil.Ok(t, s.WaitSumMetrics(Equals(1000), "metric_b"))
	testutil.Ok(t, s.WaitSumMetrics(Equals(1020), "metric_b_counter"))
	testutil.Ok(t, s.WaitSumMetrics(Equals(124), "metric_b_hist"))
	testutil.Ok(t, s.WaitSumMetrics(Equals(22), "metric_b_summary"))

	testutil.Ok(t, s.WaitSumMetrics(EqualsAmongTwo, "metric_a", "metric_a"))
	testutil.NotOk(t, s.WaitSumMetrics(EqualsAmongTwo, "metric_a", "metric_b"))

	testutil.Ok(t, s.WaitSumMetrics(GreaterAmongTwo, "metric_b", "metric_a"))
	testutil.NotOk(t, s.WaitSumMetrics(GreaterAmongTwo, "metric_a", "metric_b"))

	testutil.Ok(t, s.WaitSumMetrics(LessAmongTwo, "metric_a", "metric_b"))
	testutil.NotOk(t, s.WaitSumMetrics(LessAmongTwo, "metric_b", "metric_a"))

	testutil.NotOk(t, s.WaitSumMetrics(Equals(0), "non_existing_metric"))
}

func TestWaitSumMetric_Nan(t *testing.T) {
	// Listen on a random port before starting the HTTP server, to
	// make sure the port is already open when we'll call WaitSumMetric()
	// the first time (this avoid flaky tests).
	ln, err := net.Listen("tcp", "localhost:0")
	testutil.Ok(t, err)
	defer ln.Close()

	// Get the port.
	_, addrPort, err := net.SplitHostPort(ln.Addr().String())
	testutil.Ok(t, err)

	port, err := strconv.Atoi(addrPort)
	testutil.Ok(t, err)

	// Start an HTTP server exposing the metrics.
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`
# HELP metric_c cheescake
# TYPE metric_c GAUGE
metric_c 20
# HELP metric_a cheescake
# TYPE metric_a GAUGE
metric_a 1
metric_a{first="value1"} 10
metric_a{first="value1", something="x"} 4
metric_a{first="value1", something2="a"} 203
metric_a{first="value1", something3="b"} Nan
metric_a{first="value2"} 2
metric_a{second="value1"} 1
# HELP metric_b cheescake
# TYPE metric_b GAUGE
metric_b 1000
`))
		}),
	}
	defer srv.Close()

	go func() {
		_ = srv.Serve(ln)
	}()

	s := &HTTPService{
		httpPort: 0,
		ConcreteService: &ConcreteService{
			networkPortsContainerToLocal: map[int]int{
				0: port,
			},
		},
	}

	s.SetBackoff(backoff.Config{
		Min:        300 * time.Millisecond,
		Max:        600 * time.Millisecond,
		MaxRetries: 50,
	})
	testutil.Ok(t, s.WaitSumMetrics(Equals(math.NaN()), "metric_a"))
}
