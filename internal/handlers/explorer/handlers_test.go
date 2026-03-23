package explorer

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"budget2/internal/config"
	"budget2/internal/services/dataloader"
	"budget2/internal/services/storage"
)

func TestHandleFileUploadReplacesExistingCSV(t *testing.T) {
	dataDir := t.TempDir()

	testStore, err := storage.New(dataDir)
	if err != nil {
		t.Fatalf("storage.New() error: %v", err)
	}

	loader = dataloader.New(dataDir, testStore)
	renderer = nil
	cfg = &config.Config{DataDirectory: dataDir}
	store = testStore

	existingPath := filepath.Join(dataDir, "transactions.csv")
	existing := []byte("Date,Description,Amount\n2024-01-01,Old Row,10.00\n")
	if err := store.WriteFile(existingPath, existing, 0644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	uploaded := []byte("Date,Description,Amount\n2024-02-02,New Row,25.00\n")
	req := newUploadRequest(t, "transactions.csv", uploaded)
	rec := httptest.NewRecorder()

	handleFileUpload(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	saved, err := store.ReadFile(existingPath)
	if err != nil {
		t.Fatalf("ReadFile() error: %v", err)
	}
	if string(saved) != string(uploaded) {
		t.Fatalf("expected uploaded content to replace existing file\n got: %q\nwant: %q", string(saved), string(uploaded))
	}

	var payload struct {
		Files []any `json:"Files"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("response JSON error: %v", err)
	}
}

func TestSanitizeUploadFilename(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "plain", input: "transactions.csv", want: "transactions.csv"},
		{name: "windows path", input: `C:\fakepath\transactions.csv`, want: "transactions.csv"},
		{name: "unix path", input: "../../transactions.csv", want: "transactions.csv"},
		{name: "empty", input: "", wantErr: true},
		{name: "dot", input: ".", wantErr: true},
		{name: "dotdot basename", input: "..", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := sanitizeUploadFilename(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func newUploadRequest(t *testing.T, filename string, content []byte) *http.Request {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("CreateFormFile() error: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("part.Write() error: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close() error: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/explorer/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("HX-Request", "true")
	return req
}
