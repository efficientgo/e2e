// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

// Package e2edb is a reference, instrumented runnables that are running various popular databases one could run in their
// tests or benchmarks.
package e2edb

import (
	"fmt"
	"strings"

	"github.com/efficientgo/e2e"
)

const (
	MinioAccessKey = "Cheescake"
	MinioSecretKey = "supersecret"
)

type Option func(*options)

type options struct {
	image string
}

func WithImage(image string) Option {
	return func(o *options) {
		o.image = image
	}
}

const AccessPortName = "http"

// NewMinio returns minio server, used as a local replacement for S3.
func NewMinio(env e2e.Environment, name, bktName string, opts ...Option) *e2e.InstrumentedRunnable {
	o := options{image: "minio/minio:RELEASE.2019-12-30T05-45-39Z"}
	for _, opt := range opts {
		opt(&o)
	}

	minioKESGithubContent := "https://raw.githubusercontent.com/minio/kes/master"
	commands := []string{
		"curl -sSL --tlsv1.2 -O '%s/root.key'	-O '%s/root.cert'",
		"mkdir -p /data/%s && minio server --address :%v --quiet /data",
	}

	return e2e.NewInstrumentedRunnable(
		env,
		name,
		map[string]int{AccessPortName: 8090},
		AccessPortName,
		e2e.StartOptions{
			Image: o.image,
			// Create the required bucket before starting minio.
			Command:   e2e.NewCommandWithoutEntrypoint("sh", "-c", fmt.Sprintf(strings.Join(commands, " && "), minioKESGithubContent, minioKESGithubContent, bktName, 8090)),
			Readiness: e2e.NewHTTPReadinessProbe(AccessPortName, "/minio/health/ready", 200, 200),
			EnvVars: map[string]string{
				"MINIO_ACCESS_KEY": MinioAccessKey,
				"MINIO_SECRET_KEY": MinioSecretKey,
				"MINIO_BROWSER":    "off",
				"ENABLE_HTTPS":     "0",
				// https://docs.min.io/docs/minio-kms-quickstart-guide.html
				"MINIO_KMS_KES_ENDPOINT":  "https://play.min.io:7373",
				"MINIO_KMS_KES_KEY_FILE":  "root.key",
				"MINIO_KMS_KES_CERT_FILE": "root.cert",
				"MINIO_KMS_KES_KEY_NAME":  "my-minio-key",
			},
		},
	)
}

func NewConsul(env e2e.Environment, name string, opts ...Option) *e2e.InstrumentedRunnable {
	o := options{image: "consul:1.8.4"}
	for _, opt := range opts {
		opt(&o)
	}

	return e2e.NewInstrumentedRunnable(
		env,
		name,
		map[string]int{AccessPortName: 8500},
		AccessPortName,
		e2e.StartOptions{
			Image: o.image,
			// Run consul in "dev" mode so that the initial leader election is immediate.
			Command:   e2e.NewCommand("agent", "-server", "-client=0.0.0.0", "-dev", "-log-level=err"),
			Readiness: e2e.NewHTTPReadinessProbe(AccessPortName, "/v1/operator/autopilot/health", 200, 200, `"Healthy": true`),
		},
	)
}

func NewDynamoDB(env e2e.Environment, name string, opts ...Option) *e2e.InstrumentedRunnable {
	o := options{image: "amazon/dynamodb-local:1.11.477"}
	for _, opt := range opts {
		opt(&o)
	}

	return e2e.NewInstrumentedRunnable(
		env,
		name,
		map[string]int{AccessPortName: 8000},
		AccessPortName,
		e2e.StartOptions{
			Image:   o.image,
			Command: e2e.NewCommand("-jar", "DynamoDBLocal.jar", "-inMemory", "-sharedDb"),
			// DynamoDB doesn't have a readiness probe, so we check if the / works even if returns 400
			Readiness: e2e.NewHTTPReadinessProbe("http", "/", 400, 400),
		},
	)
}

func NewBigtable(env e2e.Environment, name string, opts ...Option) *e2e.InstrumentedRunnable {
	o := options{image: "shopify/bigtable-emulator:0.1.0"}
	for _, opt := range opts {
		opt(&o)
	}

	return e2e.NewInstrumentedRunnable(
		env,
		name,
		nil,
		AccessPortName,
		e2e.StartOptions{
			Image: o.image,
		},
	)
}

func NewCassandra(env e2e.Environment, name string, opts ...Option) *e2e.InstrumentedRunnable {
	o := options{image: "rinscy/cassandra:3.11.0"}
	for _, opt := range opts {
		opt(&o)
	}

	return e2e.NewInstrumentedRunnable(
		env,
		name,
		map[string]int{AccessPortName: 9042},
		AccessPortName,
		e2e.StartOptions{
			Image: o.image,
			// Readiness probe inspired from https://github.com/kubernetes/examples/blob/b86c9d50be45eaf5ce74dee7159ce38b0e149d38/cassandra/image/files/ready-probe.sh
			Readiness: e2e.NewCmdReadinessProbe(e2e.NewCommand("bash", "-c", "nodetool status | grep UN")),
		},
	)
}

func NewSwiftStorage(env e2e.Environment, name string, opts ...Option) *e2e.InstrumentedRunnable {
	o := options{image: "bouncestorage/swift-aio:55ba4331"}
	for _, opt := range opts {
		opt(&o)
	}

	return e2e.NewInstrumentedRunnable(
		env,
		name,
		map[string]int{AccessPortName: 8080},
		AccessPortName,
		e2e.StartOptions{
			Image:     o.image,
			Readiness: e2e.NewHTTPReadinessProbe(AccessPortName, "/", 404, 404),
		},
	)
}

func NewMemcached(env e2e.Environment, name string, opts ...Option) e2e.Runnable {
	o := options{image: "memcached:1.6.1"}
	for _, opt := range opts {
		opt(&o)
	}

	return env.Runnable(
		name,
		map[string]int{AccessPortName: 11211},
		e2e.StartOptions{
			Image:     o.image,
			Readiness: e2e.NewTCPReadinessProbe(AccessPortName),
		},
	)
}

func NewETCD(env e2e.Environment, name string, opts ...Option) *e2e.InstrumentedRunnable {
	o := options{image: "gcr.io/etcd-development/etcd:v3.4.7"}
	for _, opt := range opts {
		opt(&o)
	}

	return e2e.NewInstrumentedRunnable(
		env,
		name,
		map[string]int{
			AccessPortName: 2379,
			"metrics":      9000,
		},
		"metrics",
		e2e.StartOptions{
			Image:     o.image,
			Command:   e2e.NewCommand("/usr/local/bin/etcd", "--listen-client-urls=http://0.0.0.0:2379", "--advertise-client-urls=http://0.0.0.0:2379", "--listen-metrics-urls=http://0.0.0.0:9000", "--log-level=error"),
			Readiness: e2e.NewHTTPReadinessProbe("metrics", "/health", 200, 204),
		},
	)
}
