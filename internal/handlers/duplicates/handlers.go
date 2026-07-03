// Package duplicates serves the near-duplicate review panel.
package duplicates

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"budget2/internal/services/dataloader"
	"budget2/internal/templates"
)

var (
	loader   *dataloader.DataLoader
	renderer *templates.Renderer
)

// Initialize wires package dependencies. Both arguments may be nil in
// tests (the page falls back to a JSON-encoded payload when there is
// no renderer).
func Initialize(l *dataloader.DataLoader, r *templates.Renderer) {
	loader = l
	renderer = r
}

// RegisterRoutes registers all duplicates routes.
func RegisterRoutes(r chi.Router) {
	r.Get("/duplicates", handlePage)
	r.Post("/duplicates/resolve", handleResolve)
	r.Post("/duplicates/undo", handleUndo)
}

func handlePage(w http.ResponseWriter, r *http.Request) {
	pageData := buildPageData()
	// AttachDuplicateCount is nil-safe for both pageData and loader,
	// but we must pass nil explicitly if loader is nil, not the nil pointer,
	// because interface nil-checking distinguishes between nil pointers and nil.
	var loaderSource templates.DuplicateCountSource
	if loader != nil {
		loaderSource = loader
	}
	templates.AttachDuplicateCount(pageData, loaderSource)

	if renderer != nil {
		_ = renderer.Render(w, "base", pageData)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(pageData); err != nil {
		log.Printf("duplicates: encoding page JSON: %v", err)
	}
}

func buildPageData() map[string]interface{} {
	pageData := map[string]interface{}{
		"Title":     "Duplicates",
		"ActiveTab": "duplicates",
	}
	if loader == nil {
		// Fallback empty values so the template always has the keys.
		pageData["Unresolved"] = []dataloader.DuplicatePair{}
		pageData["Resolved"] = []dataloader.DuplicatePair{}
		return pageData
	}
	// Trigger a load so detection state is fresh; ignore the
	// transaction set itself (the panel doesn't render rows directly).
	if _, err := loader.LoadData(); err != nil {
		log.Printf("duplicates: failed to load data: %v", err)
	}
	pageData["Unresolved"] = loader.UnresolvedDuplicates()
	pageData["Resolved"] = loader.ResolvedDuplicates()
	return pageData
}

func handleResolve(w http.ResponseWriter, r *http.Request) {
	if loader == nil {
		http.Error(w, "loader not initialized", http.StatusInternalServerError)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form: "+err.Error(), http.StatusBadRequest)
		return
	}
	pairKey := r.FormValue("pair_key")
	outcome := r.FormValue("outcome")
	keptHash := r.FormValue("kept_hash")
	suppressedHash := r.FormValue("suppressed_hash")

	if pairKey == "" {
		http.Error(w, "missing pair_key", http.StatusBadRequest)
		return
	}
	dec := dataloader.DuplicateDecision{
		Outcome:        outcome,
		KeptHash:       keptHash,
		SuppressedHash: suppressedHash,
	}
	if err := loader.SaveDuplicateDecision(pairKey, dec); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/duplicates", http.StatusSeeOther)
}

func handleUndo(w http.ResponseWriter, r *http.Request) {
	if loader == nil {
		http.Error(w, "loader not initialized", http.StatusInternalServerError)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form: "+err.Error(), http.StatusBadRequest)
		return
	}
	pairKey := r.FormValue("pair_key")
	if pairKey == "" {
		http.Error(w, "missing pair_key", http.StatusBadRequest)
		return
	}
	if err := loader.ClearDuplicateDecision(pairKey); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/duplicates", http.StatusSeeOther)
}
