// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

package e2e_test

import (
	"io/ioutil"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/efficientgo/e2e"
	e2edb "github.com/efficientgo/e2e/db"
	"github.com/efficientgo/tools/core/pkg/testutil"
)

func wgetFlagsCmd(hostPort string) e2e.Command {
	return e2e.NewCommandWithoutEntrypoint("/bin/sh", "-c", "wget http://"+hostPort+"/api/v1/status/flags -O /tmp/flags && cat /tmp/flags")
}

func TestDockerEnvironment(t *testing.T) {
	t.Parallel()

	e, err := e2e.NewDockerEnvironment("e2e_lifecycle")
	testutil.Ok(t, err)
	t.Cleanup(e.Close)

	p1, err := e2edb.NewPrometheus(e, "prometheus-1")
	testutil.Ok(t, err)

	testutil.Equals(t, "prometheus-1", p1.Name())
	testutil.Equals(t, filepath.Join(e.SharedDir(), "data", p1.Name()), p1.Dir())
	testutil.Equals(t, filepath.Join("/shared", "data", p1.Name()), p1.InternalDir())
	testutil.Equals(t, "e2e_lifecycle-prometheus-1:9090", p1.InternalEndpoint("http"))
	testutil.Equals(t, "", p1.InternalEndpoint("not-existing"))
	testutil.Assert(t, !p1.IsRunning())

	// Errors as p1 was not started yet.
	testutil.NotOk(t, p1.WaitReady())
	testutil.Ok(t, p1.Stop())
	testutil.Ok(t, p1.Kill())

	_, _, err = p1.Exec(wgetFlagsCmd("localhost:9090"))
	testutil.NotOk(t, err)
	testutil.Equals(t, "stopped", p1.Endpoint("http"))
	testutil.Equals(t, "stopped", p1.Endpoint("not-existing"))

	p2, err := e2edb.NewPrometheus(e, "prometheus-2")
	testutil.Ok(t, err)

	testutil.Ok(t, e2e.StartAndWaitReady(p1, p2))
	testutil.Ok(t, p1.WaitReady())
	testutil.Ok(t, p1.WaitReady())

	testutil.Ok(t, p1.WaitSumMetrics(e2e.Greater(50), "prometheus_tsdb_head_samples_appended_total"))

	testutil.Equals(t, "prometheus-1", p1.Name())
	testutil.Equals(t, filepath.Join(e.SharedDir(), "data", p1.Name()), p1.Dir())
	testutil.Equals(t, filepath.Join("/shared", "data", p1.Name()), p1.InternalDir())
	testutil.Equals(t, "e2e_lifecycle-prometheus-1:9090", p1.InternalEndpoint("http"))
	testutil.Equals(t, "", p1.InternalEndpoint("not-existing"))
	testutil.Assert(t, p1.IsRunning())

	const (
		expectedFlagsOutputProm1 = "{\"status\":\"success\",\"data\":{\"alertmanager.notification-queue-capacity\":\"10000\",\"alertmanager.timeout\":\"\",\"config.file\":\"/shared/data/prometheus-1/prometheus.yml\",\"enable-feature\":\"\",\"log.format\":\"logfmt\",\"log.level\":\"info\",\"query.lookback-delta\":\"5m\",\"query.max-concurrency\":\"20\",\"query.max-samples\":\"50000000\",\"query.timeout\":\"2m\",\"rules.alert.for-grace-period\":\"10m\",\"rules.alert.for-outage-tolerance\":\"1h\",\"rules.alert.resend-delay\":\"1m\",\"scrape.adjust-timestamps\":\"true\",\"storage.exemplars.exemplars-limit\":\"0\",\"storage.remote.flush-deadline\":\"1m\",\"storage.remote.read-concurrent-limit\":\"10\",\"storage.remote.read-max-bytes-in-frame\":\"1048576\",\"storage.remote.read-sample-limit\":\"50000000\",\"storage.tsdb.allow-overlapping-blocks\":\"false\",\"storage.tsdb.max-block-chunk-segment-size\":\"0B\",\"storage.tsdb.max-block-duration\":\"2h\",\"storage.tsdb.min-block-duration\":\"2h\",\"storage.tsdb.no-lockfile\":\"false\",\"storage.tsdb.path\":\"/shared/data/prometheus-1\",\"storage.tsdb.retention\":\"0s\",\"storage.tsdb.retention.size\":\"0B\",\"storage.tsdb.retention.time\":\"0s\",\"storage.tsdb.wal-compression\":\"true\",\"storage.tsdb.wal-segment-size\":\"0B\",\"web.config.file\":\"\",\"web.console.libraries\":\"console_libraries\",\"web.console.templates\":\"consoles\",\"web.cors.origin\":\".*\",\"web.enable-admin-api\":\"false\",\"web.enable-lifecycle\":\"false\",\"web.external-url\":\"\",\"web.listen-address\":\":9090\",\"web.max-connections\":\"512\",\"web.page-title\":\"Prometheus Time Series Collection and Processing Server\",\"web.read-timeout\":\"5m\",\"web.route-prefix\":\"/\",\"web.user-assets\":\"\"}}"
		expectedFlagsOutputProm2 = "{\"status\":\"success\",\"data\":{\"alertmanager.notification-queue-capacity\":\"10000\",\"alertmanager.timeout\":\"\",\"config.file\":\"/shared/data/prometheus-2/prometheus.yml\",\"enable-feature\":\"\",\"log.format\":\"logfmt\",\"log.level\":\"info\",\"query.lookback-delta\":\"5m\",\"query.max-concurrency\":\"20\",\"query.max-samples\":\"50000000\",\"query.timeout\":\"2m\",\"rules.alert.for-grace-period\":\"10m\",\"rules.alert.for-outage-tolerance\":\"1h\",\"rules.alert.resend-delay\":\"1m\",\"scrape.adjust-timestamps\":\"true\",\"storage.exemplars.exemplars-limit\":\"0\",\"storage.remote.flush-deadline\":\"1m\",\"storage.remote.read-concurrent-limit\":\"10\",\"storage.remote.read-max-bytes-in-frame\":\"1048576\",\"storage.remote.read-sample-limit\":\"50000000\",\"storage.tsdb.allow-overlapping-blocks\":\"false\",\"storage.tsdb.max-block-chunk-segment-size\":\"0B\",\"storage.tsdb.max-block-duration\":\"2h\",\"storage.tsdb.min-block-duration\":\"2h\",\"storage.tsdb.no-lockfile\":\"false\",\"storage.tsdb.path\":\"/shared/data/prometheus-2\",\"storage.tsdb.retention\":\"0s\",\"storage.tsdb.retention.size\":\"0B\",\"storage.tsdb.retention.time\":\"0s\",\"storage.tsdb.wal-compression\":\"true\",\"storage.tsdb.wal-segment-size\":\"0B\",\"web.config.file\":\"\",\"web.console.libraries\":\"console_libraries\",\"web.console.templates\":\"consoles\",\"web.cors.origin\":\".*\",\"web.enable-admin-api\":\"false\",\"web.enable-lifecycle\":\"false\",\"web.external-url\":\"\",\"web.listen-address\":\":9090\",\"web.max-connections\":\"512\",\"web.page-title\":\"Prometheus Time Series Collection and Processing Server\",\"web.read-timeout\":\"5m\",\"web.route-prefix\":\"/\",\"web.user-assets\":\"\"}}"
	)
	out, errout, err := p1.Exec(wgetFlagsCmd("localhost:9090"))
	testutil.Ok(t, err, errout)
	testutil.Equals(t, expectedFlagsOutputProm1, out)

	resp, err := http.Get("http://" + p1.Endpoint("http") + "/api/v1/status/flags")
	testutil.Ok(t, err)
	defer resp.Body.Close()

	b, err := ioutil.ReadAll(resp.Body)
	testutil.Ok(t, err)
	testutil.Equals(t, expectedFlagsOutputProm1, string(b))
	testutil.Equals(t, "", p1.Endpoint("not-existing"))

	// Now try the same but cross containers.
	out, errout, err = p1.Exec(wgetFlagsCmd(p2.InternalEndpoint("http")))
	testutil.Ok(t, err, errout)
	testutil.Equals(t, expectedFlagsOutputProm2, out)

	testutil.NotOk(t, p1.Start()) // Starting ok, should fail.

	e.Close()
	_, err = e2edb.NewPrometheus(e, "prometheus-3") // Should fail.
	testutil.NotOk(t, err)
}
