# e2e test using prometheus-example-app

https://github.com/brancz/prometheus-example-app is a go application instrumented with a couple of
metrics using the [Prometheus go client](https://github.com/prometheus/client_golang).

For this example we create an e2e test providing the available Docker image at `quay.io/brancz/prometheus-example-app:v0.3.0`

The goal is to show:
* How an e2e interactive test works using a simple application as an example
* How it is possible to take advantage of functions like `WaitSumMetricsWithOptions` to ensure that a certain metric
is present
* How to easy have access to a Prometheus UI and manually interact with the scenario in progress

## Run

```bash
make run-example-app
```


