// TC1: unit tests for the two pure helpers behind "the trends chart
// follows the table cap" -- visibleTrendCategories / filterTrendTraces,
// exposed on window.insightsTrends. Loads insights.js in a vm with a
// minimal fake document/window (no real DOM), same pattern as
// page-refresh.test.cjs: the harness only needs to survive insights.js's
// top-level statements (event-listener registration, the init call to
// applyTrendsCap()); the functions under test are called directly with
// plain-object fixtures, not through the DOM.
const {test} = require('node:test');
// Loose (non-strict) assert: filterTrendTraces runs inside a vm context, so
// its return values (including nested arrays built with `[]`/JSON.parse
// inside that sandbox) belong to a different realm than the array/object
// literals this file writes to compare against. deepStrictEqual additionally
// checks prototype identity and would fail on realm alone even when every
// value matches; deepEqual compares structurally, which is what's under test.
const assert = require('node:assert');
const fs = require('node:fs');
const vm = require('node:vm');

function load() {
    const window = {
        addEventListener() {},
        localStorage: {getItem() { return null; }, setItem() {}}
    };
    const document = {
        readyState: 'complete',
        body: {addEventListener() {}},
        addEventListener() {},
        getElementById() { return null; } // no live table/chart in this harness
    };
    // Leave Array/JSON/Object/Math unset: vm.createContext gives every
    // sandbox its own consistent set of these intrinsics automatically.
    // Injecting the host's would make JSON.parse (host-realm objects) and
    // array literals (still sandbox-realm, syntax always binds to the
    // executing realm) disagree within the SAME value insights.js builds.
    const context = {document, window, console};
    vm.createContext(context);
    const source = fs.readFileSync(__dirname + '/insights.js', 'utf8');
    vm.runInContext(source, context);
    return context.window.insightsTrends;
}

const trends = load();

function fakeTable(rows) {
    // rows: [{category, hidden}]
    const trs = rows.map(r => ({hidden: !!r.hidden, dataset: {category: r.category}}));
    return {
        querySelector(sel) {
            if (sel !== 'tbody') return null;
            return {querySelectorAll(s) { return s === 'tr' ? trs : []; }};
        }
    };
}

function fixture5() {
    return {
        period: {},
        data: [
            {
                type: 'bar', name: 'Current', x: ['A', 'B', 'C', 'D', 'E'], y: [10, 20, 30, 40, 50],
                marker: {color: ['red', 'green', 'gray', 'red', 'green']}
            },
            {
                type: 'bar', name: 'Prior', x: ['A', 'B', 'C', 'D', 'E'], y: [1, 2, 3, 4, 5],
                marker: {color: '#94a3b8'}
            }
        ],
        layout: {barmode: 'group'}
    };
}

// A payload with n categories ('Cat0'..'Cat(n-1)'), used to pin the height
// formula's 38px-per-row coefficient above the 360 floor -- fixture5's 5
// categories never leave that floor, so a wrong coefficient (e.g. 30 or 40)
// would still pass against it undetected.
function fixtureN(n) {
    const cats = Array.from({length: n}, (_, i) => 'Cat' + i);
    return {
        period: {},
        data: [
            {type: 'bar', name: 'Current', x: cats.slice(), y: cats.map((_, i) => i), marker: {color: '#accent'}},
            {type: 'bar', name: 'Prior', x: cats.slice(), y: cats.map((_, i) => i + 100), marker: {color: '#94a3b8'}}
        ],
        layout: {barmode: 'group'}
    };
}

test('exposes the two pure functions on window.insightsTrends', () => {
    assert.equal(typeof trends.visibleTrendCategories, 'function');
    assert.equal(typeof trends.filterTrendTraces, 'function');
});

test('filterTrendTraces: 3-of-5 subset in a different order filters+reorders both traces, x/y/colours together, height = max(360, n*38+120)', () => {
    const raw = fixture5();
    const before = JSON.stringify(raw);
    const result = trends.filterTrendTraces(raw, ['C', 'A', 'D']);

    assert.deepEqual(result.data[0].x, ['C', 'A', 'D']);
    assert.deepEqual(result.data[0].y, [30, 10, 40]);
    assert.deepEqual(result.data[0].marker.color, ['gray', 'red', 'red']);

    assert.deepEqual(result.data[1].x, ['C', 'A', 'D']);
    assert.deepEqual(result.data[1].y, [3, 1, 4]);
    assert.equal(result.data[1].marker.color, '#94a3b8'); // scalar marker untouched, not filtered as an array

    assert.equal(result.layout.height, Math.max(360, 3 * 38 + 120));

    // raw is not mutated
    assert.equal(JSON.stringify(raw), before);
    assert.notEqual(result, raw);
    assert.notEqual(result.data[0], raw.data[0]);
});

test('filterTrendTraces: empty subset returns empty traces at height 360', () => {
    const raw = fixture5();
    const before = JSON.stringify(raw);
    const result = trends.filterTrendTraces(raw, []);

    assert.deepEqual(result.data[0].x, []);
    assert.deepEqual(result.data[0].y, []);
    assert.deepEqual(result.data[0].marker.color, []);
    assert.deepEqual(result.data[1].x, []);
    assert.deepEqual(result.data[1].y, []);
    assert.equal(result.layout.height, 360);

    assert.equal(JSON.stringify(raw), before);
});

test('filterTrendTraces: full set in original order round-trips (table-absent fallback shape) at the full-count height', () => {
    const raw = fixture5();
    const result = trends.filterTrendTraces(raw, raw.data[0].x);
    assert.deepEqual(result.data[0].x, ['A', 'B', 'C', 'D', 'E']);
    assert.deepEqual(result.data[0].y, [10, 20, 30, 40, 50]);
    assert.deepEqual(result.data[0].marker.color, ['red', 'green', 'gray', 'red', 'green']);
    assert.equal(result.layout.height, Math.max(360, 5 * 38 + 120));
});

test('visibleTrendCategories: only non-hidden rows, in DOM order', () => {
    const table = fakeTable([
        {category: 'A', hidden: false},
        {category: 'B', hidden: true},
        {category: 'C', hidden: false},
        {category: 'D', hidden: true},
        {category: 'E', hidden: false}
    ]);
    assert.deepEqual(trends.visibleTrendCategories(table), ['A', 'C', 'E']);
});

test('visibleTrendCategories: all-visible table returns every category in order', () => {
    const table = fakeTable([
        {category: 'Groceries', hidden: false},
        {category: 'Utilities', hidden: false}
    ]);
    assert.deepEqual(trends.visibleTrendCategories(table), ['Groceries', 'Utilities']);
});

// Finding B (TC1 attempt 2, ruling TC-2026-09-08b): pin the 38px coefficient
// itself, not just the 360 floor -- these two counts are exactly the
// default-cap and full-N figures the rendered checks assert against
// (12 -> 576, matching TC.0's 41-category/33-category table rows' default
// cap; 41 -> 1678, matching TC.0's "Jan 1 - Aug 27" full-expand height).
test('filterTrendTraces: 12-of-N subset -> height 576 (12*38+120, matches the default cap)', () => {
    const raw = fixtureN(20);
    const categories = raw.data[0].x.slice(0, 12);
    const result = trends.filterTrendTraces(raw, categories);
    assert.equal(result.data[0].x.length, 12);
    assert.equal(result.layout.height, 576);
});

test('filterTrendTraces: 41-category full set -> height 1678 (41*38+120, matches TC.0s full-expand figure)', () => {
    const raw = fixtureN(41);
    const result = trends.filterTrendTraces(raw, raw.data[0].x);
    assert.equal(result.data[0].x.length, 41);
    assert.equal(result.layout.height, 1678);
});

// A raw payload with NO marker property on either trace at all -- unlike
// fixture5/fixtureN above, whose 'marker' keys stand in for the endpoint's
// own per-point colour arrays and are unrelated to theming. themedTrendPayload
// is the function that first attaches a marker for rendering; starting from
// a raw with none makes "raw picked up a marker" and "raw didn't" unambiguous.
function fixtureNoMarker(categories) {
    return {
        period: {},
        data: [
            {type: 'bar', name: 'Current', x: categories.slice(), y: categories.map((_, i) => i * 10)},
            {type: 'bar', name: 'Prior', x: categories.slice(), y: categories.map((_, i) => i)}
        ],
        layout: {barmode: 'group'}
    };
}

// TH1: themedTrendPayload composes filterTrendTraces with per-trace markers
// for a full repaint (the fix for the theme-toggle-after-tab-switch height
// collapse -- see the TH1 comment in insights.js above the themechange
// listener). It exposes no new filtering behaviour of its own; these cases
// pin the marker assignment and the two things it must never do: mutate raw,
// or leave a marker on raw's own traces.
test('exposes themedTrendPayload on window.insightsTrends', () => {
    assert.equal(typeof trends.themedTrendPayload, 'function');
});

test('themedTrendPayload: 5-category payload (two traces, x/y, no markers), 3-of-5 subset reordered -> filtered x/y, height 360, markers[i] applied to trace i', () => {
    const raw = fixtureNoMarker(['A', 'B', 'C', 'D', 'E']);
    const before = JSON.parse(JSON.stringify(raw));
    const markers = [{color: '#111'}, {color: '#222', pattern: {shape: '/'}}];

    const result = trends.themedTrendPayload(raw, ['C', 'A', 'D'], markers);

    assert.deepEqual(result.data[0].x, ['C', 'A', 'D']);
    assert.deepEqual(result.data[0].y, [20, 0, 30]);
    assert.deepEqual(result.data[1].x, ['C', 'A', 'D']);
    assert.deepEqual(result.data[1].y, [2, 0, 3]);
    assert.equal(result.layout.height, 360);

    assert.deepEqual(result.data[0].marker, markers[0]);
    assert.deepEqual(result.data[1].marker, markers[1]);

    // raw deep-equals its pre-call snapshot, and no raw trace picked up a
    // marker property from this call.
    assert.deepEqual(raw, before);
    for (let i = 0; i < raw.data.length; i++) {
        assert.ok(!('marker' in raw.data[i]));
    }
});

test('themedTrendPayload: 12-category payload with all 12 -> height 576', () => {
    const categories = Array.from({length: 12}, (_, i) => 'Cat' + i);
    const raw = fixtureNoMarker(categories);
    const markers = [{color: '#111'}, {color: '#222', pattern: {shape: '/'}}];

    const result = trends.themedTrendPayload(raw, categories, markers);

    assert.deepEqual(result.data[0].x, categories);
    assert.equal(result.layout.height, 576);
    assert.deepEqual(result.data[0].marker, markers[0]);
    assert.deepEqual(result.data[1].marker, markers[1]);
    for (let i = 0; i < raw.data.length; i++) {
        assert.ok(!('marker' in raw.data[i]));
    }
});
