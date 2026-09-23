// Base layout: HTMX request/swap wiring (scroll-position preservation on
// #whatif-results swaps, error display, error logging), theme-toggle
// buttons, mobile nav toggle. Extracted from layouts/base.html (U7).

        (function () {
            // Configure HTMX
            document.body.addEventListener('htmx:configRequest', function (evt) {
                // Add any custom headers here if needed
            });

            // Browser-invalid values (e.g. a number below its min) otherwise make
            // htmx silently halt the request with nothing shown -- see the
            // vendored htmx.min.js validation pass (an()), which calls
            // elt.reportValidity() for the first invalid field ONLY when this
            // flag is set (it defaults to false). This surfaces the browser's
            // own validation message instead (WS4 AC3).
            if (window.htmx) {
                htmx.config.reportValidityOfForms = true;
            }

            // Prevent scroll-jump when swapping #whatif-results (slider changes, etc.)
            (function () {
                var savedScroll = null;
                document.body.addEventListener('htmx:beforeSwap', function (evt) {
                    if (evt.detail.xhr && evt.detail.xhr.status >= 400 && evt.detail.xhr.status < 500 &&
                        evt.detail.xhr.getResponseHeader('HX-Retarget')) {
                        evt.detail.shouldSwap = true;
                        // Deliberately NOT touching isError here (it stays
                        // whatever htmx's default response-handling table
                        // computed for a 4xx -- true). shouldSwap alone decides
                        // whether the error fragment lands in its HX-Retarget
                        // slot (independent of isError -- see Vn()'s
                        // `if (m.shouldSwap) {...}` in the vendored
                        // htmx.min.js). isError instead flows into
                        // afterRequest's detail.successful
                        // (`e.successful = !isError`), which the Add forms'
                        // own hx-on::after-request="if(event.detail.successful)
                        // this.reset()" reads to decide whether to wipe what
                        // the user typed. Forcing isError false here made a
                        // REJECTED add report success, resetting the form and
                        // discarding the input underneath the error message it
                        // had just swapped in (WS4 AC1).
                    }
                    if (evt.detail.target && evt.detail.target.id === 'whatif-results') {
                        savedScroll = window.scrollY || document.documentElement.scrollTop;
                    }
                });
                document.body.addEventListener('htmx:afterSwap', function (evt) {
                    if (savedScroll !== null && evt.detail.target && evt.detail.target.id === 'whatif-results') {
                        window.scrollTo({ top: savedScroll, behavior: 'instant' });
                        savedScroll = null;
                    }
                });
            })();

            // Non-retargeted /whatif errors: htmx 2's default response-handling
            // table marks a bare 4xx/5xx {swap:false, error:true} (see the
            // vendored htmx.min.js's Bn()), so a rejected /whatif request the
            // server did NOT explicitly retarget (the block above is the only
            // case that reaches the DOM by itself) never shows anything but the
            // console.error below. Most /whatif forms have no dedicated error
            // slot -- only the five Add forms do, via renderRetargetedError's
            // HX-Retarget header. This creates one next to whichever form (or
            // form-less control) sent the request, reusing the server's
            // renderError fragment verbatim (which carries role="alert" itself
            // -- WS4 AC4), and removes it the next time that same host
            // succeeds (WS4 AC2).
            //
            // Scoped to requests a USER actually initiated (lead ruling,
            // WS4.2): a <form>, something inside one, or an interactive
            // control (button/select/input/textarea) with its own hx-verb.
            // Everything else -- hx-trigger="every"/"load"/"revealed"/
            // "intersect" sentinels like the hidden #whatif-poll change
            // detector, and htmx.ajax() calls with no source element (which
            // default to document.body, e.g. whatif-spending-phases.js's
            // trajectory refresh) -- keeps the pre-WS4 console.error-only
            // handling: none of those is a place a user was looking, and the
            // poll in particular would otherwise re-insert (and re-announce)
            // a node every 2s. errorHost() also refuses <html>/<body>/<main>/
            // the page layout grid as a last-resort host even for an
            // eligible trigger, so a future form-less control can never turn
            // an error into an extra, unstyled top-level or grid child.
            (function () {
                var MARK = 'data-wf-inline-error';
                var HX_VERB_ATTRS = ['hx-get', 'hx-post', 'hx-put', 'hx-patch', 'hx-delete'];
                var INTERACTIVE_TAGS = {BUTTON: true, SELECT: true, INPUT: true, TEXTAREA: true};
                var FORBIDDEN_TAGS = {HTML: true, BODY: true, MAIN: true};

                function isWhatIfPath(evt) {
                    var path = evt.detail && evt.detail.pathInfo && evt.detail.pathInfo.requestPath;
                    return typeof path === 'string' && path.indexOf('/whatif') === 0;
                }
                function nearestForm(elt) {
                    var n = elt;
                    while (n) {
                        if (n.tagName === 'FORM') return n;
                        n = n.parentNode;
                    }
                    return null;
                }
                function hasOwnHxVerb(elt) {
                    for (var i = 0; i < HX_VERB_ATTRS.length; i++) {
                        if (elt.hasAttribute && elt.hasAttribute(HX_VERB_ATTRS[i])) return true;
                    }
                    return false;
                }
                function isForbiddenHost(host) {
                    if (!host || !host.tagName) return true;
                    if (FORBIDDEN_TAGS[host.tagName]) return true;
                    if (host.hasAttribute && host.hasAttribute('data-wf-layout-grid')) return true;
                    return false;
                }
                // null when `elt` is not something a user directly interacted
                // with (see the block comment above). Otherwise the nearest
                // enclosing <form>, or -- for an interactive control with no
                // form, like the Delete button on an ended-schedule income/
                // expense entry -- its own parent, so the message still lands
                // next to it (both verified live by checker-a11y/checker-tests
                // on WS4.1).
                function errorHost(elt) {
                    if (!elt || !elt.tagName) return null;
                    var form = nearestForm(elt);
                    var host = form;
                    if (!host && INTERACTIVE_TAGS[elt.tagName] && hasOwnHxVerb(elt)) {
                        host = elt.parentNode;
                    }
                    if (!host) return null;
                    return isForbiddenHost(host) ? null : host;
                }
                function findMarked(host) {
                    var children = host.children || [];
                    for (var i = 0; i < children.length; i++) {
                        if (children[i].hasAttribute && children[i].hasAttribute(MARK)) return children[i];
                    }
                    return null;
                }
                // Parses renderError's own fragment (a single element,
                // already carrying its own role="alert") out of `text` when
                // possible. A non-HTML error body -- e.g. a plain-text
                // http.Error from chi's panic Recoverer, never renderError's
                // own output -- has no element to reuse, so this falls back
                // to a plain wrapper WE control and shows the text safely via
                // textContent (never innerHTML of an unknown body; checker-
                // tests O2 -- the naive version threw on a Text node here).
                function buildErrorNode(text) {
                    var wrap = document.createElement('div');
                    wrap.innerHTML = text;
                    var node = wrap.firstChild;
                    if (node && node.nodeType === 1 && typeof node.setAttribute === 'function') {
                        return node;
                    }
                    var fallback = document.createElement('div');
                    fallback.setAttribute('role', 'alert');
                    fallback.className = 'p-4 bg-negative-soft border border-negative rounded-lg text-body-sm text-negative';
                    fallback.textContent = text || 'Something went wrong.';
                    return fallback;
                }
                document.body.addEventListener('htmx:responseError', function (evt) {
                    if (!isWhatIfPath(evt) || !evt.detail.elt) return;
                    var xhr = evt.detail.xhr;
                    // A retargeted 4xx is handled by the beforeSwap listener
                    // above; it already swapped the message into its own
                    // HX-Retarget slot -- do not show it a second time here.
                    if (xhr && xhr.getResponseHeader('HX-Retarget')) return;
                    var host = errorHost(evt.detail.elt);
                    if (!host) return;
                    var text = (xhr && xhr.responseText) || '';
                    var existing = findMarked(host);
                    // The exact same message is already showing on this host
                    // -- leave it alone rather than tear it down and put back
                    // an identical node, which would re-announce it to AT for
                    // no change a user can perceive.
                    if (existing && existing.__wfText === text) return;
                    if (existing) existing.remove();
                    var node = buildErrorNode(text);
                    node.setAttribute(MARK, '');
                    node.__wfText = text;
                    host.insertBefore(node, host.firstChild);
                });
                document.body.addEventListener('htmx:afterRequest', function (evt) {
                    if (!isWhatIfPath(evt) || !evt.detail.successful || !evt.detail.elt) return;
                    var host = errorHost(evt.detail.elt);
                    if (!host) return;
                    var existing = findMarked(host);
                    if (existing) existing.remove();
                });
            })();

            // Handle HTMX errors
            document.body.addEventListener('htmx:responseError', function (evt) {
                console.error('HTMX error:', evt.detail);
            });

            // Theme toggle functionality
            ['theme-toggle', 'theme-toggle-mobile'].forEach(function (id) {
                var btn = document.getElementById(id);
                if (!btn || btn._listenerAttached) return;
                btn._listenerAttached = true;
                btn.addEventListener('click', function () {
                    var html = document.documentElement;
                    var isDark = html.classList.contains('dark');
                    if (isDark) {
                        html.classList.remove('dark');
                        html.classList.add('light');
                        localStorage.setItem('theme', 'light');
                    } else {
                        html.classList.remove('light');
                        html.classList.add('dark');
                        localStorage.setItem('theme', 'dark');
                    }
                    window.dispatchEvent(new CustomEvent('themechange', { detail: { dark: !isDark } }));
                });
            });

            var navToggle = document.getElementById('mobile-nav-toggle');
            var mobileNav = document.getElementById('mobile-nav');
            if (navToggle && mobileNav && !navToggle._listenerAttached) {
                navToggle._listenerAttached = true;
                navToggle.addEventListener('click', function () {
                    var open = mobileNav.classList.toggle('hidden') === false;
                    navToggle.setAttribute('aria-expanded', open ? 'true' : 'false');
                });
            }
        })();
