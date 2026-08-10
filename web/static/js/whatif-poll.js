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

// Do not swap the results column out from under someone who is typing in it or
// dragging a control. The premise of this feature is that a human and the MCP
// touch the plan at the same time, so this is the normal case, not the edge.
document.body.addEventListener('htmx:confirm', function (evt) {
    if (!evt.detail || !evt.detail.elt || evt.detail.elt.id !== 'whatif-poll') {
        return;
    }
    var active = document.activeElement;
    if (!active || active === document.body) {
        return;
    }
    var interactive = active.matches('input, select, textarea, [contenteditable="true"]');
    if (interactive) {
        // Skip this tick; the next one is 2s away and the baseline is unchanged.
        evt.preventDefault();
    }
});
