package auth

import (
	"context"
	"reflect"
	"testing"
)

func TestWithheldContextRoundTrip(t *testing.T) {
	if got := WithheldFromContext(context.Background()); len(got) != 0 {
		t.Fatalf("leerer Context liefert %v", got)
	}
	ctx := WithWithheld(context.Background(), []string{RoleBackupExport})
	if got := WithheldFromContext(ctx); !reflect.DeepEqual(got, []string{RoleBackupExport}) {
		t.Fatalf("Context liefert %v", got)
	}
}
