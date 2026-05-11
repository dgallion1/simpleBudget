package testutil

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestResponseAssertionSuccessChainReadsBodyOnce(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"application/json; charset=utf-8"},
		},
		Body: io.NopCloser(strings.NewReader(`{"status":"ok","message":"done"}`)),
	}

	assertion := AssertResponse(t, resp).
		StatusOK().
		ContentTypeJSON().
		Contains("status").
		ContainsAll("ok", "done")

	if !assertion.bodyRead {
		t.Fatal("expected body to be read")
	}
	if got := assertion.readBody(); got != `{"status":"ok","message":"done"}` {
		t.Fatalf("cached body got %q", got)
	}
}

func TestResponseAssertionHTMLAndTruncate(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusCreated,
		Header: http.Header{
			"Content-Type": []string{"text/html"},
		},
		Body: io.NopCloser(strings.NewReader("<main>created</main>")),
	}

	AssertResponse(t, resp).
		Status(http.StatusCreated).
		ContentTypeHTML().
		Contains("created")

	if got := truncate("short", 10); got != "short" {
		t.Fatalf("truncate short got %q", got)
	}
	if got := truncate("abcdefghijklmnopqrstuvwxyz", 5); got != "abcde..." {
		t.Fatalf("truncate long got %q", got)
	}
}
