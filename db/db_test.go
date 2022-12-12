// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

package e2edb

import (
	"context"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/efficientgo/core/testutil"
	"github.com/efficientgo/e2e"
)

func TestMinio(t *testing.T) {
	t.Parallel()

	e, err := e2e.New()
	testutil.Ok(t, err)
	t.Cleanup(e.Close)

	minioContainer := NewMinio(e, "mintest", "bkt")
	testutil.Ok(t, e2e.StartAndWaitReady(minioContainer))

	endpoint := minioContainer.Endpoint("http")
	accessKeyID := MinioAccessKey
	secretAccessKey := MinioSecretKey
	minioClient, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKeyID, secretAccessKey, ""),
		Secure: false,
	})
	testutil.Ok(t, err)

	testutil.Ok(
		t,
		minioClient.MakeBucket(context.Background(), "test-bucket", minio.MakeBucketOptions{}),
	)
}
