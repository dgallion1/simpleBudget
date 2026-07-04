package testutil

import (
	"net/http"
	"net/http/httptest"
	"strings"
)

// BadFormRequest builds a request whose body makes r.ParseForm() fail.
// Only invalid percent-encoding ("%ZZ") triggers the error: a multipart
// body without a boundary is silently accepted by urlencoded parsing in
// modern Go, so it does NOT work for this purpose.
func BadFormRequest(method, path string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader("foo=%ZZ"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}
