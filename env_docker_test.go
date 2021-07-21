package e2e_test

import (
	"testing"

	"github.com/efficientgo/e2e"
	"github.com/efficientgo/tools/core/pkg/testutil"
)

func prometheusStartOpts(name string) e2e.StartOptions {
	return e2e.StartOptions{}
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
