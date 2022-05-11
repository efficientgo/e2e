# Example App

This folder contains a simple HTTP server instrumented with a `http_requests_total` metric,
that counts the number of GET requests to the `/` endpoint.

It uses the Go [client](https://github.com/prometheus/client_golang) library for [Prometheus](https://prometheus.io/).

## Running

From the root of the repository, run:

```bash
make run-example-app
```

This will build a local docker image and run it exposed to the port `2112`.

You can access the root endpoint at `http://localhost:2112/`
and the metrics endpoint at `http://localhost:2112/metrics`