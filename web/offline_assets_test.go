package web

import (
	"io/fs"
	"path"
	"regexp"
	"strings"
	"testing"
)

// These tests guard the promise that the shipped binary renders correctly
// with no network at all. They run against EmbeddedFS rather than the working
// tree, so they assert on what a user actually gets.

// remoteRef matches a src=/href= pointing at another origin. Protocol-relative
// URLs ("//cdn.example.com/x.js") count: they resolve to a remote host too.
var remoteRef = regexp.MustCompile(`(?i)\b(?:src|href)\s*=\s*["'](?:https?:)?//`)

// No template may fetch a stylesheet or script from another origin. The
// specific regression this exists for: base.html and unlock.html loaded
// https://cdn.tailwindcss.com, which compiled the site's utilities in the
// browser. Offline — the only mode the README promises — that request failed
// and every page rendered as unstyled HTML. Tailwind is compiled ahead of
// time now (see `make css`) and served from /static.
//
// Anchor links to external documentation are fine and are not what this
// matches: it only looks at src/href on elements that block rendering. In
// practice that means <script> and <link>, which is why the check is scoped
// to lines carrying one of those tags.
func TestTemplatesLoadNoRemoteAssets(t *testing.T) {
	err := fs.WalkDir(EmbeddedFS, "templates", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || path.Ext(p) != ".html" {
			return err
		}
		b, err := fs.ReadFile(EmbeddedFS, p)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(b), "\n") {
			lower := strings.ToLower(line)
			if !strings.Contains(lower, "<script") && !strings.Contains(lower, "<link") {
				continue
			}
			if remoteRef.MatchString(line) {
				t.Errorf("%s:%d loads a render-blocking asset from another origin, which breaks offline use:\n\t%s",
					p, i+1, strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking templates: %v", err)
	}
}

// The compiled stylesheet has to be present in the binary, not just on the
// build machine. A .gitignore entry or a missing `make css` would otherwise
// ship an empty /static/css/tailwind.css and reproduce the unstyled page the
// CDN removal was meant to fix.
func TestTailwindStylesheetIsEmbedded(t *testing.T) {
	b, err := fs.ReadFile(EmbeddedFS, "static/css/tailwind.css")
	if err != nil {
		t.Fatalf("compiled stylesheet is not embedded: %v", err)
	}
	if len(b) < 10_000 {
		t.Errorf("static/css/tailwind.css is %d bytes; expected a full build (>10KB)", len(b))
	}
	// Preflight is Tailwind's base layer. Its absence means the file was
	// built from something other than tailwind.src.css.
	if !strings.Contains(string(b), "box-sizing:border-box") {
		t.Error("static/css/tailwind.css does not contain Tailwind's base layer")
	}
}

// A pre-compiled stylesheet only contains the classes the build could find.
// The CDN needed no such list — it read the finished DOM — so the content
// globs in tailwind.config.js are the fragile part of this design: drop one
// and the pages it covered lose their styling with nothing failing to build.
//
// Each canary below is a class that reaches the browser from a different
// source, and so is only in the output if the glob covering that source is
// still there. Verified by deleting each glob in turn: every deletion fails
// at least one of these.
func TestTailwindBuildCoversEveryClassSource(t *testing.T) {
	b, err := fs.ReadFile(EmbeddedFS, "static/css/tailwind.css")
	if err != nil {
		t.Fatalf("reading stylesheet: %v", err)
	}
	css := string(b)

	for _, c := range []struct {
		selector string
		source   string
	}{
		// './web/templates/**/*.html' — plain markup.
		{".bg-gray-100", "class attributes in web/templates"},
		// './web/static/js/**/*.js' — whatif-tabs.js toggles this on the
		// results column when every card is collapsed. It appears in no
		// template, so only the JS glob can put it in the build.
		{`.lg\:col-span-5`, "classList calls in web/static/js"},
		// './internal/**/*.go' — successRateTextClass and verdictClassesFor
		// in internal/templates/render.go return these to the templates as
		// opaque strings, so only the Go glob can put them in the build.
		{".text-lime-600", "class strings returned by Go helpers in internal/"},
		{".border-rose-300", "class strings returned by Go helpers in internal/"},
	} {
		if !strings.Contains(css, c.selector) {
			t.Errorf("%s missing from the build: the tailwind.config.js content glob for %s is not matching", c.selector, c.source)
		}
	}
}
