package esxi

import (
	"context"
	"testing"
)

func TestFileCopyMoveRejectSameAndNestedDestinations(t *testing.T) {
	client := &Client{}
	for _, copy := range []bool{true, false} {
		for _, dst := range []string{"source", "source/child"} {
			var err error
			if copy {
				_, err = client.FSCopy(context.Background(), "fixture", "source", dst, true, false)
			} else {
				_, err = client.FSMove(context.Background(), "fixture", "source", dst, true, false)
			}
			if err == nil {
				t.Fatal("unsafe destination accepted", copy, dst)
			}
		}
	}
	result, err := client.FSCopy(context.Background(), "fixture", "source", "target", false, true)
	if err != nil || !result.DryRun {
		t.Fatal("dry-run needs a connection", err)
	}
}
