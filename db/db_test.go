package e2edb

import (
	"testing"

	"github.com/efficientgo/core/testutil"
	"github.com/efficientgo/e2e"
)

func TestMinio(t *testing.T) {
	t.Parallel()

	e, err := e2e.New()
	testutil.Ok(t, err)
	t.Cleanup(e.Close)

	minio := NewMinio(e, "mintest", "bkt")
	testutil.Ok(t, e2e.StartAndWaitReady(minio))
}
