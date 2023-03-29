// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

package e2e_test

import (
	"bytes"
	"io"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/efficientgo/core/testutil"
	"github.com/efficientgo/e2e"
	e2edb "github.com/efficientgo/e2e/db"
	e2emon "github.com/efficientgo/e2e/monitoring"
)

func wgetFlagsCmd(hostPort string) e2e.Command {
	return e2e.NewCommandWithoutEntrypoint("/bin/sh", "-c", "wget http://"+hostPort+"/api/v1/status/flags -O /tmp/flags && cat /tmp/flags")
}

func testEnvironment(t *testing.T, e e2e.Environment) {
	p1 := e2edb.NewPrometheus(e, "prometheus-1")
	testutil.Equals(t, "prometheus-1", p1.Name())
	testutil.Equals(t, filepath.Join(e.SharedDir(), "data", p1.Name()), p1.Dir())
	testutil.Equals(t, "", p1.InternalEndpoint("not-existing"))
	testutil.Assert(t, !p1.IsRunning())

	// Errors as p1 was not started yet.
	testutil.NotOk(t, p1.WaitReady())
	testutil.Ok(t, p1.Stop())
	testutil.Ok(t, p1.Kill())

	testutil.NotOk(t, p1.Exec(wgetFlagsCmd("localhost:9090")))
	testutil.Equals(t, "stopped", p1.Endpoint("http"))
	testutil.Equals(t, "stopped", p1.Endpoint("not-existing"))

	p2 := e2edb.NewPrometheus(e, "prometheus-2")
	testutil.Ok(t, e2e.StartAndWaitReady(p1, p2))
	testutil.Ok(t, p1.WaitReady())
	testutil.Ok(t, p1.WaitReady())

	testutil.Ok(t, p1.WaitSumMetrics(e2emon.Greater(50), "prometheus_tsdb_head_samples_appended_total"))

	testutil.Equals(t, "prometheus-1", p1.Name())
	testutil.Equals(t, filepath.Join(e.SharedDir(), "data", p1.Name()), p1.Dir())
	testutil.Equals(t, "", p1.InternalEndpoint("not-existing"))
	testutil.Assert(t, p1.IsRunning())

	expectedFlagsOutputProm1 := "{\"status\":\"success\",\"data\":{\"alertmanager.notification-queue-capacity\":\"10000\",\"alertmanager.timeout\":\"\",\"config.file\":\"" + filepath.Join(e.SharedDir(), "/data/prometheus-1/prometheus.yml") + "\",\"enable-feature\":\"\",\"log.format\":\"logfmt\",\"log.level\":\"info\",\"query.lookback-delta\":\"5m\",\"query.max-concurrency\":\"20\",\"query.max-samples\":\"50000000\",\"query.timeout\":\"2m\",\"rules.alert.for-grace-period\":\"10m\",\"rules.alert.for-outage-tolerance\":\"1h\",\"rules.alert.resend-delay\":\"1m\",\"scrape.adjust-timestamps\":\"true\",\"scrape.discovery-reload-interval\":\"5s\",\"scrape.timestamp-tolerance\":\"2ms\",\"storage.agent.no-lockfile\":\"false\",\"storage.agent.path\":\"data-agent/\",\"storage.agent.retention.max-time\":\"0s\",\"storage.agent.retention.min-time\":\"0s\",\"storage.agent.wal-compression\":\"true\",\"storage.agent.wal-segment-size\":\"0B\",\"storage.agent.wal-truncate-frequency\":\"0s\",\"storage.remote.flush-deadline\":\"1m\",\"storage.remote.read-concurrent-limit\":\"10\",\"storage.remote.read-max-bytes-in-frame\":\"1048576\",\"storage.remote.read-sample-limit\":\"50000000\",\"storage.tsdb.allow-overlapping-blocks\":\"false\",\"storage.tsdb.head-chunks-write-queue-size\":\"0\",\"storage.tsdb.max-block-chunk-segment-size\":\"0B\",\"storage.tsdb.max-block-duration\":\"2h\",\"storage.tsdb.min-block-duration\":\"2h\",\"storage.tsdb.no-lockfile\":\"false\",\"storage.tsdb.path\":\"" + filepath.Join(e.SharedDir(), "/data/prometheus-1/") + "\",\"storage.tsdb.retention\":\"0s\",\"storage.tsdb.retention.size\":\"0B\",\"storage.tsdb.retention.time\":\"0s\",\"storage.tsdb.wal-compression\":\"true\",\"storage.tsdb.wal-segment-size\":\"0B\",\"web.config.file\":\"\",\"web.console.libraries\":\"console_libraries\",\"web.console.templates\":\"consoles\",\"web.cors.origin\":\".*\",\"web.enable-admin-api\":\"false\",\"web.enable-lifecycle\":\"false\",\"web.enable-remote-write-receiver\":\"false\",\"web.external-url\":\"\",\"web.listen-address\":\":9090\",\"web.max-connections\":\"512\",\"web.page-title\":\"Prometheus Time Series Collection and Processing Server\",\"web.read-timeout\":\"5m\",\"web.route-prefix\":\"/\",\"web.user-assets\":\"\"}}"
	expectedFlagsOutputProm2 := "{\"status\":\"success\",\"data\":{\"alertmanager.notification-queue-capacity\":\"10000\",\"alertmanager.timeout\":\"\",\"config.file\":\"" + filepath.Join(e.SharedDir(), "/data/prometheus-2/prometheus.yml") + "\",\"enable-feature\":\"\",\"log.format\":\"logfmt\",\"log.level\":\"info\",\"query.lookback-delta\":\"5m\",\"query.max-concurrency\":\"20\",\"query.max-samples\":\"50000000\",\"query.timeout\":\"2m\",\"rules.alert.for-grace-period\":\"10m\",\"rules.alert.for-outage-tolerance\":\"1h\",\"rules.alert.resend-delay\":\"1m\",\"scrape.adjust-timestamps\":\"true\",\"scrape.discovery-reload-interval\":\"5s\",\"scrape.timestamp-tolerance\":\"2ms\",\"storage.agent.no-lockfile\":\"false\",\"storage.agent.path\":\"data-agent/\",\"storage.agent.retention.max-time\":\"0s\",\"storage.agent.retention.min-time\":\"0s\",\"storage.agent.wal-compression\":\"true\",\"storage.agent.wal-segment-size\":\"0B\",\"storage.agent.wal-truncate-frequency\":\"0s\",\"storage.remote.flush-deadline\":\"1m\",\"storage.remote.read-concurrent-limit\":\"10\",\"storage.remote.read-max-bytes-in-frame\":\"1048576\",\"storage.remote.read-sample-limit\":\"50000000\",\"storage.tsdb.allow-overlapping-blocks\":\"false\",\"storage.tsdb.head-chunks-write-queue-size\":\"0\",\"storage.tsdb.max-block-chunk-segment-size\":\"0B\",\"storage.tsdb.max-block-duration\":\"2h\",\"storage.tsdb.min-block-duration\":\"2h\",\"storage.tsdb.no-lockfile\":\"false\",\"storage.tsdb.path\":\"" + filepath.Join(e.SharedDir(), "/data/prometheus-2/") + "\",\"storage.tsdb.retention\":\"0s\",\"storage.tsdb.retention.size\":\"0B\",\"storage.tsdb.retention.time\":\"0s\",\"storage.tsdb.wal-compression\":\"true\",\"storage.tsdb.wal-segment-size\":\"0B\",\"web.config.file\":\"\",\"web.console.libraries\":\"console_libraries\",\"web.console.templates\":\"consoles\",\"web.cors.origin\":\".*\",\"web.enable-admin-api\":\"false\",\"web.enable-lifecycle\":\"false\",\"web.enable-remote-write-receiver\":\"false\",\"web.external-url\":\"\",\"web.listen-address\":\":9090\",\"web.max-connections\":\"512\",\"web.page-title\":\"Prometheus Time Series Collection and Processing Server\",\"web.read-timeout\":\"5m\",\"web.route-prefix\":\"/\",\"web.user-assets\":\"\"}}"

	var out bytes.Buffer
	testutil.Ok(t, p1.Exec(wgetFlagsCmd("localhost:9090"), e2e.WithExecOptionStdout(&out)))
	testutil.Equals(t, expectedFlagsOutputProm1, out.String())

	resp, err := http.Get("http://" + p1.Endpoint("http") + "/api/v1/status/flags")
	testutil.Ok(t, err)
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	testutil.Ok(t, err)
	testutil.Equals(t, expectedFlagsOutputProm1, string(b))
	testutil.Equals(t, "", p1.Endpoint("not-existing"))

	// Now try the same but cross containers.
	out.Reset()
	testutil.Ok(t, p1.Exec(wgetFlagsCmd(p2.InternalEndpoint("http")), e2e.WithExecOptionStdout(&out)))
	testutil.Equals(t, expectedFlagsOutputProm2, out.String())

	testutil.NotOk(t, p1.Start()) // Starting ok, should fail.

	// Batch job example and test.
	batch := e.Runnable("batch").Init(e2e.StartOptions{Image: "ubuntu:20.04", Command: e2e.NewCommandRunUntilStop()})
	testutil.Ok(t, batch.Start())
	for i := 0; i < 3; i++ {
		out.Reset()
		testutil.Ok(t, batch.Exec(e2e.NewCommand("echo", "yolo"), e2e.WithExecOptionStdout(&out)))
		testutil.Equals(t, "yolo\n", out.String())
	}

	e.Close()
	afterClose := e2edb.NewPrometheus(e, "prometheus-3") // Should fail.
	testutil.NotOk(t, afterClose.Start())
}
