package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"budget2/internal/services/storage"
)

// TestMain forks into a "subprocess as main()" mode when BE_MAIN=1 is
// set in the environment. The TestMain_* subprocess tests below set
// that env var when re-invoking the test binary so we can exercise the
// real main() function — including its os.Exit / log.Fatalf paths —
// without polluting the parent test process.
func TestMain(m *testing.M) {
	if os.Getenv("BE_MAIN") == "1" {
		args := strings.Fields(os.Getenv("BE_MAIN_ARGS"))
		os.Args = append([]string{"enrich-amazon"}, args...)
		// Fresh CommandLine so flag.Parse doesn't see -test.* flags.
		flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
		main()
		// main() returned normally — exit 0 so the parent sees success.
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// TestRun_EndToEnd creates a synthetic Amazon export + a synthetic CSV
// of bank transactions in a temp dir, runs the enrichment pipeline, and
// confirms the output file lands the expected hash→label entries.
func TestRun_EndToEnd(t *testing.T) {
	tmp := t.TempDir()
	amazonDir := filepath.Join(tmp, "amazon")
	dataDir := filepath.Join(tmp, "data")
	if err := os.MkdirAll(filepath.Join(amazonDir, "Your Amazon Orders"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Synthetic Order History.csv — one matchable row.
	orderCSV := `ASIN,"Billing Address","Carrier Name & Tracking Number",Currency,"Gift Message","Gift Recipient Contact","Gift Sender Name","Item Serial Number","Order Date","Order ID","Order Status","Original Quantity","Payment Method Type","Product Condition","Product Name","Purchase Order Number","Ship Date","Shipment Item Subtotal","Shipment Item Subtotal Tax","Shipment Status","Shipping Address","Shipping Charge","Shipping Option","Total Amount","Total Discounts","Unit Price","Unit Price Tax",Website
B01,"a","UPS",USD,"NA","NA","NA","NA",2024-06-01,111-TEST-0001,Closed,1,"V",New,"Test Coffee Beans","NA",2024-06-02,12.34,0,Shipped,"a",0,std,12.34,0,12.34,0,Amazon.com`
	if err := os.WriteFile(filepath.Join(amazonDir, "Your Amazon Orders", "Order History.csv"), []byte(orderCSV), 0644); err != nil {
		t.Fatal(err)
	}

	// Synthetic bank CSV with one matching Amazon charge + one non-Amazon row.
	bankCSV := "Date,Description,Amount\n" +
		"2024-06-03,AMZN MKTP US,-12.34\n" +
		"2024-06-04,Walmart,-50.00\n"
	if err := os.WriteFile(filepath.Join(dataDir, "bank.csv"), []byte(bankCSV), 0644); err != nil {
		t.Fatal(err)
	}

	// Capture stdout — the CLI prints its summary there.
	stdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	defer func() { os.Stdout = stdout }()

	done := make(chan []byte)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.Bytes()
	}()

	if err := run(amazonDir, dataDir, 5, false, 5); err != nil {
		w.Close()
		t.Fatalf("run() error: %v", err)
	}
	w.Close()
	out := <-done

	enrichPath := filepath.Join(dataDir, "amazon_enrichment.json")
	data, err := os.ReadFile(enrichPath)
	if err != nil {
		t.Fatalf("expected enrichment file at %s: %v", enrichPath, err)
	}
	var got map[string]string
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 enriched entry, got %d (%v)", len(got), got)
	}
	var label string
	for _, v := range got {
		label = v
	}
	if label != "Amazon: Test Coffee Beans" {
		t.Errorf("label = %q, want %q", label, "Amazon: Test Coffee Beans")
	}
	if !strings.Contains(string(out), "Total enriched:") {
		t.Errorf("expected summary in output, got: %s", out)
	}
}

func TestRun_DryRunDoesNotWrite(t *testing.T) {
	tmp := t.TempDir()
	amazonDir := filepath.Join(tmp, "amazon")
	dataDir := filepath.Join(tmp, "data")
	_ = os.MkdirAll(filepath.Join(amazonDir, "Your Amazon Orders"), 0755)
	_ = os.MkdirAll(dataDir, 0755)

	header := `ASIN,"Order Date","Order ID","Product Name","Ship Date","Total Amount"`
	row := `B01,2024-06-01,111-DRY-0001,"X",2024-06-02,1.00`
	_ = os.WriteFile(filepath.Join(amazonDir, "Your Amazon Orders", "Order History.csv"), []byte(header+"\n"+row), 0644)
	_ = os.WriteFile(filepath.Join(dataDir, "bank.csv"), []byte("Date,Description,Amount\n2024-06-02,Amazon.com,-1.00\n"), 0644)

	// Discard stdout
	stdout := os.Stdout
	devnull, _ := os.Open(os.DevNull)
	os.Stdout = devnull
	defer func() { os.Stdout = stdout }()

	if err := run(amazonDir, dataDir, 5, true, 0); err != nil {
		t.Fatalf("run() error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "amazon_enrichment.json")); !os.IsNotExist(err) {
		t.Errorf("dry-run should not write enrichment file (err=%v)", err)
	}
}

func TestIsAmazonDesc(t *testing.T) {
	cases := map[string]bool{
		"AMZN MKTP US":    true,
		"Amazon.com*ABC":  true,
		"AMAZON PRIME":    true,
		"Walmart":         false,
		"AMZN":            true,
		"":                false,
		"some amazon ad":  true,
	}
	for in, want := range cases {
		if got := isAmazonDesc(in); got != want {
			t.Errorf("isAmazonDesc(%q) = %v, want %v", in, got, want)
		}
	}
}

// =========================================================================
// run() error paths
// =========================================================================

// silenceStdout redirects os.Stdout to /dev/null for the duration of
// the test. run() prints a multi-line summary even on the error paths
// after a successful Match.
func silenceStdout(t *testing.T) {
	t.Helper()
	stdout := os.Stdout
	devnull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("open /dev/null: %v", err)
	}
	os.Stdout = devnull
	t.Cleanup(func() {
		os.Stdout = stdout
		_ = devnull.Close()
	})
}

// TestRun_AmazonLoadDirError forces amazon.LoadDir to return an error
// by writing an empty Order History.csv — ParseOrderHistory needs at
// least a header row, so it fails with "read header: EOF" and LoadDir
// wraps it as "parse".
func TestRun_AmazonLoadDirError(t *testing.T) {
	silenceStdout(t)
	tmp := t.TempDir()
	amazonDir := filepath.Join(tmp, "amazon")
	if err := os.MkdirAll(filepath.Join(amazonDir, "Your Amazon Orders"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(amazonDir, "Your Amazon Orders", "Order History.csv"), nil, 0644); err != nil {
		t.Fatal(err)
	}

	err := run(amazonDir, t.TempDir(), 5, true, 0)
	if err == nil || !strings.Contains(err.Error(), "load amazon dir") {
		t.Fatalf("expected 'load amazon dir' error, got %v", err)
	}
}

func TestRun_NoShipmentsError(t *testing.T) {
	silenceStdout(t)
	tmp := t.TempDir()
	amazonDir := filepath.Join(tmp, "amazon")
	if err := os.MkdirAll(filepath.Join(amazonDir, "Your Amazon Orders"), 0755); err != nil {
		t.Fatal(err)
	}
	// No CSV files in the orders directory → LoadDir returns no shipments.
	err := run(amazonDir, t.TempDir(), 5, true, 0)
	if err == nil || !strings.Contains(err.Error(), "no shipments") {
		t.Fatalf("expected 'no shipments' error, got %v", err)
	}
}

// TestRun_StorageNewError makes storage.New fail by leaving a marker
// file (".encrypted") next to a corrupt encryption-config JSON, which
// is exactly what storage.New rejects in its loadConfig branch.
func TestRun_StorageNewError(t *testing.T) {
	silenceStdout(t)
	tmp := t.TempDir()
	amazonDir := filepath.Join(tmp, "amazon")
	dataDir := filepath.Join(tmp, "data")
	if err := os.MkdirAll(filepath.Join(amazonDir, "Your Amazon Orders"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}
	csv := `ASIN,"Order Date","Order ID","Product Name","Ship Date","Total Amount"
B01,2024-06-01,111-X-0001,"Widget",2024-06-02,1.00`
	if err := os.WriteFile(filepath.Join(amazonDir, "Your Amazon Orders", "Order History.csv"), []byte(csv), 0644); err != nil {
		t.Fatal(err)
	}
	// Mark dataDir as encrypted but with corrupt config — storage.New rejects this.
	if err := os.WriteFile(filepath.Join(dataDir, ".encrypted"), []byte{}, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, ".encryption-config.json"), []byte("{not json"), 0644); err != nil {
		t.Fatal(err)
	}

	err := run(amazonDir, dataDir, 5, true, 0)
	if err == nil || !strings.Contains(err.Error(), "open storage") {
		t.Fatalf("expected 'open storage' error, got %v", err)
	}
}

// TestRun_MultipleMatchesPreviewSort exercises the sort.Slice closure
// in the preview path. With a single match the closure never runs;
// with two it must run at least once.
func TestRun_MultipleMatchesPreviewSort(t *testing.T) {
	silenceStdout(t)
	tmp := t.TempDir()
	amazonDir := filepath.Join(tmp, "amazon")
	dataDir := filepath.Join(tmp, "data")
	if err := os.MkdirAll(filepath.Join(amazonDir, "Your Amazon Orders"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}

	orderCSV := `ASIN,"Billing Address","Carrier Name & Tracking Number",Currency,"Gift Message","Gift Recipient Contact","Gift Sender Name","Item Serial Number","Order Date","Order ID","Order Status","Original Quantity","Payment Method Type","Product Condition","Product Name","Purchase Order Number","Ship Date","Shipment Item Subtotal","Shipment Item Subtotal Tax","Shipment Status","Shipping Address","Shipping Charge","Shipping Option","Total Amount","Total Discounts","Unit Price","Unit Price Tax",Website
B01,"a","UPS",USD,"NA","NA","NA","NA",2024-06-01,111-MUL-0001,Closed,1,"V",New,"Zebra Notebook","NA",2024-06-02,12.34,0,Shipped,"a",0,std,12.34,0,12.34,0,Amazon.com
B02,"a","UPS",USD,"NA","NA","NA","NA",2024-06-05,111-MUL-0002,Closed,1,"V",New,"Apple Charger","NA",2024-06-06,7.50,0,Shipped,"a",0,std,7.50,0,7.50,0,Amazon.com`
	if err := os.WriteFile(filepath.Join(amazonDir, "Your Amazon Orders", "Order History.csv"), []byte(orderCSV), 0644); err != nil {
		t.Fatal(err)
	}

	bankCSV := "Date,Description,Amount\n" +
		"2024-06-03,AMZN MKTP US,-12.34\n" +
		"2024-06-07,Amazon.com,-7.50\n"
	if err := os.WriteFile(filepath.Join(dataDir, "bank.csv"), []byte(bankCSV), 0644); err != nil {
		t.Fatal(err)
	}

	if err := run(amazonDir, dataDir, 5, true, 5); err != nil {
		t.Fatalf("run() error: %v", err)
	}
}

// TestRun_SaveEnrichmentError chmods the data dir 0o500 AFTER
// LoadData has cached the CSV, so the SaveAmazonEnrichment write
// fails. The .tmp write inside atomicWrite triggers the error.
func TestRun_SaveEnrichmentError(t *testing.T) {
	silenceStdout(t)
	tmp := t.TempDir()
	amazonDir := filepath.Join(tmp, "amazon")
	dataDir := filepath.Join(tmp, "data")
	if err := os.MkdirAll(filepath.Join(amazonDir, "Your Amazon Orders"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}
	orderCSV := `ASIN,"Order Date","Order ID","Product Name","Ship Date","Total Amount"
B01,2024-06-01,111-S-0001,"Widget",2024-06-02,12.34`
	if err := os.WriteFile(filepath.Join(amazonDir, "Your Amazon Orders", "Order History.csv"), []byte(orderCSV), 0644); err != nil {
		t.Fatal(err)
	}
	bankCSV := "Date,Description,Amount\n2024-06-03,AMZN MKTP US,-12.34\n"
	if err := os.WriteFile(filepath.Join(dataDir, "bank.csv"), []byte(bankCSV), 0644); err != nil {
		t.Fatal(err)
	}

	// Block writes to the data dir but keep it readable so LoadData /
	// existing CSVs still work.
	if err := os.Chmod(dataDir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dataDir, 0o755) })

	err := run(amazonDir, dataDir, 5, false, 0)
	if err == nil || !strings.Contains(err.Error(), "save enrichment") {
		t.Fatalf("expected 'save enrichment' error, got %v", err)
	}
}

// =========================================================================
// run() with an encrypted data directory. Each test pre-encrypts the
// dataDir via storage.EnableEncryption so the run() codepath that
// reads credentials and unlocks the store is exercised end-to-end.
// =========================================================================

// encryptedDataDir creates an Amazon export + a bank CSV under tmp,
// then encrypts the data directory with the given password. Returns
// (amazonDir, dataDir).
func encryptedDataDir(t *testing.T, tmp, password string) (string, string) {
	t.Helper()
	amazonDir := filepath.Join(tmp, "amazon")
	dataDir := filepath.Join(tmp, "data")
	if err := os.MkdirAll(filepath.Join(amazonDir, "Your Amazon Orders"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}
	csv := `ASIN,"Order Date","Order ID","Product Name","Ship Date","Total Amount"
B01,2024-06-01,111-ENC-0001,"Widget",2024-06-02,1.00`
	if err := os.WriteFile(filepath.Join(amazonDir, "Your Amazon Orders", "Order History.csv"), []byte(csv), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "bank.csv"), []byte("Date,Description,Amount\n2024-06-02,Amazon.com,-1.00\n"), 0644); err != nil {
		t.Fatal(err)
	}

	store, err := storage.New(dataDir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	if err := store.EnableEncryption(password); err != nil {
		t.Fatalf("EnableEncryption: %v", err)
	}
	return amazonDir, dataDir
}

func TestRun_EncryptedStoreSuccess(t *testing.T) {
	silenceStdout(t)
	amazonDir, dataDir := encryptedDataDir(t, t.TempDir(), "rightpw1234")
	t.Setenv("BUDGET2_PASSWORD", "rightpw1234")
	if err := run(amazonDir, dataDir, 5, true, 0); err != nil {
		t.Fatalf("run() error: %v", err)
	}
}

func TestRun_EncryptedStoreUnlockFails(t *testing.T) {
	silenceStdout(t)
	amazonDir, dataDir := encryptedDataDir(t, t.TempDir(), "rightpw1234")
	t.Setenv("BUDGET2_PASSWORD", "wrongpw00000")
	err := run(amazonDir, dataDir, 5, true, 0)
	if err == nil || !strings.Contains(err.Error(), "unlock storage") {
		t.Fatalf("expected 'unlock storage' error, got %v", err)
	}
}

func TestRun_EncryptedStoreReadCredentialsFails(t *testing.T) {
	silenceStdout(t)
	amazonDir, dataDir := encryptedDataDir(t, t.TempDir(), "rightpw1234")
	t.Setenv("BUDGET2_PASSWORD", "")
	// Non-TTY stdin (default for go test) → readCredentials returns
	// the "storage is locked: set BUDGET2_PASSWORD…" error, which run
	// wraps as "read credentials".
	err := run(amazonDir, dataDir, 5, true, 0)
	if err == nil || !strings.Contains(err.Error(), "read credentials") {
		t.Fatalf("expected 'read credentials' error, got %v", err)
	}
}

// =========================================================================
// readCredentials() — env-var path and non-TTY error path. The TTY
// branch (term.ReadPassword on a real PTY) is uncoverable from a unit
// test and is treated as a coverage ceiling.
// =========================================================================

func TestReadCredentials_FromEnv(t *testing.T) {
	t.Setenv("BUDGET2_PASSWORD", "supersecret")
	pw, err := readCredentials()
	if err != nil {
		t.Fatalf("readCredentials: %v", err)
	}
	if pw != "supersecret" {
		t.Errorf("pw = %q, want %q", pw, "supersecret")
	}
}

func TestReadCredentials_NonTTYReturnsError(t *testing.T) {
	t.Setenv("BUDGET2_PASSWORD", "")
	// go test runs with stdin as a pipe, not a TTY → term.IsTerminal
	// returns false and the function reports the locked-storage error.
	_, err := readCredentials()
	if err == nil || !strings.Contains(err.Error(), "storage is locked") {
		t.Fatalf("expected 'storage is locked' error, got %v", err)
	}
}

// =========================================================================
// main() — exercised via subprocess re-invocation of the test binary.
// TestMain at the top of this file dispatches into main() when
// BE_MAIN=1 is set, with BE_MAIN_ARGS providing the os.Args.
// =========================================================================

func TestMain_SuccessExits0(t *testing.T) {
	tmp := t.TempDir()
	amazonDir := filepath.Join(tmp, "amazon")
	dataDir := filepath.Join(tmp, "data")
	if err := os.MkdirAll(filepath.Join(amazonDir, "Your Amazon Orders"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}
	csv := `ASIN,"Order Date","Order ID","Product Name","Ship Date","Total Amount"
B01,2024-06-01,111-MAIN-0001,"Widget",2024-06-02,1.00`
	if err := os.WriteFile(filepath.Join(amazonDir, "Your Amazon Orders", "Order History.csv"), []byte(csv), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "bank.csv"), []byte("Date,Description,Amount\n2024-06-02,Amazon.com,-1.00\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestMain_SuccessExits0")
	cmd.Env = append(os.Environ(),
		"BE_MAIN=1",
		"BE_MAIN_ARGS=-amazon-dir "+amazonDir+" -data-dir "+dataDir+" -dry-run -preview 0",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("subprocess failed: %v\noutput:\n%s", err, out)
	}
}

func TestMain_MissingAmazonDirExits2(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestMain_MissingAmazonDirExits2")
	cmd.Env = append(os.Environ(), "BE_MAIN=1", "BE_MAIN_ARGS=")
	err := cmd.Run()
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected *exec.ExitError, got %v", err)
	}
	if got := exitErr.ExitCode(); got != 2 {
		t.Errorf("ExitCode() = %d, want 2 (flag.Usage + os.Exit(2))", got)
	}
}

func TestMain_RunFailureExits1(t *testing.T) {
	tmp := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=TestMain_RunFailureExits1")
	cmd.Env = append(os.Environ(),
		"BE_MAIN=1",
		"BE_MAIN_ARGS=-amazon-dir "+filepath.Join(tmp, "does-not-exist"),
	)
	err := cmd.Run()
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected *exec.ExitError, got %v", err)
	}
	if got := exitErr.ExitCode(); got != 1 {
		t.Errorf("ExitCode() = %d, want 1 (log.Fatalf)", got)
	}
}
