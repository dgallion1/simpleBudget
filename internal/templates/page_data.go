package templates

// DuplicateCountSource is the minimal contract handlers need to attach
// the unresolved-duplicate count to a page-data map. Implemented by
// *dataloader.DataLoader.
type DuplicateCountSource interface {
	UnresolvedDuplicateCount() int
}

// AttachDuplicateCount sets pageData["UnresolvedDuplicateCount"] from
// the source. Safe with a nil source (writes 0) and a nil map (no-op).
// Handlers should call this before rendering any full-page template
// so the nav badge and dashboard alert see the same value.
func AttachDuplicateCount(pageData map[string]interface{}, src DuplicateCountSource) {
	if pageData == nil {
		return
	}
	count := 0
	if src != nil {
		count = src.UnresolvedDuplicateCount()
	}
	pageData["UnresolvedDuplicateCount"] = count
}
