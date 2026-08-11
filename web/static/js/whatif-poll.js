// Baseline for GET /whatif/poll. The server answers 204 when this matches its
// revision, which is the whole reason a 2s poll is affordable.
window.__whatifRevision = window.__whatifRevision || 0;

// Every response that changes the plan carries the new revision in an
// HX-Trigger header, which htmx raises as this event. It cannot ride in the
// body: responses swap into #whatif-results and never touch the polling
// element's attributes.
document.body.addEventListener('whatif:revision', function (evt) {
    var next = evt.detail;
    if (next && typeof next.value !== 'undefined') {
        next = next.value;
    }
    next = parseInt(next, 10);
    if (!isNaN(next)) {
        window.__whatifRevision = next;
    }
});

// True while a non-poll (user-initiated) request targeting #whatif-results is
// in flight. hx-sync="#whatif-results:drop" on the sentinel only guards
// poll-vs-poll overlap: htmx redirects the shared in-flight bucket to the
// hx-sync target only for the element that *declares* hx-sync, and the
// sentinel is the only element in the page that does -- none of the ~37
// mutating elements that target #whatif-results carry it. So a poll response
// can still land in the middle of a user mutation and race it. This flag is
// how that race is closed instead: from JS, not by touching every mutating
// element's template.
var mutationInFlight = false;

// A request is a "mutation against #whatif-results" if it targets that
// element and did not originate from the sentinel itself. The sentinel's own
// exclusion matters: without it, the poll's own request would set this flag
// and its own htmx:confirm check below would then suppress every following
// tick forever.
function isMutationAgainstResults(evt) {
    var elt = evt.detail && evt.detail.elt;
    var target = evt.detail && evt.detail.target;
    if (!elt || elt.id === 'whatif-poll') {
        return false;
    }
    return !!target && target.id === 'whatif-results';
}

document.body.addEventListener('htmx:beforeRequest', function (evt) {
    if (isMutationAgainstResults(evt)) {
        mutationInFlight = true;
    }
});

// htmx:afterRequest is dispatched on every XHR termination path -- onload
// (success), onerror, onabort, and ontimeout each call it before firing
// their own more specific event (htmx:sendError, htmx:sendAbort,
// htmx:timeout respectively) -- so it is the single reliable place to clear
// the flag. A flag stuck true would silently disable polling forever, which
// is worse than the race this guards against, so this listener is
// unconditional on outcome: it clears on success and on every failure mode.
document.body.addEventListener('htmx:afterRequest', function (evt) {
    if (isMutationAgainstResults(evt)) {
        mutationInFlight = false;
    }
});

// Do not swap the results column out from under someone who is typing in it or
// dragging a control, or whose mutation request is still in flight. The
// premise of this feature is that a human and the MCP touch the plan at the
// same time, so this is the normal case, not the edge.
document.body.addEventListener('htmx:confirm', function (evt) {
    if (!evt.detail || !evt.detail.elt || evt.detail.elt.id !== 'whatif-poll') {
        return;
    }
    if (mutationInFlight) {
        // Skip this tick; the next one is 2s away and will see the
        // mutation's own HX-Trigger-advanced baseline instead of racing it.
        evt.preventDefault();
        return;
    }
    var active = document.activeElement;
    if (!active || active === document.body) {
        return;
    }
    var interactive = active.matches('input, select, textarea, [contenteditable="true"]');
    // Containment matters as much as interactivity. The poll renders
    // #whatif-results with no out-of-band blocks, so it cannot touch anything
    // outside that element -- and document.activeElement does not reset when
    // the window loses focus, so an unscoped guard suppresses polling for as
    // long as a field anywhere on the page happens to hold focus. That breaks
    // the feature's primary workflow: drag a slider, alt-tab away, apply a
    // change from the MCP, come back to a page that never updates.
    //
    // document.hasFocus() closes that gap: focus left on a control inside
    // #whatif-results, followed by alt-tabbing away, must not suppress polling
    // for the rest of the session -- only while the document actually has
    // focus is the user plausibly still interacting with that control. Do not
    // drop this clause: without it, blur alone (no click elsewhere) restores
    // the indefinite-suppression bug this comment describes.
    var results = document.getElementById('whatif-results');
    if (interactive && results && results.contains(active) && document.hasFocus()) {
        // Skip this tick; the next one is 2s away and the baseline is unchanged.
        evt.preventDefault();
    }
});
