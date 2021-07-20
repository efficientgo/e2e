// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

package e2edb

import (
	"fmt"
	"strings"

	"github.com/efficientgo/tools/e2e"
)

const (
	MinioAccessKey = "Cheescake"
	MinioSecretKey = "supersecret"
)

type Builder struct {
	MemcachedImage, MinioImage, ConsulImage, EtcdImage, DynamoDBImage, BigtableEmulatorImage, CassandraImage, SwiftEmulatorImage                         string
	MemcachedHTTPPort, MinioHTTPPort, ConsulHTTPPort, EtcdHTTPPort, DynamoDBHTTPPort, BigtableEmulatorHTTPPort, CassandraHTTPPort, SwiftEmulatorHTTPPort int
}

// Default returns default builder. Create your own if you want to adjust the images or ports.
func Default() Builder {
	return Builder{
		MemcachedImage:    "memcached:1.6.1",
		MemcachedHTTPPort: 11211,

		MinioImage:    "minio/minio:RELEASE.2019-12-30T05-45-39Z",
		MinioHTTPPort: 8090,

		ConsulImage:    "consul:1.8.4",
		ConsulHTTPPort: 8500,

		EtcdImage:    "gcr.io/etcd-development/etcd:v3.4.7",
		EtcdHTTPPort: 2379,

		DynamoDBImage:    "amazon/dynamodb-local:1.11.477",
		DynamoDBHTTPPort: 8000,

		BigtableEmulatorImage:    "shopify/bigtable-emulator:0.1.0",
		BigtableEmulatorHTTPPort: 9035,

		CassandraImage:    "rinscy/cassandra:3.11.0",
		CassandraHTTPPort: 9042,

		SwiftEmulatorImage:    "bouncestorage/swift-aio:55ba4331",
		SwiftEmulatorHTTPPort: 8080,
	}
}

// NewMinio returns minio server, used as a local replacement for S3.
func (b Builder) NewMinio(bktName string) *e2e.HTTPService {
	minioKESGithubContent := "https://raw.githubusercontent.com/minio/kes/master"
	commands := []string{
		"curl -sSL --tlsv1.2 -O '%s/root.key'	-O '%s/root.cert'",
		"mkdir -p /data/%s && minio server --address :%v --quiet /data",
	}

	m := e2e.NewHTTPService(
		fmt.Sprintf("minio-%v", b.MinioHTTPPort),
		b.MinioImage,
		// Create the required bucket before starting minio.
		e2e.NewCommandWithoutEntrypoint("sh", "-c", fmt.Sprintf(strings.Join(commands, " && "), minioKESGithubContent, minioKESGithubContent, bktName, b.MinioHTTPPort)),
		e2e.NewHTTPReadinessProbe(b.MinioHTTPPort, "/minio/health/ready", 200, 200),
		b.MinioHTTPPort,
	)
	m.SetEnvVars(map[string]string{
		"MINIO_ACCESS_KEY": MinioAccessKey,
		"MINIO_SECRET_KEY": MinioSecretKey,
		"MINIO_BROWSER":    "off",
		"ENABLE_HTTPS":     "0",
		// https://docs.min.io/docs/minio-kms-quickstart-guide.html
		"MINIO_KMS_KES_ENDPOINT":  "https://play.min.io:7373",
		"MINIO_KMS_KES_KEY_FILE":  "root.key",
		"MINIO_KMS_KES_CERT_FILE": "root.cert",
		"MINIO_KMS_KES_KEY_NAME":  "my-minio-key",
	})
	return m
}

func (b Builder) NewConsul() *e2e.HTTPService {
	return e2e.NewHTTPService(
		"consul",
		b.ConsulImage,
		// Run consul in "dev" mode so that the initial leader election is immediate.
		e2e.NewCommand("agent", "-server", "-client=0.0.0.0", "-dev", "-log-level=err"),
		e2e.NewHTTPReadinessProbe(b.ConsulHTTPPort, "/v1/operator/autopilot/health", 200, 200, `"Healthy": true`),
		b.ConsulHTTPPort,
	)
}

func (b Builder) NewETCD() *e2e.HTTPService {
	return e2e.NewHTTPService(
		"etcd",
		b.EtcdImage,
		e2e.NewCommand("/usr/local/bin/etcd", "--listen-client-urls=http://0.0.0.0:2379", "--advertise-client-urls=http://0.0.0.0:2379", "--listen-metrics-urls=http://0.0.0.0:9000", "--log-level=error"),
		e2e.NewHTTPReadinessProbe(9000, "/health", 200, 204),
		b.EtcdHTTPPort,
		9000, // Metrics.
	)
}

func (b Builder) NewDynamoDB() *e2e.HTTPService {
	return e2e.NewHTTPService(
		"dynamodb",
		b.DynamoDBImage,
		e2e.NewCommand("-jar", "DynamoDBLocal.jar", "-inMemory", "-sharedDb"),
		// DynamoDB doesn't have a readiness probe, so we check if the / works even if returns 400
		e2e.NewHTTPReadinessProbe(b.DynamoDBHTTPPort, "/", 400, 400),
		b.DynamoDBHTTPPort,
	)
}

func (b Builder) NewBigtable() *e2e.HTTPService {
	return e2e.NewHTTPService(
		"bigtable",
		b.BigtableEmulatorImage,
		nil,
		nil,
		b.BigtableEmulatorHTTPPort,
	)
}

func (b Builder) NewCassandra() *e2e.HTTPService {
	return e2e.NewHTTPService(
		"cassandra",
		b.CassandraImage,
		nil,
		// Readiness probe inspired from https://github.com/kubernetes/examples/blob/b86c9d50be45eaf5ce74dee7159ce38b0e149d38/cassandra/image/files/ready-probe.sh
		e2e.NewCmdReadinessProbe(e2e.NewCommand("bash", "-c", "nodetool status | grep UN")),
		b.CassandraHTTPPort,
	)
}

func (b Builder) NewSwiftStorage() *e2e.HTTPService {
	return e2e.NewHTTPService(
		"swift",
		b.SwiftEmulatorImage,
		nil,
		e2e.NewHTTPReadinessProbe(b.SwiftEmulatorHTTPPort, "/", 404, 404),
		b.SwiftEmulatorHTTPPort,
	)
}

func (b Builder) NewMemcached() *e2e.ConcreteService {
	return e2e.NewConcreteService(
		"memcached",
		b.MemcachedImage,
		nil,
		e2e.NewTCPReadinessProbe(b.MemcachedHTTPPort),
		b.MemcachedHTTPPort,
	)
}
