// What-If Quick Adjust floating panel: tab switching, control mirroring
// (slider <-> number input pairs), live display formatting, click-
// outside/Escape to close. Extracted from
// components/whatif/quick-adjust-scripts.html (U7).

window.quickAdjustState = window.quickAdjustState || {
    activeTab: 'portfolio'
};

// roundHalfEven rounds v to the nearest integer, ties (an exact .5) to the
// nearest EVEN integer — the same rule Go's fmt "%.0f" applies (WS1 R-FMT:
// formatNumber, internal/templates/render.go). Plain Math.round ties AWAY
// from zero (half-up for positive v), which disagrees with the server on a
// real .50 stored value (e.g. 1800.50: Math.round -> 1801, half-even ->
// 1800) — the exact split this rule exists to close.
function roundHalfEven(v) {
    var negative = v < 0;
    var abs = Math.abs(v);
    var floor = Math.floor(abs);
    var diff = abs - floor;
    var rounded;
    if (diff < 0.5) {
        rounded = floor;
    } else if (diff > 0.5) {
        rounded = floor + 1;
    } else {
        rounded = (floor % 2 === 0) ? floor : floor + 1;
    }
    return negative ? -rounded : rounded;
}

// formatWholeDollars is the SINGLE JS whole-dollar formatter: round half to
// even (matching Go's formatNumber — WS1 R-FMT), thousands separators,
// leading $. Every JS recomputation of a whole-dollar figure must route
// through this — do not add another bare locale-string call or a second
// Math.round formatter.
function formatWholeDollars(v) {
    return '$' + roundHalfEven(v).toLocaleString('en-US');
}

// incrementDigitString adds 1 to the integer represented by a decimal
// digit string, propagating carry (e.g. "199" -> "200"). Used only by
// roundHalfEvenDecimalString below.
function incrementDigitString(s) {
    var arr = s.split('');
    for (var i = arr.length - 1; i >= 0; i--) {
        if (arr[i] === '9') {
            arr[i] = '0';
        } else {
            arr[i] = String(Number(arr[i]) + 1);
            return arr.join('');
        }
    }
    return '1' + arr.join('');
}

// roundHalfEvenDecimalString rounds v to `digits` decimal places using
// round-half-to-even on v's EXACT binary value, returned as a string with
// exactly `digits` fraction digits — the same rule Go's fmt "%.<digits>f"
// applies (WS1 R-FMT': one-decimal percents). Plain toFixed(digits) ties
// AWAY from zero (ECMA-262: "if there are two such n, pick the larger"),
// which disagrees with Go at an EXACT tie (7.25 -> toFixed(1) "7.3", Go
// %.1f "7.2"); every NON-tie value already agrees between the two rules
// (both resolve to whichever candidate the double's true value is nearer
// to), so only exact ties (a value whose binary double lands EXACTLY on
// the ...50 boundary, e.g. any multiple of 0.25 for one decimal place)
// differ. toFixed(digits + 30) exposes far more of the double's own exact
// (necessarily terminating) decimal expansion than `digits` alone would,
// which is what lets this tell a genuine tie ("...500000...0", nothing
// but zeros after) from a value merely close to one
// ("...4999999...9", "...5000000...1") — 30 extra digits is far beyond
// any tie ambiguity for a float64 in the range these percent fields hold.
function roundHalfEvenDecimalString(v, digits) {
    // WS1 C2: Go's bare "%.1f" (the template's own {{printf "%.1f" ...}}
    // calls, no CB9 zero-belt) preserves the sign bit unconditionally,
    // including on a literal -0 input and on any genuine negative v that
    // rounds to zero (e.g. -0.03 -> "-0.0") — checker-second's 32,465-value
    // sweep found exactly this one divergence (1,014/32,465): the previous
    // `negative && Number(result) !== 0` dropped the sign whenever the
    // ROUNDED result was zero. Match Go exactly: the sign is a property of
    // the INPUT, never of the rounded output.
    var negative = v < 0 || Object.is(v, -0);
    var abs = Math.abs(v);
    var extra = Math.min(100, digits + 30);
    var s = abs.toFixed(extra);
    var dot = s.indexOf('.');
    var intPart = dot === -1 ? s : s.slice(0, dot);
    var fracPart = dot === -1 ? '' : s.slice(dot + 1);
    var keep = fracPart.slice(0, digits);
    var rest = fracPart.slice(digits);
    var isExactTie = /^50*$/.test(rest);
    var digitsStr = intPart + keep;
    var roundUp;
    if (isExactTie) {
        var lastDigit = Number(digitsStr.charAt(digitsStr.length - 1));
        roundUp = (lastDigit % 2) === 1; // tie: round to the EVEN neighbour
    } else {
        roundUp = rest.charAt(0) >= '5';
    }
    if (roundUp) {
        digitsStr = incrementDigitString(digitsStr);
    }
    var intLen = digitsStr.length - digits;
    var resultInt = digitsStr.slice(0, intLen) || '0';
    var resultFrac = digits > 0 ? digitsStr.slice(intLen) : '';
    var result = digits > 0 ? resultInt + '.' + resultFrac : resultInt;
    if (negative) result = '-' + result;
    return result;
}

function getQuickAdjustControlsByKey(key, mirror) {
    return Array.from(document.querySelectorAll('[data-quick-adjust-key]')).filter(function(control) {
        return control.dataset.quickAdjustKey === key && control.hasAttribute('data-quick-adjust-mirror') === mirror;
    });
}

function getQuickAdjustCanonicalControl(key) {
    return getQuickAdjustControlsByKey(key, false)[0] || null;
}

function getQuickAdjustMirrorControls(key) {
    return getQuickAdjustControlsByKey(key, true);
}

function getQuickAdjustDisplays(key) {
    return Array.from(document.querySelectorAll('[data-quick-adjust-display]')).filter(function(node) {
        return node.dataset.quickAdjustDisplay === key;
    });
}

// computeQuickAdjustDisplayText is the pure half of formatQuickAdjustDisplay:
// given a format key and a value, it returns the exact text that format
// would show, with no DOM writes. Split out (WS1) so the same formatting
// used for a visible display span can also be used for a range's
// aria-valuetext (WCAG 4.1.2) via syncQuickAdjustAriaValueTexts below —
// one formatting rule per format key, never a second one for ARIA text.
// Formats that need extra DOM context beyond the value ('phase-dollar') or
// that have no single-string representation ('investment-return') are
// handled by formatQuickAdjustDisplay itself and never given an
// aria-valuetext-bearing control, so they never reach here for that use.
function computeQuickAdjustDisplayText(format, value) {
    const numericValue = Number(value);
    switch (format) {
        case 'currency':
            return formatWholeDollars(numericValue);
        case 'years':
            return Math.round(numericValue) + ' years';
        case 'delay-years':
            return numericValue === 0 ? 'No delay' : Math.round(numericValue) + ' years';
        case 'coverage-years':
            return numericValue === 0 ? 'Medicare' : Math.round(numericValue) + ' yrs';
        case 'percent1':
            // WS1 R-FMT': half-even on the exact binary value, matching Go
            // %.1f — plain toFixed(1) ties away from zero (7.25 -> "7.3"
            // vs the server's "7.2").
            return roundHalfEvenDecimalString(numericValue, 1) + '%';
        case 'percent0':
            return Math.round(numericValue) + '%';
        case 'phase-percent':
            return Math.round(numericValue * 100) + '%';
        default:
            return String(value);
    }
}

function formatQuickAdjustDisplay(node, value) {
    const format = node.dataset.quickAdjustDisplayFormat || 'raw';
    const numericValue = Number(value);

    switch (format) {
        case 'phase-dollar': {
            // Read the canonical exact value (the hidden field), not the
            // step=100-snapped visible range — the snapped value can be off
            // by up to $50 from the saved/dragged figure.
            const baseExpenses = parseFloat(document.getElementById('monthly_living_expenses_value')?.value || '0');
            node.textContent = formatWholeDollars(numericValue * baseExpenses) + '/mo';
            return;
        }
        case 'investment-return':
            // Skip — updateInvestmentReturnDisplay() is the single authority for this display.
            // It handles both canonical and mirror displays via querySelectorAll.
            return;
        default:
            node.textContent = computeQuickAdjustDisplayText(format, value);
    }
}

// syncQuickAdjustAriaValueTexts keeps aria-valuetext current on every range
// for this key that already carries one (WS1). A control opts in simply by
// being authored with a starting aria-valuetext + data-quick-adjust-display-
// format in its template — covers the primary in-card slider AND its Quick
// Adjust mirror alike, the same way syncQuickAdjustDisplays already covers
// every display span for a key. WCAG 4.1.2: the browser's own implicit
// aria-valuenow/valuetext for a range reports the step-snapped position,
// which can be off from the true saved/dragged figure whenever a hidden
// exact-value input is the field's canonical source (the W2 Part B / WS1
// pattern) — this is what keeps the announced text the exact figure instead.
function syncQuickAdjustAriaValueTexts(key, value) {
    Array.from(document.querySelectorAll('[data-quick-adjust-key]')).forEach(function(control) {
        if (control.dataset.quickAdjustKey !== key) return;
        if (!control.hasAttribute('aria-valuetext')) return;
        const format = control.dataset.quickAdjustDisplayFormat || 'raw';
        control.setAttribute('aria-valuetext', computeQuickAdjustDisplayText(format, value));
    });
}

function syncQuickAdjustDisplays(key, value) {
    getQuickAdjustDisplays(key).forEach(function(node) {
        formatQuickAdjustDisplay(node, value);
    });
}

function syncQuickAdjustMirrorControls(key, canonicalControl) {
    getQuickAdjustMirrorControls(key).forEach(function(mirrorControl) {
        mirrorControl.value = canonicalControl.value;
        if ('min' in mirrorControl && canonicalControl.min !== '') mirrorControl.min = canonicalControl.min;
        if ('max' in mirrorControl && canonicalControl.max !== '') mirrorControl.max = canonicalControl.max;
        if ('step' in mirrorControl && canonicalControl.step !== '') mirrorControl.step = canonicalControl.step;
    });
}

function syncQuickAdjustPortfolioRangeSelect() {
    const canonicalSelect = document.getElementById('portfolio-range');
    const mirrorSelect = document.getElementById('quick-adjust-portfolio-range');
    if (!canonicalSelect || !mirrorSelect) return;
    mirrorSelect.value = canonicalSelect.value;
}

// syncQuickAdjustMirrorOnly keeps a mirror control's VALUE (and its
// min/max/step, and — for portfolio_value — the range-select's selected
// bucket) in sync with the canonical control, WITHOUT touching any display
// span or aria-valuetext (WS1 R-FMT': "the Quick Adjust load/after-swap
// sync must not rewrite any display text or aria-valuetext ... it may
// still sync mirror values/min/max/step"). This is the ONLY sync that may
// run at page load or after an htmx swap — every display/aria surface was
// just server-rendered correctly and must be left exactly as sent until a
// real user interaction changes something.
function syncQuickAdjustMirrorOnly(key) {
    const canonicalControl = getQuickAdjustCanonicalControl(key);
    if (!canonicalControl) return null;
    syncQuickAdjustMirrorControls(key, canonicalControl);
    if (key === 'portfolio_value') {
        syncQuickAdjustPortfolioRangeSelect();
    }
    return canonicalControl;
}

// syncQuickAdjustKey is the FULL sync — mirror values AND every
// display/aria surface for this key, plus the per-key derived-display
// hooks below. Call this ONLY in response to a genuine user-driven
// input/change (a real drag, typed value, or Quick Adjust mirror proxy) —
// never from a load or htmx-swap sweep (see syncQuickAdjustMirrorOnly
// above and syncAllQuickAdjustControls below).
function syncQuickAdjustKey(key) {
    const canonicalControl = syncQuickAdjustMirrorOnly(key);
    if (!canonicalControl) return;

    syncQuickAdjustDisplays(key, canonicalControl.value);
    syncQuickAdjustAriaValueTexts(key, canonicalControl.value);

    if (key === 'portfolio_value') {
        // syncQuickAdjustMirrorOnly above already resynced the range-select
        // bucket (that's a mirror concern, not a display/aria one).
        // WS1 C5 R3: updateAccountAmounts used to live in the canonical
        // (named) portfolio range's own inline oninput; the canonical
        // control is now a hidden field with no oninput of its own, so a
        // Quick Adjust portfolio drag would otherwise never refresh the
        // per-account $ breakdown. Calling it here covers every source
        // (in-card drag, Quick Adjust mirror drag, and the range-select
        // resync below) through the one path that already runs whenever
        // portfolio_value changes.
        if (typeof updateAccountAmounts === 'function') {
            updateAccountAmounts();
        }
    }

    if (key === 'investment_return') {
        // WS1 C5 R2 + C4: updateInvestmentReturnDisplay used to live in the
        // canonical (named) investment-return range's own inline oninput;
        // the canonical control is now a hidden field with no oninput, so a
        // Quick Adjust drag would otherwise never refresh the in-card/Quick
        // Adjust display text (a compound sentence formatQuickAdjustDisplay
        // deliberately never formats) or the aria-valuetext it also sets on
        // both ranges.
        if (typeof updateInvestmentReturnDisplay === 'function') {
            updateInvestmentReturnDisplay(canonicalControl.value);
        }
    }

    if (key === 'inflation_rate' || key === 'spending_decline_rate') {
        // WS1 C5 R3 (extended): updateSpendingPreview used to run from the
        // canonical range's own inline oninput too; restore it for a Quick
        // Adjust drag on either rate the same way.
        if (typeof updateSpendingPreview === 'function') {
            updateSpendingPreview();
        }
    }

    if (key === 'monthly_living_expenses') {
        // Keyed off canonicalControl.value (the hidden exact-value input, not
        // the step=100 range) so this always reflects the true saved/dragged
        // amount, whether the change came from the primary slider or its
        // quick-adjust mirror.
        if (typeof updateLivingExpensesPhaseNote === 'function') {
            updateLivingExpensesPhaseNote(canonicalControl.value);
        }
        // aria-valuetext must always carry the exact value (e.g. "$7,386"),
        // never the browser-snapped step=100 grid value the range's implicit
        // aria-valuenow would report — WCAG 4.1.2. Updated here, in the same
        // sync path that updates the visible display span, so it can never
        // drift from what's shown/saved.
        const exactValueText = formatWholeDollars(Number(canonicalControl.value) || 0);
        const primaryRange = document.getElementById('monthly_living_expenses_input');
        if (primaryRange) {
            primaryRange.setAttribute('aria-valuetext', exactValueText);
            // Thumb parity: a mirror drag proxies into the canonical hidden
            // field (see proxyQuickAdjustMirrorToCanonical), but the primary
            // range's own .value never followed — leaving its thumb position
            // (and implicit aria-valuenow) contradicting the announced
            // aria-valuetext. Setting it here keeps both directions in sync;
            // the browser snaps this assignment to the nearest step, which is
            // fine for a range control's visual thumb position.
            primaryRange.value = canonicalControl.value;
        }
        getQuickAdjustMirrorControls(key).forEach(function(mirrorControl) {
            mirrorControl.setAttribute('aria-valuetext', exactValueText);
        });
        Array.from(document.querySelectorAll('[data-quick-adjust-key]')).forEach(function(control) {
            if (!control.hasAttribute('data-quick-adjust-mirror') && control.dataset.quickAdjustKey.indexOf('phase:') === 0) {
                syncQuickAdjustDisplays(control.dataset.quickAdjustKey, control.value);
            }
        });
    }
}

function quickAdjustSyncFromCanonical(control) {
    if (!control || !control.dataset.quickAdjustKey || control.hasAttribute('data-quick-adjust-mirror')) return;
    syncQuickAdjustKey(control.dataset.quickAdjustKey);
}

function proxyQuickAdjustMirrorToCanonical(mirrorControl, triggerChange) {
    const key = mirrorControl.dataset.quickAdjustKey;
    const canonicalControl = getQuickAdjustCanonicalControl(key);
    if (!canonicalControl) return;

    canonicalControl.value = mirrorControl.value;

    if (triggerChange) {
        canonicalControl.dispatchEvent(new Event('change', { bubbles: true }));
    } else {
        canonicalControl.dispatchEvent(new Event('input', { bubbles: true }));
    }

    syncQuickAdjustKey(key);
}

// syncAllQuickAdjustControls runs at page load, after every htmx swap, and
// when the Quick Adjust panel opens (scheduleQuickAdjustSync /
// DOMContentLoaded / toggleQuickAdjustPanel below) — none of those is a
// user editing a value, so it must use the MIRROR-ONLY sync (WS1 R-FMT'):
// every display span and aria-valuetext the server just rendered is left
// exactly as sent. The one exception is updateAccountAmounts(), called
// once here unconditionally: its two spans (td/roth/taxable-amount-display)
// are NEVER server-rendered (they start empty in the template) and have no
// other populate path, so this isn't "rewriting server text" — it's the
// only renderer that field has ever had.
function syncAllQuickAdjustControls() {
    if (typeof initializePortfolioRange === 'function') {
        initializePortfolioRange();
    }

    Array.from(document.querySelectorAll('[data-quick-adjust-key]')).forEach(function(control) {
        if (!control.hasAttribute('data-quick-adjust-mirror')) {
            syncQuickAdjustMirrorOnly(control.dataset.quickAdjustKey);
        }
    });

    if (typeof updateAccountAmounts === 'function') {
        updateAccountAmounts();
    }
}

function showQuickAdjustTab(tabName) {
    window.quickAdjustState.activeTab = tabName;

    Array.from(document.querySelectorAll('[data-quick-adjust-tab]')).forEach(function(button) {
        const isActive = button.dataset.quickAdjustTab === tabName;
        button.classList.toggle('qa-tab-active', isActive);
    });

    Array.from(document.querySelectorAll('[data-quick-adjust-tab-panel]')).forEach(function(panel) {
        panel.classList.toggle('hidden', panel.dataset.quickAdjustTabPanel !== tabName);
    });
}

function toggleQuickAdjustPanel(forceOpen) {
    const panel = document.getElementById('quick-adjust-panel');
    const button = document.getElementById('quick-adjust-toggle');
    if (!panel || !button) return;

    const shouldOpen = typeof forceOpen === 'boolean'
        ? forceOpen
        : panel.classList.contains('hidden');

    panel.classList.toggle('hidden', !shouldOpen);
    button.classList.toggle('hidden', shouldOpen);
    button.setAttribute('aria-expanded', shouldOpen ? 'true' : 'false');

    if (shouldOpen) {
        showQuickAdjustTab(window.quickAdjustState.activeTab || 'portfolio');
        syncAllQuickAdjustControls();
    } else {
        button.classList.remove('hidden');
        button.focus();
    }
}

function scheduleQuickAdjustSync() {
    window.requestAnimationFrame(function() {
        syncAllQuickAdjustControls();
        showQuickAdjustTab(window.quickAdjustState.activeTab || 'portfolio');
    });
}

document.addEventListener('DOMContentLoaded', function() {
    showQuickAdjustTab(window.quickAdjustState.activeTab || 'portfolio');
    syncAllQuickAdjustControls();
});

document.addEventListener('input', function(event) {
    const control = event.target;
    if (!control || !control.dataset || !control.dataset.quickAdjustKey) return;

    if (control.hasAttribute('data-quick-adjust-mirror')) {
        proxyQuickAdjustMirrorToCanonical(control, false);
        return;
    }

    syncQuickAdjustKey(control.dataset.quickAdjustKey);
}, true);

document.addEventListener('change', function(event) {
    const control = event.target;
    if (!control || !control.dataset || !control.dataset.quickAdjustKey) return;

    if (control.hasAttribute('data-quick-adjust-mirror')) {
        proxyQuickAdjustMirrorToCanonical(control, true);
        return;
    }

    syncQuickAdjustKey(control.dataset.quickAdjustKey);
}, true);

document.addEventListener('click', function(event) {
    const panel = document.getElementById('quick-adjust-panel');
    const button = document.getElementById('quick-adjust-toggle');
    if (!panel || !button || panel.classList.contains('hidden')) return;

    if (panel.contains(event.target) || button.contains(event.target)) return;
    toggleQuickAdjustPanel(false);
});

document.addEventListener('keydown', function(event) {
    if (event.key !== 'Escape') return;
    const panel = document.getElementById('quick-adjust-panel');
    if (!panel || panel.classList.contains('hidden')) return;
    toggleQuickAdjustPanel(false);
});

document.body.addEventListener('htmx:afterSwap', scheduleQuickAdjustSync);
document.body.addEventListener('htmx:oobAfterSwap', scheduleQuickAdjustSync);

// The toggle/close buttons and the four tab buttons used to carry inline
// onclick= (U7).
document.addEventListener('DOMContentLoaded', function () {
    var toggle = document.getElementById('quick-adjust-toggle');
    if (toggle) toggle.addEventListener('click', function () { toggleQuickAdjustPanel(); });
    var close = document.getElementById('quick-adjust-close');
    if (close) close.addEventListener('click', function () { toggleQuickAdjustPanel(false); });
    document.querySelectorAll('[data-quick-adjust-tab]').forEach(function (btn) {
        btn.addEventListener('click', function () { showQuickAdjustTab(btn.dataset.quickAdjustTab); });
    });
});
