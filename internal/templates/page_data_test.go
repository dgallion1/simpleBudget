package templates

import (
	"testing"
)

type fakeCountSource struct{ n int }

func (f fakeCountSource) UnresolvedDuplicateCount() int { return f.n }

func TestAttachDuplicateCount_SetsKey(t *testing.T) {
	pageData := map[string]interface{}{"Title": "Dashboard"}
	AttachDuplicateCount(pageData, fakeCountSource{n: 3})
	if got := pageData["UnresolvedDuplicateCount"]; got != 3 {
		t.Errorf("UnresolvedDuplicateCount = %v, want 3", got)
	}
}

func TestAttachDuplicateCount_NilSourceSetsZero(t *testing.T) {
	pageData := map[string]interface{}{}
	AttachDuplicateCount(pageData, nil)
	if got := pageData["UnresolvedDuplicateCount"]; got != 0 {
		t.Errorf("UnresolvedDuplicateCount = %v, want 0", got)
	}
}

func TestAttachDuplicateCount_NilMap_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("nil map should not panic, got: %v", r)
		}
	}()
	AttachDuplicateCount(nil, fakeCountSource{n: 1})
}
