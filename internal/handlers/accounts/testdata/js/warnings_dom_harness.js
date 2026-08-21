'use strict';

// Self-contained regression harness for the <script> block that ships
// inside web/templates/pages/accounts.html (the syncWarnings() IIFE).
//
// This file is invoked from Go (see warnings_client_regression_test.go),
// which extracts the LIVE <script>...</script> body straight out of the
// real template on every run and writes it to a temp file passed as
// argv[2]. There is no copy of that JS pasted in here -- only a minimal
// stand-in for the handful of DOM / Web Storage APIs the script actually
// touches, plus the sequences that ACCESSIBILITY.md point 16 requires.
//
// No npm, no network, no third-party modules: this runs under a bare
// `node` binary. See internal/handlers/accounts/warnings_client_regression_test.go
// for how it is wired into `go test` -- that same test file is also why a
// machine without node cannot silently skip it: TestSyncWarnings_ClientRegressionHarness
// fails (t.Fatal), not skips, when `node` is missing from PATH, unless
// BUDGET2_ALLOW_SKIP_JS is set. The guard lives in the test itself, not in
// any Makefile target.
//
// ---------------------------------------------------------------------
// Minimal DOM / sessionStorage stand-in
// ---------------------------------------------------------------------
//
// Deliberately NOT a general-purpose DOM: only the exact surface
// syncWarnings() calls (getElementById, getAttribute/setAttribute,
// textContent, remove, querySelector('[data-dismiss-warnings]'),
// addEventListener('click', ...), plus document.body's
// addEventListener('htmx:afterSwap', ...) and the evt.detail.target shape
// it reads). Anything the real script doesn't call, this stub doesn't
// implement.

function makeEnv() {
  const elements = new Map();
  const bodyListeners = Object.create(null);
  // Instrumentation: counts writes to #accounts-warnings-region's
  // textContent, independent of what value was written. This is what
  // the "guard directions" checks below assert on.
  const writes = { region: 0 };

  function makeElement(id) {
    const el = {
      id,
      _attrs: Object.create(null),
      _text: '',
      _clickHandlers: [],
      _dismissBtn: null,
      getAttribute(name) {
        return Object.prototype.hasOwnProperty.call(this._attrs, name) ? this._attrs[name] : null;
      },
      setAttribute(name, value) {
        this._attrs[name] = String(value);
      },
      addEventListener(type, handler) {
        if (type === 'click') this._clickHandlers.push(handler);
      },
      // Not a real DOM event dispatch -- just invokes whatever click
      // handlers syncWarnings() registered, the same thing a real click
      // would trigger.
      dispatchClick() {
        this._clickHandlers.slice().forEach((h) => h.call(el, { type: 'click' }));
      },
      remove() {
        elements.delete(id);
      },
      querySelector(sel) {
        if (sel === '[data-dismiss-warnings]') return this._dismissBtn;
        return null;
      },
      focus() {},
      get textContent() {
        return this._text;
      },
      set textContent(v) {
        this._text = v;
        if (id === 'accounts-warnings-region') writes.region += 1;
      },
    };
    return el;
  }

  const document = {
    getElementById(id) {
      return elements.has(id) ? elements.get(id) : null;
    },
    body: {
      addEventListener(type, handler) {
        (bodyListeners[type] = bodyListeners[type] || []).push(handler);
      },
    },
  };

  function fireAfterSwap() {
    const evt = {
      detail: {
        target: {
          id: 'accounts-list',
          // The real handler also probes for [data-focus-target],
          // [aria-invalid="true"] and #accounts-list-heading for focus
          // management; none of that is in scope for the warnings
          // regression checks here, so it always reports "not found".
          querySelector() {
            return null;
          },
        },
      },
    };
    (bodyListeners['htmx:afterSwap'] || []).forEach((h) => h(evt));
  }

  return { document, elements, makeElement, writes, fireAfterSwap };
}

function makeSessionStorage() {
  const store = new Map();
  return {
    getItem(k) {
      return store.has(k) ? store.get(k) : null;
    },
    setItem(k, v) {
      store.set(k, String(v));
    },
    removeItem(k) {
      store.delete(k);
    },
  };
}

// Renders the data carrier (#accounts-warnings-data) with the given
// warning text/key -- present on every response, per the template's own
// comment on that span, warnings or not.
function renderData(env, text, key) {
  const data = env.makeElement('accounts-warnings-data');
  data.setAttribute('data-warnings-text', text);
  data.setAttribute('data-warnings-key', key);
  env.elements.set('accounts-warnings-data', data);
}

function renderBanner(env, key) {
  const banner = env.makeElement('accounts-warnings-banner');
  const dismissBtn = env.makeElement('dismiss-btn-' + key);
  banner._dismissBtn = dismissBtn;
  env.elements.set('accounts-warnings-banner', banner);
}

// A full page load (or reload): #accounts-warnings-region is created here
// and only here -- it lives outside the HTMX swap target and a real
// browser never replaces it for the lifetime of the page. Its initial
// text is whatever the server unconditionally renders (the region's
// {{if .Warnings}}...{{end}} in the template has no client-side gate),
// so it starts equal to `text` regardless of any prior client-only
// dismissal -- the server has no notion of that.
function renderInitial(env, text, key, hasBanner) {
  const region = env.makeElement('accounts-warnings-region');
  region._text = text;
  env.elements.set('accounts-warnings-region', region);
  renderData(env, text, key);
  if (hasBanner) renderBanner(env, key);
}

// Simulates one HTMX innerHTML swap of #accounts-list: the data carrier
// and (if present) the banner are fresh DOM nodes, exactly like a real
// server response replaces them. The region is deliberately left alone --
// it is outside the swap target -- and htmx:afterSwap is fired afterward,
// same as the real integration.
function renderSwap(env, text, key, hasBanner) {
  env.elements.delete('accounts-warnings-banner');
  renderData(env, text, key);
  if (hasBanner) renderBanner(env, key);
  env.fireAfterSwap();
}

// Evaluates the extracted <script> body fresh, as a full page load would:
// its top-level `var`s (region, lastWarningsText, ...) get a brand-new
// closure each time this is called, exactly like a real page evaluating
// the same <script> tag again after a reload. `document` and
// `sessionStorage` are passed as function parameters so the extracted
// source's bare references to those globals resolve to our stubs without
// needing a real global object or vm sandbox.
function runPageLoad(scriptSource, env, sessionStorage) {
  const fn = new Function('document', 'sessionStorage', scriptSource);
  fn(env.document, sessionStorage);
}

// ---------------------------------------------------------------------
// The sequences ACCESSIBILITY.md point 16 requires
// ---------------------------------------------------------------------

const WARNING_TEXT_1 = 'Pattern overlap warning: usaa-checking.csv overlaps with schwab-checking.csv (basename usaa-checking)';
const WARNING_KEY_1 = 'k-overlap-usaa-schwab';
const WARNING_TEXT_2 = 'Pattern overlap warning: usaa-checking.csv overlaps with chase-checking.csv (basename usaa-checking)';
const WARNING_KEY_2 = 'k-overlap-usaa-chase';

// 1. Dismiss -> resolve the overlap -> recreate the SAME overlap.
// Required: banner present and its dismiss control present, and the live
// region's content matching what the banner shows (parity, not exact
// wording -- a copy edit to either string must not flip this test).
function testDismissResolveRecreate(scriptSource) {
  const env = makeEnv();
  const sessionStorage = makeSessionStorage();

  renderInitial(env, WARNING_TEXT_1, WARNING_KEY_1, true);
  runPageLoad(scriptSource, env, sessionStorage);

  const banner0 = env.document.getElementById('accounts-warnings-banner');
  const dismissBtn0 = banner0 && banner0.querySelector('[data-dismiss-warnings]');
  if (dismissBtn0) dismissBtn0.dispatchClick();

  // Dismissal must actually have taken effect for the rest of this
  // sequence to mean anything -- otherwise a script that does nothing at
  // all could coast through on the fact that a fresh server render is
  // already internally consistent.
  const postDismissBannerAbsent = env.document.getElementById('accounts-warnings-banner') === null;
  const postDismissStored = sessionStorage.getItem('accounts-warnings-dismissed') === WARNING_KEY_1;

  // Mutation RESOLVES the overlap: server renders no banner, empty data.
  renderSwap(env, '', '', false);

  // Mutation RECREATES the IDENTICAL overlap: same key, same text.
  renderSwap(env, WARNING_TEXT_1, WARNING_KEY_1, true);

  const banner = env.document.getElementById('accounts-warnings-banner');
  const region = env.document.getElementById('accounts-warnings-region');
  const data = env.document.getElementById('accounts-warnings-data');

  const bannerPresent = banner !== null;
  const dismissPresent = bannerPresent && banner.querySelector('[data-dismiss-warnings]') !== null;
  const currentWarningText = data.getAttribute('data-warnings-text') || '';
  // Parity invariant: the region must never assert the ACTIVE warning
  // content (the same content the banner is built from) while the banner
  // that would let a user act on it is missing from the DOM.
  const regionAssertsWarning = currentWarningText !== '' && region.textContent === currentWarningText;
  const parityOK = !(regionAssertsWarning && !bannerPresent);

  const pass = postDismissBannerAbsent && postDismissStored && bannerPresent
    && dismissPresent && regionAssertsWarning && parityOK;

  return {
    name: 'dismiss_resolve_recreate',
    pass,
    detail: {
      postDismissBannerAbsent, postDismissStored, bannerPresent, dismissPresent,
      regionAssertsWarning, parityOK, regionText: region.textContent, dataText: currentWarningText,
    },
  };
}

// 2. Dismiss -> reload with warnings UNCHANGED.
// Required: banner absent AND region empty.
function testDismissThenReloadUnchanged(scriptSource) {
  const sessionStorage = makeSessionStorage();

  let env = makeEnv();
  renderInitial(env, WARNING_TEXT_1, WARNING_KEY_1, true);
  runPageLoad(scriptSource, env, sessionStorage);

  const banner0 = env.document.getElementById('accounts-warnings-banner');
  const dismissBtn0 = banner0 && banner0.querySelector('[data-dismiss-warnings]');
  if (dismissBtn0) dismissBtn0.dispatchClick();

  const dismissedTookEffect = sessionStorage.getItem('accounts-warnings-dismissed') === WARNING_KEY_1
    && env.document.getElementById('accounts-warnings-banner') === null;

  // A full page reload: brand-new DOM, freshly server-rendered. Warnings
  // are UNCHANGED, so the server still emits the banner and the region's
  // raw warning text exactly as before -- the server has no notion of a
  // client-only, sessionStorage-scoped dismissal. sessionStorage itself
  // persists across a reload of the same tab (that is the whole point of
  // using it here), unlike a fresh navigation would.
  env = makeEnv();
  renderInitial(env, WARNING_TEXT_1, WARNING_KEY_1, true);
  runPageLoad(scriptSource, env, sessionStorage);

  const banner = env.document.getElementById('accounts-warnings-banner');
  const region = env.document.getElementById('accounts-warnings-region');
  const bannerAbsent = banner === null;
  const regionEmpty = region.textContent === '';

  return {
    name: 'dismiss_then_reload_unchanged',
    pass: dismissedTookEffect && bannerAbsent && regionEmpty,
    detail: { dismissedTookEffect, bannerAbsent, regionEmpty, regionText: region.textContent },
  };
}

// 3. Guard directions that must not regress (S4-era behaviour, unrelated
// to the S5 dismissal-ordering fix, but the same script owns all of it):
// an unrelated mutation writes to the region zero times; a genuinely
// changed warning set writes exactly once; warnings clearing leaves the
// region empty.
function testGuardDirections(scriptSource) {
  const env = makeEnv();
  const sessionStorage = makeSessionStorage();

  renderInitial(env, WARNING_TEXT_1, WARNING_KEY_1, true);
  runPageLoad(scriptSource, env, sessionStorage);

  const before1 = env.writes.region;
  // Unrelated mutation: the warning set is byte-identical (same text,
  // same key) even though something else on the page changed.
  renderSwap(env, WARNING_TEXT_1, WARNING_KEY_1, true);
  const unrelatedWrites = env.writes.region - before1;

  const before2 = env.writes.region;
  // A genuinely different warning set.
  renderSwap(env, WARNING_TEXT_2, WARNING_KEY_2, true);
  const changedWrites = env.writes.region - before2;

  // Warnings clear entirely.
  renderSwap(env, '', '', false);
  const region = env.document.getElementById('accounts-warnings-region');
  const clearingEmpty = region.textContent === '';

  return {
    name: 'guard_directions',
    pass: unrelatedWrites === 0 && changedWrites === 1 && clearingEmpty,
    detail: { unrelatedWrites, changedWrites, clearingEmpty },
  };
}

function main() {
  const scriptPath = process.argv[2];
  if (!scriptPath) {
    console.error('usage: node warnings_dom_harness.js <path-to-extracted-script.js>');
    process.exit(2);
  }
  const fs = require('fs');
  const scriptSource = fs.readFileSync(scriptPath, 'utf8');

  const tests = [testDismissResolveRecreate, testDismissThenReloadUnchanged, testGuardDirections];
  let allPass = true;
  for (const t of tests) {
    let result;
    try {
      result = t(scriptSource);
    } catch (e) {
      result = { name: t.name || 'unknown', pass: false, detail: { error: String((e && e.stack) || e) } };
    }
    allPass = allPass && result.pass;
    console.log('RESULT ' + result.name + ' ' + (result.pass ? 'PASS' : 'FAIL') + ' ' + JSON.stringify(result.detail));
  }
  console.log('SUMMARY ' + (allPass ? 'ALL_PASS' : 'SOME_FAILED'));
  process.exit(allPass ? 0 : 1);
}

main();
