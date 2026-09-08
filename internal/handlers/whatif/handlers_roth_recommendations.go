package whatif

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"sync"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/retirement"
	"budget2/internal/services/retirement/analysis"
)

const rothRecommendationLimit = 128
const rothRecommendationLifetime = 15 * time.Minute

type rothRecommendation struct {
	manager         *retirement.SettingsManager
	scenario        string
	revision        int
	fingerprint     [32]byte
	roth            models.RothConversionConfig
	primary, spouse int
	expires         time.Time
}

var rothRecommendations = struct {
	sync.Mutex
	entries map[string]rothRecommendation
}{entries: make(map[string]rothRecommendation)}

type rothRecommendationOption struct {
	Token     string
	Candidate models.TaxOptimizerCandidate
}

func cacheRothRecommendation(manager *retirement.SettingsManager, scenario string, revision int, s *models.WhatIfSettings, roth *models.RothConversionConfig, primary, spouse int) (string, error) {
	raw, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", err
	}
	token := hex.EncodeToString(tokenBytes)
	cfg := *roth
	if roth.PerYearOverrides != nil {
		cfg.PerYearOverrides = make(map[int]float64, len(roth.PerYearOverrides))
		for y, v := range roth.PerYearOverrides {
			cfg.PerYearOverrides[y] = v
		}
	}
	now := time.Now()
	rothRecommendations.Lock()
	defer rothRecommendations.Unlock()
	for key, entry := range rothRecommendations.entries {
		if !now.Before(entry.expires) {
			delete(rothRecommendations.entries, key)
		}
	}
	for len(rothRecommendations.entries) >= rothRecommendationLimit {
		var oldest string
		var expiration time.Time
		for key, entry := range rothRecommendations.entries {
			if oldest == "" || entry.expires.Before(expiration) {
				oldest, expiration = key, entry.expires
			}
		}
		delete(rothRecommendations.entries, oldest)
	}
	rothRecommendations.entries[token] = rothRecommendation{manager: manager, scenario: scenario, revision: revision, fingerprint: sha256.Sum256(raw), roth: cfg, primary: primary, spouse: spouse, expires: now.Add(rothRecommendationLifetime)}
	return token, nil
}

func rothRecommendationError(w http.ResponseWriter, message string, status int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("HX-Retarget", "#roth-recommendations-results")
	w.Header().Set("HX-Reswap", "innerHTML")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `<p role="alert" tabindex="-1" class="text-sm text-negative rounded focus:outline focus:outline-2 focus:outline-accent">%s Find recommendations again and retry.</p>`, html.EscapeString(message))
}

// handleRothRecommendations runs only on explicit request. Identities contain no
// client-supplied financial inputs and are local to this server's manager.
func handleRothRecommendations(w http.ResponseWriter, r *http.Request) {
	manager := retirementMgr
	scenario := manager.ActiveFilename()
	settings, revision, err := manager.LoadContextWithRevision(r.Context())
	if err != nil {
		rothRecommendationError(w, "Could not load the plan.", http.StatusInternalServerError)
		return
	}
	if scenario != manager.ActiveFilename() {
		rothRecommendationError(w, "The active plan changed.", http.StatusConflict)
		return
	}
	in, _, err := buildEngineInput(settings)
	if err != nil {
		rothRecommendationError(w, "Could not prepare this plan.", http.StatusInternalServerError)
		return
	}
	result := retirement.RunTaxOptimizer(getEngine(), in)
	options := make([]rothRecommendationOption, 0, len(result.Top))
	for _, candidate := range result.Top {
		exact, err := analysis.SettingsForTaxOptimizerCandidate(in.Prepared.Settings(), candidate)
		if err != nil {
			rothRecommendationError(w, "Could not prepare the recommendation.", http.StatusInternalServerError)
			return
		}
		token, err := cacheRothRecommendation(manager, scenario, revision, settings, exact.RothConversion, candidate.PrimaryClaimAge, candidate.SpouseClaimAge)
		if err != nil {
			rothRecommendationError(w, "Could not retain recommendations.", http.StatusInternalServerError)
			return
		}
		options = append(options, rothRecommendationOption{Token: token, Candidate: candidate})
	}
	data := map[string]any{"Options": options, "Optimizer": result, "Revision": revision}
	w.Header().Set("Cache-Control", "no-store")
	if renderer != nil {
		if err := renderer.RenderPartial(w, "whatif-roth-recommendations", data); err != nil {
			rothRecommendationError(w, "Could not display recommendations.", http.StatusInternalServerError)
		}
	} else {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(data)
	}
}

func handleApplyRothRecommendation(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		rothRecommendationError(w, "Invalid request.", http.StatusBadRequest)
		return
	}
	token := r.PostForm.Get("recommendation")
	rothRecommendations.Lock()
	entry, ok := rothRecommendations.entries[token]
	rothRecommendations.Unlock()
	if !ok || entry.manager != retirementMgr || !time.Now().Before(entry.expires) {
		rothRecommendationError(w, "This recommendation is unavailable or expired. Nothing was saved.", http.StatusConflict)
		return
	}
	s, revision, err := entry.manager.LoadContextWithRevision(r.Context())
	if err != nil {
		rothRecommendationError(w, "Could not load the plan. Nothing was saved.", http.StatusInternalServerError)
		return
	}
	raw, err := json.Marshal(s)
	if err != nil || revision != entry.revision || entry.scenario != entry.manager.ActiveFilename() || sha256.Sum256(raw) != entry.fingerprint {
		rothRecommendationError(w, "The plan changed since these recommendations were generated. Nothing was saved.", http.StatusConflict)
		return
	}
	if s.SocialSecurity == nil {
		rothRecommendationError(w, "Social Security settings are unavailable. Nothing was saved.", http.StatusConflict)
		return
	}
	s.RothConversion = &entry.roth
	s.SocialSecurity.ClaimAge = entry.primary
	s.SocialSecurity.SpouseClaimAge = entry.spouse
	revision, err = entry.manager.SaveWithRevisionIfScenario(s, entry.scenario, entry.revision)
	if err != nil {
		rothRecommendationError(w, "The recommendation could not be saved: "+err.Error(), statusForMutationError(err))
		return
	}
	rothRecommendations.Lock()
	delete(rothRecommendations.entries, token)
	rothRecommendations.Unlock()
	// A full reload refreshes every dependent SS control and analysis together.
	// The card's script announces success and focuses its heading after navigation.
	redirectAfterRothChange(w, revision, "roth-recommendation-applied")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
}

// A query change forces document navigation even when the user is already on
// /whatif. A fragment alone would leave the old controls and scripts in place.
func redirectAfterRothChange(w http.ResponseWriter, revision int, fragment string) {
	w.Header().Set("HX-Redirect", fmt.Sprintf("/whatif?roth_applied=%d#%s", revision, fragment))
}
