package main

import (
	"context"
	"log"
	"syscall"

	"github.com/efficientgo/tools/core/pkg/testutil"
	"github.com/efficientgo/tools/e2e"
	e2edb "github.com/efficientgo/tools/e2e/db"
	"github.com/oklog/run"
)

// Demo made based on https://github.com/grafana/tns

func deployTNS(ctx context.Context) error {
	s, err := e2e.NewScenario(e2e.WithNetworkName(networkName))
	testutil.Ok(t, err)

	m1 := e2edb.Default().NewMinio(bktName)

	d := e2edb.Default()
	d.MinioHTTPPort = 9001
	m2 := d.NewMinio(bktName)

	closePlease := true
	defer func() {
		if closePlease {
			// You're welcome.
			s.Close()
		}
	}()
	testutil.Ok(t, s.StartAndWaitReady(m1, m2))
	testutil.NotOk(t, s.Start(m1))
	testutil.NotOk(t, s.Start(e2edb.Default().NewMinio(bktName)))

	closePlease = false
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
