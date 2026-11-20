package handler

import (
	"reflect"
	"testing"
)

func TestMergeHitlWhitelistKeepsUniqueOrderWithoutMutatingInputs(t *testing.T) {
	existing := []string{" Read ", "WRITE", ""}
	added := []string{"read", " Execute ", "write", " "}
	got := mergeHitlToolWhitelistSlice(existing, added)
	if !reflect.DeepEqual(got, []string{"Read", "WRITE", "Execute"}) {
		t.Fatalf("unexpected whitelist: %v", got)
	}
	if existing[0] != " Read " || added[1] != " Execute " {
		t.Fatal("input mutated")
	}
	if got := mergeHitlToolWhitelistSlice(nil, nil); got == nil || len(got) != 0 {
		t.Fatal("empty result must remain a non-nil empty slice")
	}
}
