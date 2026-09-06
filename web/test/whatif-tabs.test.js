'use strict';

const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');
const vm = require('node:vm');

class FakeClassList {
  constructor(names) { this.names = new Set(names || []); }
  contains(name) { return this.names.has(name); }
  toggle(name, force) {
    const on = force === undefined ? !this.names.has(name) : force;
    if (on) this.names.add(name); else this.names.delete(name);
    return on;
  }
}

class FakeElement {
  constructor(document, attributes, classes) {
    this.document = document;
    this.attributes = new Map(Object.entries(attributes || {}));
    this.classList = new FakeClassList(classes);
    this.listeners = new Map();
    this.children = [];
    this.parent = null;
    this.focusCount = 0;
  }
  append(child) { child.parent = this; this.children.push(child); return child; }
  get id() { return this.getAttribute('id') || ''; }
  getAttribute(name) { return this.attributes.has(name) ? this.attributes.get(name) : null; }
  setAttribute(name, value) { this.attributes.set(name, String(value)); }
  addEventListener(name, fn) {
    if (!this.listeners.has(name)) this.listeners.set(name, []);
    this.listeners.get(name).push(fn);
  }
  dispatch(name, event) {
    (this.listeners.get(name) || []).forEach((fn) => fn(event || { target: this }));
  }
  closest(selector) {
    const match = selector.match(/^\[([^\]]+)\]$/);
    if (match && this.attributes.has(match[1])) return this;
    return this.parent ? this.parent.closest(selector) : null;
  }
  matches(selector) {
    if (selector === '[data-wf-panel]') return this.attributes.has('data-wf-panel');
    if (selector === '[data-wf-tab]') return this.attributes.has('data-wf-tab');
    if (selector === '[data-wf-collapse]') return this.attributes.has('data-wf-collapse');
    if (selector === '[data-wf-collapse-body]') return this.attributes.has('data-wf-collapse-body');
    if (selector === '[data-wf-collapse-toggle]') return this.attributes.has('data-wf-collapse-toggle');
    if (selector === '[data-wf-chevron]') return this.attributes.has('data-wf-chevron');
    if (selector === '[id^="chart-"]') return (this.getAttribute('id') || '').startsWith('chart-');
    return false;
  }
  querySelectorAll(selector) {
    const found = [];
    const visit = (node) => {
      node.children.forEach((child) => {
        if (child.matches(selector)) found.push(child);
        visit(child);
      });
    };
    visit(this);
    return found;
  }
  querySelector(selector) { return this.querySelectorAll(selector)[0] || null; }
  focus() { this.focusCount += 1; this.document.activeElement = this; }
}

class FakeDocument extends FakeElement {
  constructor() {
    super(null);
    this.document = this;
    this.body = this.append(new FakeElement(this, { id: 'body' }));
    this.activeElement = null;
  }
  getElementById(id) {
    let match = null;
    const visit = (node) => {
      if (node.getAttribute && node.getAttribute('id') === id) match = node;
      if (!match) node.children.forEach(visit);
    };
    visit(this);
    return match;
  }
}

function createHarness(options) {
  const document = new FakeDocument();
  const settings = document.body.append(new FakeElement(document, { id: 'whatif-settings-col' }, ['lg:col-span-2']));
  document.body.append(new FakeElement(document, { id: 'whatif-results' }, ['lg:col-span-4']));
  const container = document.body.append(new FakeElement(document, { id: 'whatif-tabs', 'data-scenario': 'scenario-a.json' }));
  const tabs = {};
  const panels = {};
  ['overview', 'cashflow', 'risk', 'taxes', 'strategies'].forEach((name, index) => {
    tabs[name] = container.append(new FakeElement(document, {
      id: 'wf-tab-' + name,
      'data-wf-tab': name,
      'aria-selected': index === 0 ? 'true' : 'false',
      tabindex: index === 0 ? '0' : '-1'
    }));
    panels[name] = container.append(new FakeElement(document, { 'data-wf-panel': name }, index === 0 ? [] : ['hidden']));
  });

  const collapses = {};
  ['money', 'assumptions', 'strategies'].forEach((name) => {
    const card = settings.append(new FakeElement(document, { 'data-wf-collapse': name }));
    const toggle = card.append(new FakeElement(document, {
      'data-wf-collapse-toggle': '',
      'aria-expanded': 'true'
    }));
    const chevron = card.append(new FakeElement(document, { 'data-wf-chevron': '' }, ['rotate-180']));
    const body = card.append(new FakeElement(document, { 'data-wf-collapse-body': '' }));
    collapses[name] = { toggle, body };
  });

  const values = new Map(Object.entries((options && options.storage) || {}));
  const localStorage = {
    getItem(key) { return values.has(key) ? values.get(key) : null; },
    setItem(key, value) { values.set(key, String(value)); }
  };
  const window = { localStorage };
  const source = fs.readFileSync(path.join(__dirname, '..', 'static', 'js', 'whatif-tabs.js'), 'utf8');
  vm.runInNewContext(source, { document, window });
  document.dispatch('DOMContentLoaded', { target: document });
  return { document, values, container, tabs, panels, collapses };
}

function keyEvent(target, key) {
  return {
    target,
    key,
    prevented: false,
    preventDefault() { this.prevented = true; }
  };
}

test('restores the per-scenario tab with roving state without stealing focus', () => {
  const h = createHarness({ storage: { 'whatifActiveTab:scenario-a.json': 'cashflow' } });
  assert.equal(h.tabs.cashflow.getAttribute('aria-selected'), 'true');
  assert.equal(h.tabs.cashflow.getAttribute('tabindex'), '0');
  assert.equal(h.panels.cashflow.classList.contains('hidden'), false);
  assert.equal(h.tabs.overview.getAttribute('aria-selected'), 'false');
  assert.equal(h.tabs.overview.getAttribute('tabindex'), '-1');
  assert.equal(h.panels.overview.classList.contains('hidden'), true);
  assert.equal(h.document.activeElement, null);

  h.document.body.dispatch('htmx:afterSettle', { detail: { target: h.document.getElementById('whatif-results') } });
  assert.equal(h.document.activeElement, null);
  assert.equal(Object.values(h.tabs).reduce((sum, tab) => sum + tab.focusCount, 0), 0);
});

test('falls back to Overview when the saved tab is unknown', () => {
  const h = createHarness({ storage: { 'whatifActiveTab:scenario-a.json': 'missing' } });
  assert.equal(h.tabs.overview.getAttribute('aria-selected'), 'true');
  assert.equal(h.tabs.overview.getAttribute('tabindex'), '0');
  assert.equal(h.panels.overview.classList.contains('hidden'), false);
});

test('supports wraparound and Home/End keyboard tab selection', () => {
  const h = createHarness();

  const left = keyEvent(h.tabs.overview, 'ArrowLeft');
  h.container.dispatch('keydown', left);
  assert.equal(left.prevented, true);
  assert.equal(h.tabs.strategies.getAttribute('aria-selected'), 'true');
  assert.equal(h.tabs.strategies.getAttribute('tabindex'), '0');
  assert.equal(h.tabs.strategies.focusCount, 1);
  assert.equal(h.values.get('whatifActiveTab:scenario-a.json'), 'strategies');

  const home = keyEvent(h.tabs.strategies, 'Home');
  h.container.dispatch('keydown', home);
  assert.equal(h.tabs.overview.getAttribute('aria-selected'), 'true');
  assert.equal(h.tabs.overview.focusCount, 1);

  const end = keyEvent(h.tabs.overview, 'End');
  h.container.dispatch('keydown', end);
  assert.equal(h.tabs.strategies.getAttribute('aria-selected'), 'true');
  assert.equal(h.tabs.strategies.focusCount, 2);

  const right = keyEvent(h.tabs.strategies, 'ArrowRight');
  h.container.dispatch('keydown', right);
  assert.equal(h.tabs.overview.getAttribute('aria-selected'), 'true');
  assert.equal(h.tabs.overview.focusCount, 2);
});

test('restores and toggles collapse announcement state with the existing key', () => {
  const h = createHarness({ storage: { 'whatifCollapse:money': '1' } });
  const money = h.collapses.money;
  assert.equal(money.body.classList.contains('hidden'), true);
  assert.equal(money.toggle.getAttribute('aria-expanded'), 'false');

  money.toggle.dispatch('click', { target: money.toggle });
  assert.equal(money.body.classList.contains('hidden'), false);
  assert.equal(money.toggle.getAttribute('aria-expanded'), 'true');
  assert.equal(h.values.get('whatifCollapse:money'), '0');
});
