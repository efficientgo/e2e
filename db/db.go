// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

// Package e2edb is a reference, instrumented runnables that are running various popular databases one could run in their
// tests or benchmarks.
package e2edb

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/efficientgo/core/errors"
	"github.com/efficientgo/e2e"
	e2emon "github.com/efficientgo/e2e/monitoring"
	e2eprof "github.com/efficientgo/e2e/profiling"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

const (
	S3AccessKey = "Cheescake"
	S3SecretKey = "supersecret"
)

type Option func(*options)

type options struct {
	image          string
	flagOverride   map[string]string
	minioOptions   minioOptions
	seaweedOptions seaweedFSOptions
	azuriteOptions azuriteOptions
}

type azuriteOptions struct {
	enableTLS             bool
	customStorageAccounts string
	connectionString      string
}

type minioOptions struct {
	enableSSE bool
	enableTLS bool
}

type seaweedFSOptions struct {
	enableTLS     bool
	enableIceberg bool
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

func WithSeaweedFSTLS() Option {
	return func(o *options) {
		o.seaweedOptions.enableTLS = true
	}
}

func WithSeaweedFSIceberg() Option {
	return func(o *options) {
		o.seaweedOptions.enableIceberg = true
	}
}

func WithAzuriteStorageAccounts(acc string) Option {
	return func(o *options) {
		o.azuriteOptions.customStorageAccounts = acc
	}
}

func WithAzuriteConnectionStringOverride(s string) Option {
	return func(o *options) {
		o.azuriteOptions.connectionString = s
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
		"MINIO_ROOT_USER=" + S3AccessKey,
		"MINIO_ROOT_PASSWORD=" + S3SecretKey,
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

// NewAzuriteBlobStorage returns azurite server, used as a local replacement for Azure Blob Storage.
func NewAzuriteBlobStorage(env e2e.Environment, name string, opts ...Option) *e2emon.InstrumentedRunnable {
	o := options{image: "mcr.microsoft.com/azure-storage/azurite:latest"}
	for _, opt := range opts {
		opt(&o)
	}

	ports := map[string]int{"blob": 10000}

	f := env.Runnable(name).WithPorts(ports).Future()

	envVars := []string{}
	if o.azuriteOptions.customStorageAccounts != "" {
		envVars = append(envVars, "AZURITE_ACCOUNTS="+o.azuriteOptions.customStorageAccounts)
	}

	command := fmt.Sprintf(
		"'%s azurite-blob --blobHost 0.0.0.0 --blobPort %v -l %v",
		strings.Join(envVars, " "),
		ports["blob"],
		f.Dir(),
	)

	if o.azuriteOptions.enableTLS {
		if err := os.MkdirAll(filepath.Join(f.Dir(), "certs", "CAs"), 0750); err != nil {
			return &e2emon.InstrumentedRunnable{Runnable: e2e.NewFailedRunnable(name, errors.Wrap(err, "create certs dir"))}
		}

		if err := genCerts(
			filepath.Join(f.Dir(), "certs", "cert.pem"),
			filepath.Join(f.Dir(), "certs", "key.pem"),
			filepath.Join(f.Dir(), "certs", "CAs", "ca.crt"),
			fmt.Sprintf("%s-%s", env.Name(), name),
		); err != nil {
			return &e2emon.InstrumentedRunnable{Runnable: e2e.NewFailedRunnable(name, errors.Wrap(err, "fail to generate certs"))}
		}

		// As per https://github.com/Azure/Azurite/tree/main#start-azurite-with-https-and-pem-1
		command += fmt.Sprintf(
			" --cert %s/certs/cert.pem --key %s/certs/key.pem --oauth basic",
			f.Dir(),
			f.Dir(),
		)
	}
	command += "'"

	return e2emon.AsInstrumented(f.Init(e2e.StartOptions{
		Image: o.image,
		Command: e2e.NewCommandWithoutEntrypoint(
			"sh",
			"-c",
			command,
		),
	}), "blob")
}

// NewAzuriteBlobStorageWriter starts a container with azure CLI, to write data into azurite blob storage.
// This is needed, since Azurite doesn't support copying over dirs, but encodes into https://github.com/techfort/LokiJS db.
func NewAzuriteBlobStorageWriter(env e2e.Environment, name, containerName, tempDataDir string, opts ...Option) e2e.Runnable {
	o := options{image: "mcr.microsoft.com/azure-cli:latest", azuriteOptions: azuriteOptions{
		// As per https://github.com/Azure/Azurite/tree/main#default-storage-account
		connectionString: "\"DefaultEndpointsProtocol=http;AccountName=devstoreaccount1;AccountKey=Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw==;BlobEndpoint=http://0.0.0.0:10000/devstoreaccount1;\"",
	}}
	for _, opt := range opts {
		opt(&o)
	}

	f := env.Runnable(name).Future()

	command := fmt.Sprintf(
		"'az storage container create -n %s --connection-string=%s",
		containerName,
		o.azuriteOptions.connectionString,
	)

	command += fmt.Sprintf(
		"&& az storage blob upload-batch -d %s -s /shared/ --connection-string=%s'",
		containerName,
		o.azuriteOptions.connectionString,
	)

	return f.Init(e2e.StartOptions{
		Image: o.image,
		Command: e2e.NewCommandWithoutEntrypoint(
			"sh",
			"-c",
			command,
		),
		Volumes: []string{tempDataDir + ":/shared"},
	})
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

func NewSeaweedFS(env e2e.Environment, name, bucket string, opts ...Option) *e2emon.InstrumentedRunnable {
	o := options{image: "chrislusf/seaweedfs:4.47"}
	for _, opt := range opts {
		opt(&o)
	}

	const (
		adminPortName        = "admin"
		icebergPortName      = "iceberg"
		seaweedFSIcebergPort = 8181
	)
	ports := map[string]int{
		AccessPortName: 8333,
		adminPortName:  23646,
	}
	icebergPort := 0
	if o.seaweedOptions.enableIceberg {
		icebergPort = seaweedFSIcebergPort
		ports[icebergPortName] = seaweedFSIcebergPort
	}
	f := env.Runnable(name).WithPorts(ports).Future()
	dataDir := filepath.Join(f.Dir(), "data")
	if err := os.MkdirAll(dataDir, 0750); err != nil {
		return &e2emon.InstrumentedRunnable{Runnable: e2e.NewFailedRunnable(name, errors.Wrap(err, "create SeaweedFS data directory"))}
	}

	args := []string{
		"mini",
		"-dir=/data",
		fmt.Sprintf("-s3.port=%d", ports[AccessPortName]),
		"-master.telemetry=false",
		"-webdav=false",
		fmt.Sprintf("-s3.port.iceberg=%d", icebergPort),
		"-s3.port.lance=0",
		"-s3.autoCreateBucket=false",
	}
	readiness := e2e.ReadinessProbe(e2e.NewHTTPReadinessProbe(AccessPortName, "/readyz", http.StatusOK, http.StatusOK))
	instrumentedOpts := []e2emon.InstrumentedOption{}
	caFile := ""

	if o.seaweedOptions.enableTLS {
		certDir := filepath.Join(f.Dir(), "certs")
		caDir := filepath.Join(certDir, "CAs")
		if err := os.MkdirAll(caDir, 0750); err != nil {
			return &e2emon.InstrumentedRunnable{Runnable: e2e.NewFailedRunnable(name, errors.Wrap(err, "create SeaweedFS certificate directory"))}
		}

		certFile := filepath.Join(certDir, "public.crt")
		keyFile := filepath.Join(certDir, "private.key")
		caFile = filepath.Join(caDir, "ca.crt")
		if err := genCerts(certFile, keyFile, caFile, fmt.Sprintf("%s-%s", env.Name(), name)); err != nil {
			return &e2emon.InstrumentedRunnable{Runnable: e2e.NewFailedRunnable(name, errors.Wrap(err, "generate SeaweedFS certificates"))}
		}

		args = append(args, "-s3.cert.file="+certFile, "-s3.key.file="+keyFile)
		readiness = e2e.NewHTTPSReadinessProbe(AccessPortName, "/readyz", http.StatusOK, http.StatusOK)
		instrumentedOpts = append(instrumentedOpts, e2emon.WithInstrumentedScheme("https"))
	}

	envVars := map[string]string{
		"AWS_ACCESS_KEY_ID":     S3AccessKey,
		"AWS_SECRET_ACCESS_KEY": S3SecretKey,
	}
	if bucket != "" {
		envVars["S3_BUCKET"] = bucket
		readiness = &seaweedFSBucketReadinessProbe{
			readiness: readiness,
			bucket:    bucket,
			enableTLS: o.seaweedOptions.enableTLS,
			caFile:    caFile,
		}
	}

	r := f.Init(e2e.StartOptions{
		Image:     o.image,
		User:      strconv.Itoa(os.Getuid()),
		Command:   e2e.NewCommand(args[0], args[1:]...),
		EnvVars:   envVars,
		Readiness: readiness,
		Volumes:   []string{dataDir + ":/data:z"},
	})
	return e2emon.AsInstrumented(r, AccessPortName, instrumentedOpts...)
}

type seaweedFSBucketReadinessProbe struct {
	readiness e2e.ReadinessProbe
	bucket    string
	enableTLS bool
	caFile    string
}

// This explicitly checks if bucket exists or not (seaweedfs s3 gateway server might report healthy while bucket creation is still pending, which happens in later stage).
func (p *seaweedFSBucketReadinessProbe) Ready(runnable e2e.Runnable) error {
	if err := p.readiness.Ready(runnable); err != nil {
		return err
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	defer transport.CloseIdleConnections()
	if p.enableTLS {
		caPEM, err := os.ReadFile(p.caFile)
		if err != nil {
			return errors.Wrap(err, "read SeaweedFS CA certificate")
		}
		rootCAs := x509.NewCertPool()
		if !rootCAs.AppendCertsFromPEM(caPEM) {
			return errors.New("parse SeaweedFS CA certificate")
		}
		transport.TLSClientConfig = &tls.Config{RootCAs: rootCAs}
	}

	client, err := minio.New(runnable.Endpoint(AccessPortName), &minio.Options{
		Creds:     credentials.NewStaticV4(S3AccessKey, S3SecretKey, ""),
		Secure:    p.enableTLS,
		Transport: transport,
	})
	if err != nil {
		return errors.Wrap(err, "create SeaweedFS readiness client")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	exists, err := client.BucketExists(ctx, p.bucket)
	if err != nil {
		return errors.Wrap(err, "check SeaweedFS bucket")
	}
	if !exists {
		return errors.Newf("SeaweedFS bucket %q does not exist", p.bucket)
	}
	return nil
}
