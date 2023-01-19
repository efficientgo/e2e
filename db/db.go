// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

// Package e2edb is a reference, instrumented runnables that are running various popular databases one could run in their
// tests or benchmarks.
package e2edb

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/efficientgo/core/errors"
	"github.com/efficientgo/e2e"
	e2emon "github.com/efficientgo/e2e/monitoring"
	e2eprof "github.com/efficientgo/e2e/profiling"
)

const (
	MinioAccessKey = "Cheescake"
	MinioSecretKey = "supersecret"
)

type Option func(*options)

type options struct {
	image        string
	flagOverride map[string]string
	minioOptions minioOptions
}

type minioOptions struct {
	enableSSE bool
	enableTLS bool
}

func WithImage(image string) Option {
	return func(o *options) {
		o.image = image
	}
}

func WithFlagOverride(ov map[string]string) Option {
	return func(o *options) {
		o.flagOverride = ov
	}
}

func WithMinioSSE() Option {
	return func(o *options) {
		o.minioOptions.enableSSE = true
	}
}

func WithMinioTLS() Option {
	return func(o *options) {
		o.minioOptions.enableTLS = true
	}
}

const AccessPortName = "http"

func NewPrometheus(env e2e.Environment, name string, opts ...Option) *e2emon.Prometheus {
	o := options{}
	for _, opt := range opts {
		opt(&o)
	}

	return e2emon.NewPrometheus(env, name, o.image, o.flagOverride)
}

func NewParca(env e2e.Environment, name string, opts ...Option) *e2eprof.Parca {
	o := options{}
	for _, opt := range opts {
		opt(&o)
	}

	return e2eprof.NewParca(env, name, o.image, o.flagOverride)
}

// NewMinio returns minio server, used as a local replacement for S3.
func NewMinio(env e2e.Environment, name, bktName string, opts ...Option) *e2emon.InstrumentedRunnable {
	o := options{image: "minio/minio:RELEASE.2022-03-14T18-25-24Z"}
	for _, opt := range opts {
		opt(&o)
	}

	userID := strconv.Itoa(os.Getuid())
	ports := map[string]int{AccessPortName: 8090}
	envVars := []string{
		"MINIO_ROOT_USER=" + MinioAccessKey,
		"MINIO_ROOT_PASSWORD=" + MinioSecretKey,
		"MINIO_BROWSER=" + "off",
	}

	f := env.Runnable(name).WithPorts(ports).Future()

	// Hacky: Create user that matches ID with host ID to be able to remove .minio.sys details on the start.
	// Proper solution would be to contribute/create our own minio image which is non root.
	command := fmt.Sprintf("useradd -G root -u %v me && mkdir -p %s && chown -R me %s &&", userID, f.Dir(), f.Dir())

	if o.minioOptions.enableSSE {
		envVars = append(envVars, []string{
			// https://docs.min.io/docs/minio-kms-quickstart-guide.html
			"MINIO_KMS_KES_ENDPOINT=" + "https://play.min.io:7373",
			"MINIO_KMS_KES_KEY_FILE=" + "root.key",
			"MINIO_KMS_KES_CERT_FILE=" + "root.cert",
			"MINIO_KMS_KES_KEY_NAME=" + "my-minio-key",
		}...)
		command += "curl -sSL --tlsv1.3 -O 'https://raw.githubusercontent.com/minio/kes/master/root.key' -O 'https://raw.githubusercontent.com/minio/kes/master/root.cert' && cp root.* /home/me/ && "
	}

	var readiness e2e.ReadinessProbe

	if o.minioOptions.enableTLS {
		if err := os.MkdirAll(filepath.Join(f.Dir(), "certs", "CAs"), 0750); err != nil {
			return &e2emon.InstrumentedRunnable{Runnable: e2e.NewFailedRunnable(name, errors.Wrap(err, "create certs dir"))}
		}

		if err := genCerts(
			filepath.Join(f.Dir(), "certs", "public.crt"),
			filepath.Join(f.Dir(), "certs", "private.key"),
			filepath.Join(f.Dir(), "certs", "CAs", "ca.crt"),
			fmt.Sprintf("%s-%s", env.Name(), name),
		); err != nil {
			return &e2emon.InstrumentedRunnable{Runnable: e2e.NewFailedRunnable(name, errors.Wrap(err, "fail to generate certs"))}
		}

		envVars = append(envVars, "ENABLE_HTTPS="+"1")
		command = command + fmt.Sprintf(
			"su - me -s /bin/sh -c 'mkdir -p %s && %s /opt/bin/minio server --certs-dir %s/certs --address :%v --quiet %v'",
			filepath.Join(f.Dir(), bktName),
			strings.Join(envVars, " "),
			f.Dir(),
			ports[AccessPortName],
			f.Dir(),
		)

		readiness = e2e.NewHTTPSReadinessProbe(
			AccessPortName,
			"/minio/health/cluster",
			200,
			200,
		)
	} else {
		envVars = append(envVars, "ENABLE_HTTPS="+"0")
		command = command + fmt.Sprintf(
			"su - me -s /bin/sh -c 'mkdir -p %s && %s /opt/bin/minio server --address :%v --quiet %v'",
			filepath.Join(f.Dir(), bktName),
			strings.Join(envVars, " "),
			ports[AccessPortName],
			f.Dir(),
		)

		readiness = e2e.NewHTTPReadinessProbe(
			AccessPortName,
			"/minio/health/cluster",
			200,
			200,
		)
	}

	return e2emon.AsInstrumented(f.Init(
		e2e.StartOptions{
			Image: o.image,
			// Create the required bucket before starting minio.
			Command: e2e.NewCommandWithoutEntrypoint(
				"sh",
				"-c",
				command,
			),
			Readiness: readiness,
		},
	), AccessPortName)
}

// genCerts generates certificates and writes those to the provided paths.
func genCerts(certPath, privkeyPath, caPath, serverName string) error {
	var caRoot = &x509.Certificate{
		SerialNumber:          big.NewInt(2019),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		IsCA:                  true,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}

	var cert = &x509.Certificate{
		SerialNumber: big.NewInt(1658),
		DNSNames:     []string{serverName},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
		NotAfter:     time.Now().AddDate(10, 0, 0),
		SubjectKeyId: []byte{1, 2, 3},
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}

	caPrivKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}

	certPrivKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	// Generate CA cert.
	caBytes, err := x509.CreateCertificate(rand.Reader, caRoot, caRoot, &caPrivKey.PublicKey, caPrivKey)
	if err != nil {
		return err
	}
	caPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: caBytes,
	})
	err = os.WriteFile(caPath, caPEM, 0644)
	if err != nil {
		return err
	}

	// Sign the cert with the CA private key.
	certBytes, err := x509.CreateCertificate(rand.Reader, cert, caRoot, &certPrivKey.PublicKey, caPrivKey)
	if err != nil {
		return err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certBytes,
	})
	err = os.WriteFile(certPath, certPEM, 0644)
	if err != nil {
		return err
	}

	certPrivKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(certPrivKey),
	})
	err = os.WriteFile(privkeyPath, certPrivKeyPEM, 0644)
	if err != nil {
		return err
	}

	return nil
}

func NewConsul(env e2e.Environment, name string, opts ...Option) *e2emon.InstrumentedRunnable {
	o := options{image: "consul:1.8.4"}
	for _, opt := range opts {
		opt(&o)
	}

	e2e.MergeFlags()
	return e2emon.AsInstrumented(env.Runnable(name).WithPorts(map[string]int{AccessPortName: 8500}).Init(
		e2e.StartOptions{
			Image: o.image,
			// Run consul in "dev" mode so that the initial leader election is immediate.
			Command:   e2e.NewCommand("agent", "-server", "-client=0.0.0.0", "-dev", "-log-level=err"),
			Readiness: e2e.NewHTTPReadinessProbe(AccessPortName, "/v1/operator/autopilot/health", 200, 200, `"Healthy": true`),
		},
	), AccessPortName)
}

func NewDynamoDB(env e2e.Environment, name string, opts ...Option) *e2emon.InstrumentedRunnable {
	o := options{image: "amazon/dynamodb-local:1.11.477"}
	for _, opt := range opts {
		opt(&o)
	}

	return e2emon.AsInstrumented(env.Runnable(name).WithPorts(map[string]int{AccessPortName: 8000}).Init(
		e2e.StartOptions{
			Image:   o.image,
			Command: e2e.NewCommand("-jar", "DynamoDBLocal.jar", "-inMemory", "-sharedDb"),
			// DynamoDB doesn't have a readiness probe, so we check if the / works even if returns 400
			Readiness: e2e.NewHTTPReadinessProbe("http", "/", 400, 400),
		},
	), AccessPortName)
}

func NewBigtable(env e2e.Environment, name string, opts ...Option) e2e.Runnable {
	o := options{image: "shopify/bigtable-emulator:0.1.0"}
	for _, opt := range opts {
		opt(&o)
	}

	return env.Runnable(name).Init(
		e2e.StartOptions{
			Image: o.image,
		},
	)
}

func NewCassandra(env e2e.Environment, name string, opts ...Option) *e2emon.InstrumentedRunnable {
	o := options{image: "rinscy/cassandra:3.11.0"}
	for _, opt := range opts {
		opt(&o)
	}

	return e2emon.AsInstrumented(env.Runnable(name).WithPorts(map[string]int{AccessPortName: 9042}).Init(
		e2e.StartOptions{
			Image: o.image,
			// Readiness probe inspired from https://github.com/kubernetes/examples/blob/b86c9d50be45eaf5ce74dee7159ce38b0e149d38/cassandra/image/files/ready-probe.sh
			Readiness: e2e.NewCmdReadinessProbe(e2e.NewCommand("bash", "-c", "nodetool status | grep UN")),
		},
	), AccessPortName)
}

func NewSwiftStorage(env e2e.Environment, name string, opts ...Option) *e2emon.InstrumentedRunnable {
	o := options{image: "bouncestorage/swift-aio:55ba4331"}
	for _, opt := range opts {
		opt(&o)
	}

	return e2emon.AsInstrumented(env.Runnable(name).WithPorts(map[string]int{AccessPortName: 8080}).Init(
		e2e.StartOptions{
			Image:     o.image,
			Readiness: e2e.NewHTTPReadinessProbe(AccessPortName, "/", 404, 404),
		},
	), AccessPortName)
}

func NewMemcached(env e2e.Environment, name string, opts ...Option) e2e.Runnable {
	o := options{image: "memcached:1.6.1"}
	for _, opt := range opts {
		opt(&o)
	}

	return env.Runnable(name).WithPorts(map[string]int{AccessPortName: 11211}).Init(
		e2e.StartOptions{
			Image:     o.image,
			Readiness: e2e.NewTCPReadinessProbe(AccessPortName),
		},
	)
}

func NewETCD(env e2e.Environment, name string, opts ...Option) *e2emon.InstrumentedRunnable {
	o := options{image: "gcr.io/etcd-development/etcd:v3.4.7"}
	for _, opt := range opts {
		opt(&o)
	}

	return e2emon.AsInstrumented(env.Runnable(name).WithPorts(map[string]int{AccessPortName: 2379, "metrics": 9000}).Init(
		e2e.StartOptions{
			Image: o.image,
			Command: e2e.NewCommand(
				"/usr/local/bin/etcd",
				"--listen-client-urls=http://0.0.0.0:2379",
				"--advertise-client-urls=http://0.0.0.0:2379",
				"--listen-metrics-urls=http://0.0.0.0:9000",
				"--log-level=error",
			),
			Readiness: e2e.NewHTTPReadinessProbe("metrics", "/health", 200, 204),
		},
	), "metrics")
}
