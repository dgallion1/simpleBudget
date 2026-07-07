package templates

import (
	"fmt"
	"html/template"
	"io/fs"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"budget2/internal/models"
)

// ============================================================
// Template helper function tests
// ============================================================

func TestFormatMoney(t *testing.T) {
	tests := []struct {
		in   float64
		want string
	}{
		{0, "$0.00"},
		{1234.56, "$1,234.56"},
		{-1234.56, "-$1,234.56"},
		{1000000.99, "$1,000,000.99"},
		{0.5, "$0.50"},
		{999.999, "$1,000.00"},
		{-0.01, "-$0.01"},
		{123456789.12, "$123,456,789.12"},
	}
	for _, tt := range tests {
		got := formatMoney(tt.in)
		if got != tt.want {
			t.Errorf("formatMoney(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestConversionSummary(t *testing.T) {
	tests := []struct {
		name string
		in   []models.YearlyConversion
		want string
	}{
		{
			name: "empty returns empty string",
			in:   nil,
			want: "",
		},
		{
			name: "single entry, whole-dollar total",
			in: []models.YearlyConversion{
				{Age: 67, Amount: 5_400},
			},
			want: "Avg $5,400  ·  Min $5,400  ·  Max $5,400  ·  Total $5,400 over 1 year",
		},
		{
			name: "multi-entry, K-abbreviated total",
			in: []models.YearlyConversion{
				{Age: 67, Amount: 50_000},
				{Age: 68, Amount: 50_000},
				{Age: 69, Amount: 50_000},
			},
			want: "Avg $50,000  ·  Min $50,000  ·  Max $50,000  ·  Total $150K over 3 years",
		},
		{
			name: "multi-entry, M-abbreviated total with varying amounts",
			in: []models.YearlyConversion{
				{Age: 67, Amount: 320_400},
				{Age: 68, Amount: 310_200},
				{Age: 69, Amount: 300_600},
				{Age: 70, Amount: 291_500},
				{Age: 71, Amount: 283_000},
				{Age: 72, Amount: 275_100},
			},
			// Sum = 1_780_800 → "$1.78M". Avg = 296_800.
			want: "Avg $296,800  ·  Min $275,100  ·  Max $320,400  ·  Total $1.78M over 6 years",
		},
		{
			name: "sub-$10K total uses whole-dollar formatting",
			in: []models.YearlyConversion{
				{Age: 67, Amount: 2_500},
				{Age: 68, Amount: 4_000},
			},
			want: "Avg $3,250  ·  Min $2,500  ·  Max $4,000  ·  Total $6,500 over 2 years",
		},
		{
			name: "exactly $10K total uses K abbreviation",
			in: []models.YearlyConversion{
				{Age: 67, Amount: 5_000},
				{Age: 68, Amount: 5_000},
			},
			want: "Avg $5,000  ·  Min $5,000  ·  Max $5,000  ·  Total $10K over 2 years",
		},
		{
			name: "exactly $1M total uses M abbreviation",
			in: []models.YearlyConversion{
				{Age: 67, Amount: 500_000},
				{Age: 68, Amount: 500_000},
			},
			want: "Avg $500,000  ·  Min $500,000  ·  Max $500,000  ·  Total $1.00M over 2 years",
		},
		{
			name: "negative total handled in default branch",
			in: []models.YearlyConversion{
				{Age: 67, Amount: -2_500},
				{Age: 68, Amount: -3_000},
			},
			want: "Avg -$2,750  ·  Min -$3,000  ·  Max -$2,500  ·  Total -$5,500 over 2 years",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := conversionSummary(tt.in)
			if got != tt.want {
				t.Errorf("conversionSummary:\n got: %q\nwant: %q", got, tt.want)
			}
		})
	}
}

func TestFormatNumber(t *testing.T) {
	tests := []struct {
		in   float64
		want string
	}{
		{0, "0"},
		{1234, "1,234"},
		{-1234, "-1,234"},
		{1000000, "1,000,000"},
		{999, "999"},
		{42, "42"},
	}
	for _, tt := range tests {
		got := formatNumber(tt.in)
		if got != tt.want {
			t.Errorf("formatNumber(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFormatPercent(t *testing.T) {
	tests := []struct {
		in   float64
		want string
	}{
		{5.5, "+5.5"},
		{-3.2, "-3.2"},
		{0, "0.0"},
	}
	for _, tt := range tests {
		got := formatPercent(tt.in)
		if got != tt.want {
			t.Errorf("formatPercent(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFormatDollarsTemplateFunc(t *testing.T) {
	fn, ok := getFuncMap()["formatDollars"].(func(float64) string)
	if !ok {
		t.Fatal("formatDollars not registered in template func map")
	}
	cases := []struct {
		in   float64
		want string
	}{
		{1624993.75, "$1,624,994"},
		{-6435.53, "-$6,436"},
		{0, "$0"},
		{342706.4, "$342,706"},
	}
	for _, c := range cases {
		if got := fn(c.in); got != c.want {
			t.Errorf("formatDollars(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFormatDate(t *testing.T) {
	tm := time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC)
	got := formatDate(tm)
	if got != "Mar 15, 2024" {
		t.Errorf("formatDate = %q, want %q", got, "Mar 15, 2024")
	}
	if formatDate(time.Time{}) != "" {
		t.Error("formatDate(zero) should be empty")
	}
}

func TestFormatDateTime(t *testing.T) {
	tm := time.Date(2024, 3, 15, 14, 30, 0, 0, time.UTC)
	got := formatDateTime(tm)
	if got != "Mar 15, 2024 2:30 PM" {
		t.Errorf("formatDateTime = %q, want %q", got, "Mar 15, 2024 2:30 PM")
	}
	if formatDateTime(time.Time{}) != "" {
		t.Error("formatDateTime(zero) should be empty")
	}
}

func TestAbs(t *testing.T) {
	if abs(-5.0) != 5.0 {
		t.Error("abs(-5) should be 5")
	}
	if abs(5.0) != 5.0 {
		t.Error("abs(5) should be 5")
	}
	if abs(0) != 0 {
		t.Error("abs(0) should be 0")
	}
}

func TestAdd(t *testing.T) {
	// int + int -> int
	result := add(3, 4)
	if v, ok := result.(int); !ok || v != 7 {
		t.Errorf("add(3,4) = %v, want int 7", result)
	}
	// int + float -> float (falls through to toFloat path)
	result = add(3, 4.5)
	if v, ok := result.(float64); !ok || v != 7.5 {
		t.Errorf("add(3, 4.5) = %v, want float64 7.5", result)
	}
	// float + float -> float
	result = add(1.5, 2.5)
	if v, ok := result.(float64); !ok || v != 4.0 {
		t.Errorf("add(1.5, 2.5) = %v, want float64 4.0", result)
	}
}

func TestSub(t *testing.T) {
	if sub(10, 3) != 7 {
		t.Error("sub(10,3) should be 7")
	}
	if sub(3.5, 1.5) != 2.0 {
		t.Error("sub(3.5,1.5) should be 2.0")
	}
}

func TestMul(t *testing.T) {
	if mul(3, 4) != 12 {
		t.Error("mul(3,4) should be 12")
	}
	if mul(2.5, 4.0) != 10.0 {
		t.Error("mul(2.5,4.0) should be 10")
	}
}

func TestDiv(t *testing.T) {
	if div(10, 2) != 5 {
		t.Error("div(10,2) should be 5")
	}
	if div(10, 0) != 0 {
		t.Error("div(10,0) should be 0")
	}
	if div(7.5, 2.5) != 3.0 {
		t.Error("div(7.5,2.5) should be 3")
	}
}

func TestMod(t *testing.T) {
	if mod(10, 3) != 1 {
		t.Error("mod(10,3) should be 1")
	}
	if mod(10, 0) != 0 {
		t.Error("mod(10,0) should be 0")
	}
}

func TestToFloat(t *testing.T) {
	tests := []struct {
		name string
		in   interface{}
		want float64
	}{
		{"int", 42, 42.0},
		{"int64", int64(99), 99.0},
		{"float64", 3.14, 3.14},
		{"float32", float32(2.5), 2.5},
		{"*int non-nil", intPtr(10), 10.0},
		{"*int nil", (*int)(nil), 0},
		{"*int64 non-nil", int64Ptr(20), 20.0},
		{"*int64 nil", (*int64)(nil), 0},
		{"*float64 non-nil", float64Ptr(1.5), 1.5},
		{"*float64 nil", (*float64)(nil), 0},
		{"string (unsupported)", "hello", 0},
	}
	for _, tt := range tests {
		got := toFloat(tt.in)
		if got != tt.want {
			t.Errorf("toFloat(%s) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func intPtr(v int) *int             { return &v }
func int64Ptr(v int64) *int64       { return &v }
func float64Ptr(v float64) *float64 { return &v }

func TestSeq(t *testing.T) {
	got := seq(1, 5)
	if len(got) != 5 || got[0] != 1 || got[4] != 5 {
		t.Errorf("seq(1,5) = %v", got)
	}
	if seq(5, 1) != nil {
		t.Error("seq(5,1) should be nil")
	}
	got = seq(3, 3)
	if len(got) != 1 || got[0] != 3 {
		t.Errorf("seq(3,3) = %v", got)
	}
}

func TestDict(t *testing.T) {
	d := dict("a", 1, "b", "two")
	if d["a"] != 1 || d["b"] != "two" {
		t.Errorf("dict = %v", d)
	}
	if dict("a") != nil {
		t.Error("dict with odd args should be nil")
	}
	d = dict(123, "val")
	if len(d) != 0 {
		t.Error("dict with non-string key should skip that pair")
	}
}

func TestJsonMarshal(t *testing.T) {
	got := jsonMarshal(map[string]int{"a": 1})
	if !strings.Contains(string(got), `"a":1`) {
		t.Errorf("jsonMarshal = %q", got)
	}
	got = jsonMarshal(make(chan int))
	if string(got) != "null" {
		t.Errorf("jsonMarshal(chan) = %q, want null", got)
	}
}

func TestSafeHTML(t *testing.T) {
	got := safeHTML("<b>bold</b>")
	if got != template.HTML("<b>bold</b>") {
		t.Error("safeHTML mismatch")
	}
}

func TestSafeJS(t *testing.T) {
	got := safeJS("alert(1)")
	if got != template.JS("alert(1)") {
		t.Error("safeJS mismatch")
	}
}

func TestColorClass(t *testing.T) {
	if !strings.Contains(colorClass(1.0), "green") {
		t.Error("positive should be green")
	}
	if !strings.Contains(colorClass(-1.0), "red") {
		t.Error("negative should be red")
	}
	if !strings.Contains(colorClass(0), "gray") {
		t.Error("zero should be gray")
	}
}

func TestSuccessRateClasses(t *testing.T) {
	cases := []struct {
		v        float64
		wantText string // substring expected in text class
		wantBar  string // substring expected in bar class
	}{
		{100, "green", "green"},
		{90, "green", "green"},
		{89.9, "lime", "lime"},
		{80, "lime", "lime"},
		{79.9, "yellow", "yellow"},
		{72.6, "yellow", "yellow"}, // the user's reported value — must NOT be red
		{70, "yellow", "yellow"},
		{69.9, "orange", "orange"},
		{60, "orange", "orange"},
		{59.9, "red", "red"},
		{0, "red", "red"},
	}
	for _, tc := range cases {
		if got := successRateTextClass(tc.v); !strings.Contains(got, tc.wantText) {
			t.Errorf("successRateTextClass(%v) = %q, want substring %q", tc.v, got, tc.wantText)
		}
		if got := successRateBarClass(tc.v); !strings.Contains(got, tc.wantBar) {
			t.Errorf("successRateBarClass(%v) = %q, want substring %q", tc.v, got, tc.wantBar)
		}
	}
}

func TestPercentOf(t *testing.T) {
	if percentOf(25, 100) != 25 {
		t.Error("percentOf(25,100) should be 25")
	}
	if percentOf(10, 0) != 0 {
		t.Error("percentOf(10,0) should be 0")
	}
}

func TestPercentDiff(t *testing.T) {
	if percentDiff(110, 100) != 10 {
		t.Error("percentDiff(110,100) should be 10")
	}
	if percentDiff(50, 0) != 0 {
		t.Error("percentDiff(50,0) should be 0")
	}
}

func TestDeref(t *testing.T) {
	v := 3.14
	if deref(&v) != 3.14 {
		t.Error("deref non-nil should return value")
	}
	if deref(nil) != 0 {
		t.Error("deref nil should return 0")
	}
}

func TestIsNonNegative(t *testing.T) {
	if !isNonNegative(0) {
		t.Error("0 should be non-negative")
	}
	if !isNonNegative(1) {
		t.Error("1 should be non-negative")
	}
	if isNonNegative(-1) {
		t.Error("-1 should not be non-negative")
	}
}

// ============================================================
// getFuncMap — ensure all expected keys exist
// ============================================================

func TestGetFuncMap(t *testing.T) {
	fm := getFuncMap()
	expectedKeys := []string{
		"formatMoney", "formatDollars", "conversionSummary", "formatNumber", "formatPercent", "formatDate", "formatDateTime",
		"abs", "add", "sub", "mul", "div", "mod", "toFloat", "seq", "dict",
		"json", "toJSON", "lower", "upper", "title", "contains", "hasPrefix",
		"hasSuffix", "trimSpace", "split", "join", "safeHTML", "safeJS", "now",
		"isNegative", "isPositive", "isNonNegative", "colorClass", "percentOf",
		"percentDiff", "deref", "urlEncode",
	}
	for _, key := range expectedKeys {
		if fm[key] == nil {
			t.Errorf("getFuncMap missing key %q", key)
		}
	}
}

// ============================================================
// Inline function tests (isNegative, isPositive from getFuncMap)
// ============================================================

func TestInlineFuncs(t *testing.T) {
	fm := getFuncMap()
	isNeg := fm["isNegative"].(func(interface{}) bool)
	isPos := fm["isPositive"].(func(interface{}) bool)

	if !isNeg(-1.0) {
		t.Error("isNegative(-1) should be true")
	}
	if isNeg(1.0) {
		t.Error("isNegative(1) should be false")
	}
	if !isPos(1.0) {
		t.Error("isPositive(1) should be true")
	}
	if isPos(-1.0) {
		t.Error("isPositive(-1) should be false")
	}
}

// ============================================================
// extractLineNumber
// ============================================================

func TestExtractLineNumber(t *testing.T) {
	if extractLineNumber(`template: foo.html:42: unexpected`) != 42 {
		t.Error("should extract 42")
	}
	if extractLineNumber("no line number here") != 0 {
		t.Error("should return 0 for no match")
	}
}

// ============================================================
// formatTemplateError
// ============================================================

func TestFormatTemplateError(t *testing.T) {
	content := "line1\nline2\nline3\nline4\nline5\nline6\nline7"

	// With extractable line number
	errMsg := formatTemplateError("test.html", content, &fakeError{msg: "test.html:3: something wrong"})
	if !strings.Contains(errMsg, "Line: 3") {
		t.Errorf("expected Line: 3 in error, got %q", errMsg)
	}
	if !strings.Contains(errMsg, ">>>") {
		t.Error("expected >>> marker in context")
	}

	// Without extractable line number
	errMsg = formatTemplateError("test.html", content, &fakeError{msg: "generic error"})
	if strings.Contains(errMsg, "Line:") {
		t.Error("should not have Line: for generic error")
	}
	if !strings.Contains(errMsg, "generic error") {
		t.Error("should contain error message")
	}

	// Line near beginning of file (start < 0 guard)
	errMsg = formatTemplateError("test.html", "line1\nline2", &fakeError{msg: "test.html:1: err"})
	if !strings.Contains(errMsg, "Line: 1") {
		t.Error("should show line 1")
	}

	// Line near end of file (end > len guard)
	errMsg = formatTemplateError("test.html", "line1\nline2", &fakeError{msg: "test.html:2: err"})
	if !strings.Contains(errMsg, "Line: 2") {
		t.Error("should show line 2")
	}
}

type fakeError struct {
	msg string
}

func (e *fakeError) Error() string {
	return e.msg
}

// ============================================================
// Render infrastructure with in-memory FS
// ============================================================

func makeTestFS() fstest.MapFS {
	return fstest.MapFS{
		"layouts/base.html": &fstest.MapFile{
			Data: []byte(`{{define "base"}}<!DOCTYPE html><html>{{template "content" .}}</html>{{end}}`),
		},
		"pages/home.html": &fstest.MapFile{
			Data: []byte(`{{define "content"}}Hello {{.Name}}{{end}}`),
		},
		"partials/header.html": &fstest.MapFile{
			Data: []byte(`{{define "header"}}Header{{end}}`),
		},
		"components/widget.html": &fstest.MapFile{
			Data: []byte(`{{define "widget"}}Widget{{end}}`),
		},
	}
}

func TestNewFromFS_Success(t *testing.T) {
	r, err := NewFromFS(makeTestFS(), false)
	if err != nil {
		t.Fatalf("NewFromFS error: %v", err)
	}
	if r == nil {
		t.Fatal("renderer should not be nil")
	}
}

func TestNewFromFS_Debug(t *testing.T) {
	r, err := NewFromFS(makeTestFS(), true)
	if err != nil {
		t.Fatalf("NewFromFS error: %v", err)
	}
	if !r.debug {
		t.Error("debug should be true")
	}
}

func TestNew_NonexistentDir(t *testing.T) {
	_, err := New("/nonexistent/path/templates", false)
	if err == nil {
		t.Error("expected error for nonexistent dir")
	}
}

func TestNewFromFS_NoTemplates(t *testing.T) {
	emptyFS := fstest.MapFS{}
	_, err := NewFromFS(emptyFS, false)
	if err == nil {
		t.Error("expected error for empty FS")
	}
	if !strings.Contains(err.Error(), "no template files found") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestNewFromFS_ParseError(t *testing.T) {
	badFS := fstest.MapFS{
		"layouts/bad.html": &fstest.MapFile{
			Data: []byte(`{{define "bad"}}{{ .Foo | nonexistentFunc }}{{end}}`),
		},
	}
	_, err := NewFromFS(badFS, false)
	if err == nil {
		t.Error("expected error for bad template")
	}
	if !strings.Contains(err.Error(), "parsing failed") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestNewFromFS_UndefinedTemplateReference(t *testing.T) {
	badFS := fstest.MapFS{
		"layouts/base.html": &fstest.MapFile{
			Data: []byte(`{{define "base"}}{{template "nonexistent" .}}{{end}}`),
		},
	}
	_, err := NewFromFS(badFS, false)
	if err == nil {
		t.Error("expected error for undefined template reference")
	}
	if !strings.Contains(err.Error(), "undefined template reference") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestReload(t *testing.T) {
	r, err := NewFromFS(makeTestFS(), false)
	if err != nil {
		t.Fatalf("NewFromFS error: %v", err)
	}
	if err := r.Reload(); err != nil {
		t.Errorf("Reload error: %v", err)
	}
}

func TestRender(t *testing.T) {
	r, err := NewFromFS(makeTestFS(), false)
	if err != nil {
		t.Fatalf("NewFromFS error: %v", err)
	}

	w := httptest.NewRecorder()
	err = r.Render(w, "base", map[string]string{"Name": "World"})
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}

	body := w.Body.String()
	if !strings.Contains(body, "Hello World") {
		t.Errorf("Render output = %q, want 'Hello World'", body)
	}
	if w.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Error("wrong content type")
	}
}

func TestRender_DebugMode(t *testing.T) {
	r, err := NewFromFS(makeTestFS(), true)
	if err != nil {
		t.Fatalf("NewFromFS error: %v", err)
	}

	w := httptest.NewRecorder()
	err = r.Render(w, "base", map[string]string{"Name": "Debug"})
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	if !strings.Contains(w.Body.String(), "Hello Debug") {
		t.Error("debug render failed")
	}
}

func TestRender_BadTemplate(t *testing.T) {
	r, err := NewFromFS(makeTestFS(), false)
	if err != nil {
		t.Fatalf("NewFromFS error: %v", err)
	}

	w := httptest.NewRecorder()
	err = r.Render(w, "nonexistent", nil)
	if err == nil {
		t.Error("expected error for nonexistent template")
	}
}

func TestRenderPartial(t *testing.T) {
	r, err := NewFromFS(makeTestFS(), false)
	if err != nil {
		t.Fatalf("NewFromFS error: %v", err)
	}

	w := httptest.NewRecorder()
	err = r.RenderPartial(w, "header", nil)
	if err != nil {
		t.Fatalf("RenderPartial error: %v", err)
	}
	if !strings.Contains(w.Body.String(), "Header") {
		t.Error("partial should contain Header")
	}
}

func TestRenderPartial_DebugMode(t *testing.T) {
	r, err := NewFromFS(makeTestFS(), true)
	if err != nil {
		t.Fatalf("NewFromFS error: %v", err)
	}

	w := httptest.NewRecorder()
	err = r.RenderPartial(w, "header", nil)
	if err != nil {
		t.Fatalf("RenderPartial error: %v", err)
	}
}

func TestRenderPartial_BadTemplate(t *testing.T) {
	r, err := NewFromFS(makeTestFS(), false)
	if err != nil {
		t.Fatalf("NewFromFS error: %v", err)
	}

	w := httptest.NewRecorder()
	err = r.RenderPartial(w, "nonexistent", nil)
	if err == nil {
		t.Error("expected error for nonexistent partial")
	}
}

func TestRenderToString_Success(t *testing.T) {
	r, err := NewFromFS(makeTestFS(), false)
	if err != nil {
		t.Fatalf("NewFromFS error: %v", err)
	}

	result, err := r.RenderToString("widget", nil)
	if err != nil {
		t.Fatalf("RenderToString error: %v", err)
	}
	if result != "Widget" {
		t.Errorf("got %q, want Widget", result)
	}
}

func TestRenderToString_Error(t *testing.T) {
	r, err := NewFromFS(makeTestFS(), false)
	if err != nil {
		t.Fatalf("NewFromFS error: %v", err)
	}

	_, err = r.RenderToString("nonexistent", nil)
	if err == nil {
		t.Error("expected error for nonexistent template")
	}
}

func TestExecuteTemplate(t *testing.T) {
	r, err := NewFromFS(makeTestFS(), false)
	if err != nil {
		t.Fatalf("NewFromFS error: %v", err)
	}

	var buf strings.Builder
	err = r.ExecuteTemplate(&buf, "widget", nil)
	if err != nil {
		t.Fatalf("ExecuteTemplate error: %v", err)
	}
	if buf.String() != "Widget" {
		t.Errorf("got %q, want Widget", buf.String())
	}
}

// ============================================================
// loadTemplates with OS filesystem (New)
// ============================================================

func TestNew_WithTempDir(t *testing.T) {
	dir := t.TempDir()
	for _, sub := range []string{"layouts", "pages", "partials", "components"} {
		if err := os.MkdirAll(dir+"/"+sub, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	os.WriteFile(dir+"/layouts/base.html", []byte(`{{define "base"}}base{{end}}`), 0o644)
	os.WriteFile(dir+"/pages/index.html", []byte(`{{define "index"}}index{{end}}`), 0o644)
	os.WriteFile(dir+"/partials/nav.html", []byte(`{{define "nav"}}nav{{end}}`), 0o644)
	os.WriteFile(dir+"/components/btn.html", []byte(`{{define "btn"}}btn{{end}}`), 0o644)

	r, err := New(dir, false)
	if err != nil {
		t.Fatalf("New error: %v", err)
	}
	if r == nil {
		t.Fatal("renderer should not be nil")
	}
}

func TestNew_WithNestedComponents(t *testing.T) {
	dir := t.TempDir()
	for _, sub := range []string{"layouts", "pages", "partials", "components", "components/whatif"} {
		if err := os.MkdirAll(dir+"/"+sub, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	os.WriteFile(dir+"/layouts/base.html", []byte(`{{define "base"}}base{{end}}`), 0o644)
	os.WriteFile(dir+"/components/whatif/slider.html", []byte(`{{define "slider"}}slider{{end}}`), 0o644)

	r, err := New(dir, false)
	if err != nil {
		t.Fatalf("New error: %v", err)
	}

	var buf strings.Builder
	if err := r.ExecuteTemplate(&buf, "slider", nil); err != nil {
		t.Fatalf("ExecuteTemplate error: %v", err)
	}
	if buf.String() != "slider" {
		t.Errorf("got %q, want slider", buf.String())
	}
}

// ============================================================
// validateTemplateReferences with FS
// ============================================================

func TestValidateTemplateReferences_WithFS(t *testing.T) {
	goodFS := fstest.MapFS{
		"layouts/base.html": &fstest.MapFile{
			Data: []byte(`{{define "base"}}{{template "part" .}}{{end}}`),
		},
		"partials/part.html": &fstest.MapFile{
			Data: []byte(`{{define "part"}}part content{{end}}`),
		},
	}
	_, err := NewFromFS(goodFS, false)
	if err != nil {
		t.Errorf("valid references should not error: %v", err)
	}
}

// ============================================================
// loadTemplates edge: components/whatif via embedded FS
// ============================================================

func TestNewFromFS_WithWhatifSubdir(t *testing.T) {
	testFS := fstest.MapFS{
		"layouts/base.html": &fstest.MapFile{
			Data: []byte(`{{define "base"}}base{{end}}`),
		},
		"components/whatif/slider.html": &fstest.MapFile{
			Data: []byte(`{{define "slider"}}slider{{end}}`),
		},
	}
	r, err := NewFromFS(testFS, false)
	if err != nil {
		t.Fatalf("NewFromFS error: %v", err)
	}
	var buf strings.Builder
	if err := r.ExecuteTemplate(&buf, "slider", nil); err != nil {
		t.Fatalf("ExecuteTemplate error: %v", err)
	}
}

// ============================================================
// loadTemplates edge: read error for a file
// ============================================================

func TestLoadTemplates_ReadError(t *testing.T) {
	// Use a custom FS that fails ReadFile
	badFS := &readErrorFS{
		MapFS: fstest.MapFS{
			"layouts/test.html": &fstest.MapFile{Data: []byte(`{{define "t"}}t{{end}}`)},
		},
	}
	_, err := NewFromFS(badFS, false)
	if err == nil {
		t.Error("expected error when file read fails")
	}
}

type readErrorFS struct {
	fstest.MapFS
}

func (f *readErrorFS) Open(name string) (fs.File, error) {
	return f.MapFS.Open(name)
}

func (f *readErrorFS) Glob(pattern string) ([]string, error) {
	return fs.Glob(f.MapFS, pattern)
}

func (f *readErrorFS) ReadFile(name string) ([]byte, error) {
	if name == "layouts/test.html" {
		return nil, &fs.PathError{Op: "read", Path: name, Err: fs.ErrPermission}
	}
	return fs.ReadFile(f.MapFS, name)
}

// ============================================================
// Render/RenderPartial debug mode with broken FS (reload fails)
// ============================================================

func TestRender_DebugReloadError(t *testing.T) {
	// Start with a working FS, then swap to a broken one
	r, err := NewFromFS(makeTestFS(), true)
	if err != nil {
		t.Fatalf("NewFromFS error: %v", err)
	}
	// Break the FS so reload fails, but templates still exist from first load
	r.fsys = fstest.MapFS{} // empty FS => "no template files found"

	w := httptest.NewRecorder()
	// Render should still work (reload error is logged but not fatal)
	err = r.Render(w, "base", map[string]string{"Name": "World"})
	if err != nil {
		t.Fatalf("Render should succeed even if reload fails: %v", err)
	}
}

func TestRenderPartial_DebugReloadError(t *testing.T) {
	r, err := NewFromFS(makeTestFS(), true)
	if err != nil {
		t.Fatalf("NewFromFS error: %v", err)
	}
	r.fsys = fstest.MapFS{}

	w := httptest.NewRecorder()
	err = r.RenderPartial(w, "header", nil)
	if err != nil {
		t.Fatalf("RenderPartial should succeed even if reload fails: %v", err)
	}
}

// ============================================================
// validateTemplateReferences: file read error during validation
// ============================================================

func TestValidateTemplateReferences_ReadErrorSkipsFile(t *testing.T) {
	// Use a FS that succeeds on Glob and first ReadFile (for parsing)
	// but fails on the second ReadFile call (during validation).
	countFS := &failOnSecondReadFS{
		inner: fstest.MapFS{
			"layouts/base.html": &fstest.MapFile{
				Data: []byte(`{{define "base"}}base{{end}}`),
			},
		},
		readCount: make(map[string]int),
	}
	// This should succeed — the validation read failure is skipped via continue
	_, err := NewFromFS(countFS, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

type failOnSecondReadFS struct {
	inner     fstest.MapFS
	readCount map[string]int
}

func (f *failOnSecondReadFS) Open(name string) (fs.File, error) {
	return f.inner.Open(name)
}

func (f *failOnSecondReadFS) Glob(pattern string) ([]string, error) {
	return fs.Glob(f.inner, pattern)
}

func (f *failOnSecondReadFS) ReadFile(name string) ([]byte, error) {
	f.readCount[name]++
	if f.readCount[name] > 1 {
		return nil, &fs.PathError{Op: "read", Path: name, Err: fs.ErrPermission}
	}
	return fs.ReadFile(f.inner, name)
}

// ============================================================
// loadTemplates: glob error paths
// ============================================================

func TestLoadTemplates_GlobError_MainSubdir(t *testing.T) {
	badFS := &globErrorFS{
		failPattern: "layouts/*.html",
	}
	_, err := NewFromFS(badFS, false)
	if err == nil {
		t.Error("expected error for glob failure")
	}
	if !strings.Contains(err.Error(), "error globbing") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestLoadTemplates_GlobError_NestedSubdir(t *testing.T) {
	// Need to succeed for main subdirs but fail for components/whatif
	badFS := &globErrorFS{
		inner: fstest.MapFS{
			"layouts/base.html": &fstest.MapFile{
				Data: []byte(`{{define "base"}}base{{end}}`),
			},
		},
		failPattern: "components/whatif/*.html",
	}
	_, err := NewFromFS(badFS, false)
	if err == nil {
		t.Error("expected error for nested glob failure")
	}
	if !strings.Contains(err.Error(), "error globbing") {
		t.Errorf("unexpected error: %v", err)
	}
}

type globErrorFS struct {
	inner       fstest.MapFS
	failPattern string
}

func (f *globErrorFS) Open(name string) (fs.File, error) {
	if f.inner != nil {
		return f.inner.Open(name)
	}
	return nil, fs.ErrNotExist
}

func (f *globErrorFS) Glob(pattern string) ([]string, error) {
	if pattern == f.failPattern {
		return nil, fmt.Errorf("glob error")
	}
	if f.inner != nil {
		return fs.Glob(f.inner, pattern)
	}
	return nil, nil
}

func (f *globErrorFS) ReadFile(name string) ([]byte, error) {
	if f.inner != nil {
		return fs.ReadFile(f.inner, name)
	}
	return nil, fs.ErrNotExist
}

// ============================================================
// loadTemplates: OS filesystem ReadFile error
// ============================================================

func TestNew_WithUnreadableFile(t *testing.T) {
	dir := t.TempDir()
	for _, sub := range []string{"layouts", "pages", "partials", "components"} {
		os.MkdirAll(dir+"/"+sub, 0o755)
	}

	// Create a valid file first, then make one unreadable
	path := dir + "/layouts/base.html"
	os.WriteFile(path, []byte(`{{define "base"}}base{{end}}`), 0o644)
	unreadable := dir + "/pages/broken.html"
	os.WriteFile(unreadable, []byte(`content`), 0o000)

	_, err := New(dir, false)
	// Should get an error because the file can't be read
	if err == nil {
		// If running as root, file permissions won't matter
		t.Skip("test requires non-root execution")
	}
}
