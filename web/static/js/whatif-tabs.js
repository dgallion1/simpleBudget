// What-If page: tab switching, per-scenario persistence, settings-card
// collapse, and Plotly resize when a hidden tab becomes visible.
(function () {
  'use strict';

  function scenarioKey() {
    var c = document.getElementById('whatif-tabs');
    var sc = c ? (c.getAttribute('data-scenario') || 'default') : 'default';
    return 'whatifActiveTab:' + sc;
  }

  function resizeChartsIn(panel) {
    if (!panel || !window.Plotly) return;
    panel.querySelectorAll('[id^="chart-"]').forEach(function (el) {
      try { window.Plotly.Plots.resize(el); } catch (e) { /* not yet rendered */ }
    });
  }

  function activateTab(name, persist) {
    var container = document.getElementById('whatif-tabs');
    if (!container) return;
    var panels = container.querySelectorAll('[data-wf-panel]');
    var tabs = container.querySelectorAll('[data-wf-tab]');
    var matched = false;

    panels.forEach(function (p) {
      var on = p.getAttribute('data-wf-panel') === name;
      p.classList.toggle('hidden', !on);
      if (on) { matched = true; resizeChartsIn(p); }
    });
    tabs.forEach(function (t) {
      var on = t.getAttribute('data-wf-tab') === name;
      t.classList.toggle('wf-tab-active', on);
      t.setAttribute('aria-selected', on ? 'true' : 'false');
    });

    if (!matched) { return activateTab('overview', persist); }
    if (persist && window.localStorage) {
      try { window.localStorage.setItem(scenarioKey(), name); } catch (e) {}
    }
  }

  function restoreTab() {
    var name = 'overview';
    if (window.localStorage) {
      try { name = window.localStorage.getItem(scenarioKey()) || 'overview'; } catch (e) {}
    }
    activateTab(name, false);
  }

  // Settings-card collapse with persistence.
  function applyCollapse(card) {
    var id = card.getAttribute('data-wf-collapse');
    var body = card.querySelector('[data-wf-collapse-body]');
    if (!body) return;
    var collapsed = false;
    if (window.localStorage) {
      try { collapsed = window.localStorage.getItem('whatifCollapse:' + id) === '1'; } catch (e) {}
    }
    body.classList.toggle('hidden', collapsed);
    var chevron = card.querySelector('[data-wf-chevron]');
    if (chevron) chevron.classList.toggle('rotate-180', !collapsed);
  }

  function toggleCollapse(card) {
    var id = card.getAttribute('data-wf-collapse');
    var body = card.querySelector('[data-wf-collapse-body]');
    if (!body) return;
    var nowCollapsed = !body.classList.contains('hidden');
    body.classList.toggle('hidden', nowCollapsed);
    var chevron = card.querySelector('[data-wf-chevron]');
    if (chevron) chevron.classList.toggle('rotate-180', !nowCollapsed);
    if (window.localStorage) {
      try { window.localStorage.setItem('whatifCollapse:' + id, nowCollapsed ? '1' : '0'); } catch (e) {}
    }
    syncSettingsRail();
  }

  // With every settings group collapsed the left column is three thin bars;
  // narrow it to a 1/6 rail and let the results span the reclaimed width.
  function syncSettingsRail() {
    var col = document.getElementById('whatif-settings-col');
    var results = document.getElementById('whatif-results');
    if (!col || !results) return;
    var bodies = document.querySelectorAll('[data-wf-collapse-body]');
    var allCollapsed = bodies.length > 0;
    bodies.forEach(function (b) {
      if (!b.classList.contains('hidden')) allCollapsed = false;
    });
    col.classList.toggle('lg:col-span-1', allCollapsed);
    col.classList.toggle('lg:col-span-2', !allCollapsed);
    results.classList.toggle('lg:col-span-5', allCollapsed);
    results.classList.toggle('lg:col-span-4', !allCollapsed);
    resizeChartsIn(results);
  }

  function wire() {
    var container = document.getElementById('whatif-tabs');
    if (container && !container.__wfWired) {
      container.__wfWired = true;
      container.addEventListener('click', function (e) {
        var tab = e.target.closest('[data-wf-tab]');
        if (tab) { e.preventDefault(); activateTab(tab.getAttribute('data-wf-tab'), true); return; }
        var goto_ = e.target.closest('[data-wf-goto]');
        if (goto_) { e.preventDefault(); activateTab(goto_.getAttribute('data-wf-goto'), true); }
      });
    }
    document.querySelectorAll('[data-wf-collapse]').forEach(function (card) {
      applyCollapse(card);
      var header = card.querySelector('[data-wf-collapse-toggle]');
      if (header && !header.__wfWired) {
        header.__wfWired = true;
        header.addEventListener('click', function () { toggleCollapse(card); });
      }
    });
    syncSettingsRail();
  }

  // New-scenario form toggle/cancel buttons used to carry inline onclick=
  // (U7); harmless no-op on any other page since these ids only exist on
  // pages/whatif.html.
  function wireScenarioForm() {
    var toggle = document.getElementById('whatif-new-scenario-toggle');
    var form = document.getElementById('new-scenario-form');
    if (toggle && form && !toggle.__wfWired) {
      toggle.__wfWired = true;
      toggle.addEventListener('click', function () { form.classList.toggle('hidden'); });
    }
    var cancel = document.getElementById('whatif-new-scenario-cancel');
    if (cancel && form && !cancel.__wfWired) {
      cancel.__wfWired = true;
      cancel.addEventListener('click', function () { form.classList.add('hidden'); });
    }
  }

  document.addEventListener('DOMContentLoaded', function () { wire(); restoreTab(); wireScenarioForm(); });

  // After the results partial re-renders, re-wire tabs and re-apply active tab
  // (charts.js handles chart (re)creation on the same afterSettle event).
  document.body.addEventListener('htmx:afterSettle', function (evt) {
    var t = evt.detail && evt.detail.target;
    if (t && t.id === 'whatif-results') { wire(); restoreTab(); }
  });
})();
