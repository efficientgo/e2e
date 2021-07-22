package e2e_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/efficientgo/e2e"
	"github.com/efficientgo/tools/core/pkg/testutil"
	"github.com/pkg/errors"
)

func newPrometheus(env e2e.Environment, name string) e2e.Runnable {
	dir := filepath.Join(sharedDir, PrometheusRelLocalDir(name))
	container := filepath.Join(e2e.ContainerSharedDir, PrometheusRelLocalDir(name))
	if err := os.MkdirAll(dir, 0750); err != nil {
		return nil, "", errors.Wrap(err, "create prometheus dir")
	}

	if err := ioutil.WriteFile(filepath.Join(dir, "prometheus.yml"), []byte(config), 0600); err != nil {
		return nil, "", errors.Wrap(err, "creating prom config failed")
	}

	args := e2e.BuildArgs(map[string]string{
		"--config.file":                     filepath.Join(container, "prometheus.yml"),
		"--storage.tsdb.path":               container,
		"--storage.tsdb.max-block-duration": "2h",
		"--log.level":                       infoLogLevel,
		"--web.listen-address":              ":9090",
	})

	if len(enableFeatures) > 0 {
		args = append(args, fmt.Sprintf("--enable-feature=%s", strings.Join(enableFeatures, ",")))
	}
	prom := e2e.NewHTTPService(
		fmt.Sprintf("prometheus-%s", name),
		promImage,
		e2e.NewCommandWithoutEntrypoint("prometheus", args...),
		e2e.NewHTTPReadinessProbe(9090, "/-/ready", 200, 200),
		9090,
	)
	prom.SetUser(strconv.Itoa(os.Getuid()))
	prom.SetBackoff(defaultBackoffConfig)

	return prom, container, nil

	return env.Runnable(e2e.StartOptions{
		Name: name,
		Readiness:
		NetworkPorts: map[string]int{"http": 9090},
	})
}

func TestDockerEnvLifecycle(t *testing.T) {
	e, err := e2e.NewDockerEnvironment()
	testutil.Ok(t, err)

	var closed bool
	t.Cleanup(func() {
		if !closed {
			e.Close()
		}
	})

}
