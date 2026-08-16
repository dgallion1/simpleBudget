package main

import "testing"

// TestMCPBaseURL pins where browser-facing links from the MCP server point.
// They are followed by a human's browser, which need not be on this machine,
// so a configured public origin has to win over anything derived from the
// listen address.
func TestMCPBaseURL(t *testing.T) {
	tests := []struct {
		name          string
		publicBaseURL string
		listenAddr    string
		want          string
	}{
		{
			name:          "configured origin wins over the listen address",
			publicBaseURL: "https://budget.example.net",
			listenAddr:    ":8080",
			want:          "https://budget.example.net",
		},
		{
			name:          "configured origin keeps a non-default port",
			publicBaseURL: "http://192.168.1.10:8080",
			listenAddr:    ":8080",
			want:          "http://192.168.1.10:8080",
		},
		{
			name:          "configured origin is trimmed, not re-derived",
			publicBaseURL: "  https://budget.example.net/  ",
			listenAddr:    ":8080",
			want:          "https://budget.example.net",
		},
		{
			name:       "wildcard bind names no host, so localhost",
			listenAddr: ":8080",
			want:       "http://localhost:8080",
		},
		{
			name:       "explicit 0.0.0.0 is still a wildcard",
			listenAddr: "0.0.0.0:8080",
			want:       "http://localhost:8080",
		},
		{
			name:       "IPv6 wildcard is still a wildcard",
			listenAddr: "[::]:8080",
			want:       "http://localhost:8080",
		},
		{
			// The only evidence the process has about how it is addressed.
			name:       "a concrete host is kept",
			listenAddr: "192.168.1.10:8080",
			want:       "http://192.168.1.10:8080",
		},
		{
			name:       "loopback is kept",
			listenAddr: "127.0.0.1:8080",
			want:       "http://127.0.0.1:8080",
		},
		{
			name:       "unparseable listen address falls back rather than emitting a malformed URL",
			listenAddr: "8080",
			want:       "http://localhost8080",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mcpBaseURL(tt.publicBaseURL, tt.listenAddr); got != tt.want {
				t.Errorf("mcpBaseURL(%q, %q) = %q, want %q",
					tt.publicBaseURL, tt.listenAddr, got, tt.want)
			}
		})
	}
}

// TestStartupBaseURLWarning covers the case the URL derivation cannot fix on
// its own: a wildcard bind accepts remote connections, but nothing in the
// process knows which of its addresses a remote browser can reach. The
// approval link then resolves to the user's own machine and the guarded
// operation times out with nothing on screen to explain it, so startup says so.
func TestStartupBaseURLWarning(t *testing.T) {
	tests := []struct {
		name          string
		publicBaseURL string
		listenAddr    string
		wantWarning   bool
	}{
		{
			name:        "wildcard bind with no configured origin warns",
			listenAddr:  ":8080",
			wantWarning: true,
		},
		{
			name:        "explicit 0.0.0.0 warns",
			listenAddr:  "0.0.0.0:8080",
			wantWarning: true,
		},
		{
			name:          "a configured origin is the fix, so no warning",
			publicBaseURL: "https://budget.example.net",
			listenAddr:    ":8080",
		},
		{
			name:       "loopback bind cannot be reached remotely anyway",
			listenAddr: "127.0.0.1:8080",
		},
		{
			name:       "a concrete host produces usable links",
			listenAddr: "192.168.1.10:8080",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := startupBaseURLWarning(tt.publicBaseURL, tt.listenAddr)
			if (got != "") != tt.wantWarning {
				t.Errorf("startupBaseURLWarning(%q, %q) = %q, want warning=%v",
					tt.publicBaseURL, tt.listenAddr, got, tt.wantWarning)
			}
		})
	}
}
