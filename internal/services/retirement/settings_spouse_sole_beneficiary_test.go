package retirement

import "testing"

// TestUpdateSettings_SpouseSoleBeneficiary_RoundTrip exercises the full apply
// layer for the spouse_sole_beneficiary key: UpdateSettings writes it through
// applySettingsUpdates and persists to disk, Load reads it back, and a
// subsequent partial update that omits the key must preserve the stored value.
func TestUpdateSettings_SpouseSoleBeneficiary_RoundTrip(t *testing.T) {
	sm := newTestSM(t)

	// Explicit false persists and reads back as false.
	if _, _, err := sm.UpdateSettings(map[string]interface{}{"spouse_sole_beneficiary": false}); err != nil {
		t.Fatalf("UpdateSettings(false): %v", err)
	}
	loaded, err := sm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.SpouseSoleBeneficiary == nil || *loaded.SpouseSoleBeneficiary != false {
		t.Fatalf("after persisting false, SpouseSoleBeneficiary = %v, want &false", loaded.SpouseSoleBeneficiary)
	}
	if loaded.IsSpouseSoleBeneficiary() {
		t.Errorf("IsSpouseSoleBeneficiary() = true after explicit false")
	}

	// A partial update that does NOT carry the key must leave false intact.
	if _, _, err := sm.UpdateSettings(map[string]interface{}{"portfolio_value": float64(750000)}); err != nil {
		t.Fatalf("UpdateSettings(partial): %v", err)
	}
	loaded, err = sm.Load()
	if err != nil {
		t.Fatalf("Load after partial: %v", err)
	}
	if loaded.SpouseSoleBeneficiary == nil || *loaded.SpouseSoleBeneficiary != false {
		t.Errorf("partial update cleared the flag: SpouseSoleBeneficiary = %v, want &false", loaded.SpouseSoleBeneficiary)
	}

	// Flipping back to true persists.
	if _, _, err := sm.UpdateSettings(map[string]interface{}{"spouse_sole_beneficiary": true}); err != nil {
		t.Fatalf("UpdateSettings(true): %v", err)
	}
	loaded, err = sm.Load()
	if err != nil {
		t.Fatalf("Load after true: %v", err)
	}
	if !loaded.IsSpouseSoleBeneficiary() {
		t.Errorf("IsSpouseSoleBeneficiary() = false after explicit true")
	}
}
