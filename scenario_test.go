// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

package e2e_test

import (
	"bytes"
	"context"
	"io/ioutil"
	"testing"
	"time"

	"github.com/efficientgo/tools/core/pkg/testutil"
	"github.com/efficientgo/tools/e2e"
	e2edb "github.com/efficientgo/tools/e2e/db"
	"github.com/efficientgo/tools/e2e/internal/s3"
	"github.com/go-kit/kit/log"
	"gopkg.in/yaml.v3"
)

const bktName = "cheesecake"

func spinup(t *testing.T, networkName string) (*e2e.Scenario, *e2e.HTTPService, *e2e.HTTPService) {
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
	return s, m1, m2
}

// TODO(bwplotka): Get rid of minio example and test scenario with just raw server and some HTTP, no need to minio client deps.
func testMinioWorking(t *testing.T, m *e2e.HTTPService) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	b, err := yaml.Marshal(s3.Config{
		Endpoint:  m.HTTPEndpoint(),
		Bucket:    bktName,
		AccessKey: e2edb.MinioAccessKey,
		SecretKey: e2edb.MinioSecretKey,
		Insecure:  true, // WARNING: Our secret cheesecake recipes might leak.
	})
	testutil.Ok(t, err)

	bkt, err := s3.NewBucket(log.NewNopLogger(), b, "test")
	testutil.Ok(t, err)

	testutil.Ok(t, bkt.Upload(ctx, "recipe", bytes.NewReader([]byte("Just go to Pastry Shop and buy."))))
	testutil.Ok(t, bkt.Upload(ctx, "mom/recipe", bytes.NewReader([]byte("https://www.bbcgoodfood.com/recipes/strawberry-cheesecake-4-easy-steps"))))

	r, err := bkt.Get(ctx, "recipe")
	testutil.Ok(t, err)
	b, err = ioutil.ReadAll(r)
	testutil.Ok(t, err)
	testutil.Equals(t, "Just go to Pastry Shop and buy.", string(b))

	r, err = bkt.Get(ctx, "mom/recipe")
	testutil.Ok(t, err)
	b, err = ioutil.ReadAll(r)
	testutil.Ok(t, err)
	testutil.Equals(t, "https://www.bbcgoodfood.com/recipes/strawberry-cheesecake-4-easy-steps", string(b))
}

func TestScenario(t *testing.T) {
	t.Parallel()

	s, m1, m2 := spinup(t, "e2e-scenario-test")
	defer s.Close()

	t.Run("minio is working", func(t *testing.T) {
		testMinioWorking(t, m1)
		testMinioWorking(t, m2)
	})

	t.Run("concurrent nested scenario 1 is working just fine as well", func(t *testing.T) {
		t.Parallel()

		s, m1, m2 := spinup(t, "e2e-scenario-test1")
		defer s.Close()

		testMinioWorking(t, m1)
		testMinioWorking(t, m2)
	})
	t.Run("concurrent nested scenario 2 is working just fine as well", func(t *testing.T) {
		t.Parallel()

		s, m1, m2 := spinup(t, "e2e-scenario-test2")
		defer s.Close()

		testMinioWorking(t, m1)
		testMinioWorking(t, m2)
	})

	testutil.Ok(t, s.Stop(m1))

	// Expect m1 not working.
	b, err := yaml.Marshal(s3.Config{
		Endpoint:  m1.Name(),
		Bucket:    "cheescake",
		AccessKey: e2edb.MinioAccessKey,
		SecretKey: e2edb.MinioSecretKey,
	})
	testutil.Ok(t, err)
	bkt, err := s3.NewBucket(log.NewNopLogger(), b, "test")
	testutil.Ok(t, err)

	_, err = bkt.Get(context.Background(), "recipe")
	testutil.NotOk(t, err)

	testMinioWorking(t, m2)

	testutil.NotOk(t, s.Stop(m1))
	// Should be noop.
	testutil.Ok(t, m1.Stop())
	// I can run closes as many times I want.
	s.Close()
	s.Close()
	s.Close()

	// Expect m2 not working.
	b, err = yaml.Marshal(s3.Config{
		Endpoint:  m2.Name(),
		Bucket:    "cheescake",
		AccessKey: e2edb.MinioAccessKey,
		SecretKey: e2edb.MinioSecretKey,
	})
	testutil.Ok(t, err)
	bkt, err = s3.NewBucket(log.NewNopLogger(), b, "test")
	testutil.Ok(t, err)

	_, err = bkt.Get(context.Background(), "recipe")
	testutil.NotOk(t, err)
}
