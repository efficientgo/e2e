package main

import (
	"context"
	"log"
	"syscall"

	"github.com/efficientgo/tools/core/pkg/testutil"
	"github.com/efficientgo/e2e"
	"github.com/oklog/run"
)

func deployPrometheusAndWaitFor5Scrapes() {
	e2e.NewScenario()

	p := e2e.NewMonitoring()
	e2e.

}
func main() {
	g := &run.Group{}
	g.Add(run.SignalHandler(context.Background(), syscall.SIGINT, syscall.SIGTERM))

	{
		ctx, cancel := context.WithCancel(context.Background())
		g.Add(func() error { return deployTNS(ctx) }, func(error) { cancel() })
	}
	if err := g.Run(); err != nil {
		log.Fatal(err)
	}
}
