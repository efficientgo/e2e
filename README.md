# e2e

[![golang docs](https://img.shields.io/badge/go.dev-reference-007d9c?logo=go&logoColor=white&style=flat-square)](https://pkg.go.dev/github.com/efficientgo/e2e)

Robust framework for running complex workload scenarios in isolation, using Go; for integration, e2e tests, benchmarks and more!

## What is it?

`e2e` is a Go module which implements a fully featured e2e suite allowing utilizing `go test` for setting hermetic up complex microservice testing scenarios using `docker`.

```go mdox-gen-exec="sh -c 'tail -n +6 e2e/doc.go'"
// This module is a fully featured e2e suite allowing utilizing `go test` for setting hermetic up complex microservice integration testing scenarios using docker.
// Example usages:
//  * https://github.com/cortexproject/cortex/tree/master/integration
//  * https://github.com/thanos-io/thanos/tree/master/test/e2e
//
// Check github.com/efficientgo/tools/e2e/db for common DBs services you can run out of the box.
```

Credits:

* [Cortex Team](https://github.com/cortexproject/cortex/tree/f639b1855c9f0c9564113709a6bce2996d151ec7/integration)
* Initial Authors: [@pracucci](https://github.com/pracucci), [@bwplotka](https://github.com/bwplotka)
