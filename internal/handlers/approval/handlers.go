// Package approval serves the page a human answers when an MCP tool asks
// permission for something destructive.
//
// It is deliberately the app's own page rather than a prompt inside whatever
// client is driving the model: the person reads the consequences in the
// application they already trust, and clicks a button there. That is the one
// approval path that does not depend on the model's client being honest about
// what it showed them.
package approval

import (
	"fmt"
	"html/template"
	"net/http"
	"strings"

	"budget2/internal/services/mcpsvc/confirm"

	"github.com/go-chi/chi/v5"
)

// approvals holds the requests this handler answers. Nil until Initialize,
// which is a supported state: the page then reports that no approval is
// pending rather than panicking.
var approvals *confirm.Approvals

// Initialize wires the handler to the registry the MCP tools file their
// requests in. It must be the SAME instance -- two registries would mean a
// human approving a request no tool is waiting on.
func Initialize(a *confirm.Approvals) { approvals = a }

// RegisterPublicRoutes registers the approval page.
//
// PUBLIC, alongside /unlock and /killme, for two reasons. A guarded operation
// can need approving while storage is locked (shutdown_server certainly can),
// and the lock-check middleware would redirect the page to the unlock screen
// exactly when it is needed. The capability is the approval ID itself: 32
// random bytes, single-use, expiring, and never logged. Same shape as the
// confirm token, and the same reasoning as /killme being public.
func RegisterPublicRoutes(r chi.Router) {
	r.Get("/mcp/approve/{id}", HandleShow)
	r.Post("/mcp/approve/{id}", HandleDecide)
}

const pageCSS = `
:root { color-scheme: light dark; }
body { font: 16px/1.5 system-ui, -apple-system, "Segoe UI", sans-serif;
       max-width: 46rem; margin: 3rem auto; padding: 0 1.25rem; }
.tool { font: 0.8rem/1 ui-monospace, SFMono-Regular, Menlo, monospace;
        letter-spacing: .08em; text-transform: uppercase; opacity: .7; }
h1 { font-size: 1.5rem; margin: .4rem 0 1rem; }
.detail { border-left: 4px solid currentColor; padding: .75rem 1rem; margin: 0 0 1.5rem;
          opacity: .95; background: rgba(128,128,128,.09); border-radius: 0 6px 6px 0; }
form { display: flex; gap: .75rem; align-items: center; }
button { font: inherit; padding: .6rem 1.25rem; border-radius: 6px; cursor: pointer;
         border: 1px solid rgba(128,128,128,.5); background: transparent; color: inherit; }
/* The SAFE action is the prominent one. On a page whose whole purpose is an
   irreversible choice, the emphasis must not be doing the persuading: a
   person clicking the obvious button should end up not losing their data. */
button.primary { border-color: #2563eb; background: #2563eb; color: #fff; font-weight: 600; }
button.danger { color: #b3261e; border-color: rgba(179,38,30,.55); font-weight: 500; }
@media (prefers-color-scheme: dark) {
  button.primary { border-color: #3b82f6; background: #3b82f6; }
  button.danger { color: #f2686c; border-color: rgba(242,104,108,.55); }
}
.note { margin-top: 2rem; font-size: .9rem; opacity: .75; }
`

var showTmpl = template.Must(template.New("approve").Parse(`<!doctype html>
<meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Approve: {{.Tool}} — budget2</title>
<style>` + pageCSS + `</style>
<p class="tool">{{.Tool}} is asking permission</p>
<h1>{{.Title}}</h1>
<p class="detail">{{.Detail}}</p>
<form method="post">
  <button type="submit" name="decision" value="decline" class="primary">No, cancel</button>
  <button type="submit" name="decision" value="approve" class="danger">Yes, do it</button>
</form>
<p class="note">Nothing has happened yet. Closing this page without choosing is the same
as declining — the request expires on its own and the tool is told nobody answered.</p>
`))

var doneTmpl = template.Must(template.New("done").Parse(`<!doctype html>
<meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Heading}} — budget2</title>
<style>` + pageCSS + `</style>
<h1>{{.Heading}}</h1>
<p class="detail">{{.Body}}</p>
<p class="note">You can close this tab.</p>
`))

// HandleShow renders the request for a human to read.
func HandleShow(w http.ResponseWriter, r *http.Request) {
	p, ok := lookup(r)
	if !ok {
		gone(w)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// No caching: an answered or expired request must not be re-served from a
	// back button as though it were still open.
	w.Header().Set("Cache-Control", "no-store")
	_ = showTmpl.Execute(w, p)
}

// HandleDecide records the answer and tells the person what they just did.
func HandleDecide(w http.ResponseWriter, r *http.Request) {
	p, ok := lookup(r)
	if !ok {
		gone(w)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "could not read the form", http.StatusBadRequest)
		return
	}

	// Anything that is not an explicit approval is a refusal, including a
	// missing or unexpected value. The default has to be no.
	decision := confirm.Refused
	if strings.TrimSpace(r.PostFormValue("decision")) == "approve" {
		decision = confirm.Approved
	}

	if err := approvals.Decide(p.ID, decision); err != nil {
		gone(w)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	data := struct{ Heading, Body string }{
		Heading: "Declined",
		Body:    fmt.Sprintf("%s was told you said no. Nothing was changed.", p.Tool),
	}
	if decision == confirm.Approved {
		data = struct{ Heading, Body string }{
			Heading: "Approved",
			Body:    fmt.Sprintf("%s is going ahead now. Watch the session you started this from for the result.", p.Tool),
		}
	}
	_ = doneTmpl.Execute(w, data)
}

func lookup(r *http.Request) (*confirm.Pending, bool) {
	if approvals == nil {
		return nil, false
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		return nil, false
	}
	return approvals.Get(id)
}

// gone is the one answer for unknown, expired and already-answered requests.
// Telling them apart would only help someone guessing at IDs, and the person
// who legitimately clicked twice needs the same sentence either way.
func gone(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNotFound)
	_ = doneTmpl.Execute(w, struct{ Heading, Body string }{
		Heading: "Nothing to approve",
		Body: "This request is not open. It may have already been answered, or it may have expired — " +
			"requests are short-lived on purpose. Nothing has been changed. Ask again in the session you " +
			"started from if you still want it.",
	})
}
