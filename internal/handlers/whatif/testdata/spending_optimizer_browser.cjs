// Standalone synthetic interaction oracle. Financial markup and graph values
// come from the Go handler presentation fixture, never from browser formulas.
const fs = require('fs');
const os = require('os');
const path = require('path');
const assert = require('node:assert/strict');
const child = require('child_process');
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');

(async () => {
  const screenshotDir = process.env.SCREENSHOT_DIR;
  const screenshot = async (name, selector) => {
    if (!screenshotDir) return;
    fs.mkdirSync(screenshotDir, {recursive: true});
    if (selector) await page.locator(selector).first().scrollIntoViewIfNeeded();
    await page.screenshot({path: path.join(screenshotDir, name), fullPage: false});
  };
  const fixturePath = path.join(os.tmpdir(), `spending-browser-${process.pid}.json`);
  const generated = child.spawnSync('go', ['test', './internal/handlers/whatif', '-run', '^TestSpendingOptimizerBrowserFixture$', '-count=1'], {
    cwd: process.cwd(), encoding: 'utf8', env: { ...process.env, SPENDING_BROWSER_FIXTURE: fixturePath }
  });
  assert.equal(generated.status, 0, generated.stdout + generated.stderr);
  const fixture = JSON.parse(fs.readFileSync(fixturePath, 'utf8'));
  fs.unlinkSync(fixturePath);

  const browser = await chromium.launch({headless: true, executablePath: process.env.CHROMIUM_PATH, args: ['--no-sandbox']});
  const page = await browser.newPage({viewport: {width: 650, height: 900}});
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.setContent(`<html><body class="bg-gray-100 dark:bg-gray-900"><main><div class="bg-white dark:bg-gray-800">${fixture.form}</div></main></body></html>`);
  await page.addStyleTag({path: 'web/static/css/tailwind.css'});
  await page.addStyleTag({path: 'web/static/css/styles.css'});
  await page.addScriptTag({path: process.env.PLOTLY_PATH});
  await page.addScriptTag({path: 'web/static/js/charts.js'});
  await page.evaluate(({results, nilResults, noResults, graph}) => {
    window.htmx = { process() {} };
    window.fixtureResults = results;
    window.fixtureGraph = graph;
    window.fixtureNilResults = nilResults;
    window.fixtureNoResults = noResults;
    window.requests = [];
    window.searchDelay = 0;
    window.graphDelay = 0;
    window.applyDelay = 0;
    window.cleanupStats = {mutationDisconnects: 0, resizeDisconnects: 0, plotPurges: 0};
    const NativeMutationObserver = window.MutationObserver;
    window.MutationObserver = class extends NativeMutationObserver {
      disconnect() { window.cleanupStats.mutationDisconnects++; return super.disconnect(); }
    };
    const NativeResizeObserver = window.ResizeObserver;
    window.ResizeObserver = class extends NativeResizeObserver {
      disconnect() { window.cleanupStats.resizeDisconnects++; return super.disconnect(); }
    };
    const plotlyPurge = window.Plotly.purge.bind(window.Plotly);
    window.Plotly.purge = (...args) => { window.cleanupStats.plotPurges++; return plotlyPurge(...args); };
    window.fetch = async (url, options = {}) => {
      const body = options.body;
      const request = {url, method: options.method, kind: body && body.constructor.name, encoded: body && body.toString(), aborted: false};
      if (options.signal) options.signal.addEventListener('abort', () => { request.aborted = true; });
      window.requests.push(request);
      if (url.endsWith('/prepare')) return {ok: true, json: async () => ({request: {floor_monthly_real: 7000, near_term_years: 5, search_min_monthly_real: 6500, search_max_monthly_real: 9500, search_step_monthly_real: 250}, current_base_monthly_real: 8000, current_starting_monthly_real: 8500})};
      if (url.endsWith('/cancel')) return {ok: true, status: 204, text: async () => ''};
      if (url.endsWith('/graph')) {
        if (window.graphDelay) await new Promise(resolve => setTimeout(resolve, window.graphDelay));
        const payload = structuredClone(window.fixtureGraph);
        payload.display_dollars = body.get('display_dollars') || 'real';
        return {ok: true, json: async () => payload};
      }
      if (url.endsWith('/apply')) {
        if (window.applyDelay) await new Promise(resolve => setTimeout(resolve, window.applyDelay));
        return {ok: true, headers: {get: () => null}, text: async () => ''};
      }
      if (url.endsWith('/optimize')) {
        if (window.searchDelay) await new Promise(resolve => setTimeout(resolve, window.searchDelay));
        return {ok: true, text: async () => window.fixtureResults};
      }
      throw new Error(`Unexpected request ${url}`);
    };
  }, {results: fixture.results, nilResults: fixture.nil_results, noResults: fixture.no_results, graph: fixture.graph});
  await page.addScriptTag({path: 'web/static/js/whatif-spending-optimizer.js'});

  const gridContrast = () => page.evaluate(() => {
    const parse = value => (value.match(/[\d.]+/g) || []).slice(0, 3).map(Number);
    const luminance = value => {
      const rgb = parse(value).map(component => {
        const channel = component / 255;
        return channel <= 0.04045 ? channel / 12.92 : ((channel + 0.055) / 1.055) ** 2.4;
      });
      return 0.2126 * rgb[0] + 0.7152 * rgb[1] + 0.0722 * rgb[2];
    };
    const plot = document.querySelector('#spending-graph-main');
    const stroke = getComputedStyle(plot.querySelector('.gridlayer path')).stroke;
    let backgroundNode = plot;
    let background = 'rgba(0, 0, 0, 0)';
    while (backgroundNode && (background === 'rgba(0, 0, 0, 0)' || background === 'transparent')) {
      background = getComputedStyle(backgroundNode).backgroundColor;
      backgroundNode = backgroundNode.parentElement;
    }
    const high = Math.max(luminance(stroke), luminance(background));
    const low = Math.min(luminance(stroke), luminance(background));
    return {stroke, background, ratio: (high + 0.05) / (low + 0.05)};
  });

  assert((await page.locator('#spending-boost-required').textContent()).includes('both extra-spending fields are required'));
  assert(await page.locator('#spending-boost-required').evaluate(node => node.hidden));
  for (const copy of ['Lowest starting monthly living budget', 'Highest starting monthly living budget', 'Starting monthly living step', "today's US dollars per month"]) {
    assert((await page.locator('#spending-optimizer-form').textContent()).includes(copy));
  }

  const minimum = page.locator('#spending-minimum');
  await minimum.fill('7000');
  await page.waitForFunction(() => document.querySelector('#spending-effective-range').textContent.includes('$6,500.00'));
  const prepareRequest = await page.evaluate(() => window.requests.find(request => request.url.endsWith('/prepare')));
  assert.equal(prepareRequest.kind, 'URLSearchParams');
  assert(prepareRequest.encoded.includes('floor_monthly_real=7000'));

  await page.locator('#spending-optimizer-run').click();
  await page.waitForSelector('[data-spending-outcome]');
  assert((await page.locator('[data-spending-outcome]').textContent()).includes('spending options'));
  const optimizeRequest = await page.evaluate(() => window.requests.find(request => request.url.endsWith('/optimize')));
  assert.equal(optimizeRequest.kind, 'URLSearchParams');
  assert(optimizeRequest.encoded.includes('request_id='));
  assert.equal(await page.locator('[data-spending-headline]').count(), 2);
  assert((await page.locator('#spending-optimizer-results').textContent()).includes('$1,000.00 more per month'));

  assert((await page.locator('#spending-optimizer-results').textContent()).includes('Cuts occurred in 250 of 1,000 futures (25.00%).'));
  assert((await page.locator('#spending-optimizer-results').textContent()).includes('median first cut was in plan year 3'));
  assert((await page.locator('#spending-optimizer-results').textContent()).includes('depletion in 120 of 1,000 futures; in 90 of those'));
  assert((await page.locator('#spending-optimizer-results').textContent()).includes('planned change is separate from below-plan cuts'));
  assert((await page.locator('#spending-optimizer-results').textContent()).includes('No cuts observed across 1,000 checked futures.'));
  await screenshot('task5-results-desktop-light.png', '[data-spending-outcome]');

  const nonHeadlineApply = page.locator('[data-spending-frontier="planned"] [data-spending-apply="planned-8250"]');
  await nonHeadlineApply.focus();
  await nonHeadlineApply.press('Enter');
  await page.waitForFunction(() => document.querySelector('#spending-optimizer-status').textContent.includes('saved'));
  const applyRequest = await page.evaluate(() => window.requests.find(request => request.url.endsWith('/apply')));
  assert.equal(applyRequest.kind, 'URLSearchParams');

  await page.locator('[data-spending-graph="flex-9000"]').first().click();
  await page.waitForFunction(() => !document.querySelector('#spending-graph-preview').hidden);
  assert.equal(await page.locator('#spending-graph-heading').textContent(), 'Evidence for Flexible spending at $9,000.00 per month');
  assert(!(await page.locator('#spending-graph-heading').textContent()).includes('flex-9000'));
  assert.deepEqual(await page.evaluate(() => document.querySelector('#spending-graph-main').data[2].y), [9000, 9100]);
  assert((await page.locator('#spending-graph-data').textContent()).includes('1,000'));
  const lightGrid = await gridContrast();
  assert(lightGrid.ratio >= 3, JSON.stringify(lightGrid));
  await screenshot('task5-spending-graph-desktop-light.png', '#spending-graph-heading');
  await page.locator('#spending-graph-view').selectOption('worst');
  await page.waitForFunction(() => document.querySelector('#spending-graph-summary').textContent.includes('lowest monthly funded living'));
  assert((await page.locator('#spending-graph-summary').textContent()).includes('$6,000.00'));
  assert((await page.locator('#spending-graph-summary').textContent()).includes("today's dollars"));
  await page.locator('#spending-graph-dollars').selectOption('nominal');
  await page.waitForFunction(() => document.querySelector('#spending-graph-data caption').textContent.includes('nominal dollars'));
  assert((await page.locator('#spending-graph-summary').textContent()).includes("$6,000.00 in today's dollars"));
  assert((await page.locator('#spending-graph-data').textContent()).includes('Omitted in nominal view'));
  await page.locator('#spending-graph-dollars').selectOption('real');
  await page.locator('#spending-graph-view').selectOption('timeline');
  assert((await page.locator('#spending-graph-note').textContent()).includes('Later base-case months are unavailable'));
  assert((await page.locator('#spending-graph-data').textContent()).includes('Pension starts'));
  assert((await page.locator('#spending-graph-data').textContent()).includes('Social Security starts'));
  assert((await page.locator('#spending-graph-data').textContent()).includes('partial'));
  const monthlyTable = page.locator('#spending-graph-data table').filter({hasText: "Observed calendar-month amounts — today's dollars"});
  assert.equal(await monthlyTable.locator('tbody tr').count(), 12);
  assert((await monthlyTable.textContent()).includes('2026-12'));
  assert((await monthlyTable.textContent()).includes('2027-03'));
  assert((await monthlyTable.textContent()).includes('$1,800.00'));
  assert((await monthlyTable.textContent()).includes('$9,500.00'));
  assert((await monthlyTable.textContent()).includes('$9,000.00'));
  assert(!(await monthlyTable.textContent()).includes('2027-09'));
  const timelineRows = await page.evaluate(() => {
    const tables = Array.from(document.querySelectorAll('#spending-graph-data table'));
    const rowCells = (caption, rowLabel) => {
      const table = tables.find(candidate => candidate.caption.textContent.trim() === caption);
      if (!table) throw new Error(`Missing timeline table: ${caption}`);
      const row = Array.from(table.tBodies[0].rows).find(candidate => candidate.cells[0].textContent.trim() === rowLabel);
      if (!row) throw new Error(`Missing timeline row: ${rowLabel}`);
      return Array.from(row.cells, cell => cell.textContent.trim());
    };
    return {
      february: rowCells("Observed calendar-month amounts — today's dollars", '2027-02'),
      march: rowCells("Observed calendar-month amounts — today's dollars", '2027-03'),
      annual2027: rowCells("Calendar-year average monthly amounts — today's dollars", '2027 (partial)')
    };
  });
  assert.deepEqual(timelineRows.february, ['2027-02', '$0.00', '$1,000.00', '$2,000.00', '$0.00', '$0.00', '$500.00', '$9,500.00', '$9,000.00', '$300.00']);
  assert.deepEqual(timelineRows.march, ['2027-03', '$1,800.00', '$1,000.00', '$2,000.00', '$0.00', '$0.00', '$500.00', '$9,000.00', '$8,900.00', '$300.00']);
  assert.deepEqual(timelineRows.annual2027, ['2027 (partial)', '8', '$1,350.00', '$1,000.00', '$2,000.00', '$0.00', '$0.00', '$500.00', '$9,125.00', '$8,925.00', '$300.00']);
  assert((await page.locator('#spending-graph-data').textContent()).includes('Early-spending boost stops'));
  assert.deepEqual(await page.evaluate(() => document.querySelector('#spending-graph-main').layout.shapes.map(shape => Number(shape.x0.toFixed(6)))), [2026.916667, 2027.166667, 2027.166667]);
  assert(await page.evaluate(() => document.querySelector('#spending-graph-main').layout.annotations.some(annotation => annotation.text.includes('Social Security starts<br>Early-spending boost stops'))));
  await page.locator('#spending-graph-data').evaluate(node => { node.closest('details').open = true; });
  await screenshot('task5-monthly-timeline-table-desktop-light.png', '#spending-graph-data caption');
  await screenshot('task5-timeline-2027-evidence-desktop-light.png', '#spending-graph-data table:nth-of-type(2) tbody tr:last-child');
  await page.locator('#spending-graph-view').selectOption('portfolio');
  assert.deepEqual(await page.evaluate(() => document.querySelector('#spending-graph-main').data[2].y), [500000, 450000]);
  await page.locator('#spending-graph-view').selectOption('base');
  assert.deepEqual(await page.evaluate(() => document.querySelector('#spending-graph-main').data[0].y), [500000, 450000]);
  await page.locator('#spending-graph-view').selectOption('timeline');

  const requestCount = await page.evaluate(() => window.requests.length);
  await page.locator('#spending-graph-view').focus();
  await page.evaluate(() => document.documentElement.classList.add('dark'));
  await page.waitForFunction(() => document.querySelector('#spending-graph-main').data[0].line.color === '#f87171');
  const darkGrid = await gridContrast();
  assert(darkGrid.ratio >= 3, JSON.stringify(darkGrid));
  assert.equal(await page.evaluate(() => window.requests.length), requestCount);
  assert.equal(await page.evaluate(() => document.activeElement.id), 'spending-graph-view');
  await screenshot('task5-timeline-desktop-dark.png', '#spending-graph-heading');

  await page.evaluate(() => { window.graphDelay = 180; });
  await page.locator('#spending-graph-view').selectOption('spending');
  await page.locator('[data-spending-graph="flex-9000"]').first().click();
  await page.waitForTimeout(25);
  await page.evaluate(() => document.documentElement.classList.remove('dark'));
  await page.waitForFunction(() => document.querySelector('#spending-optimizer-status').textContent === 'Evidence loaded. Nothing was saved.');
  assert.equal(await page.evaluate(() => document.querySelector('#spending-graph-main').data[0].line.color), '#1d4ed8');
  await page.evaluate(() => { window.graphDelay = 0; });

  await page.locator('#spending-boost-details > summary').click();
  await page.locator('#spending-boost-enabled').check();
  assert(!(await page.locator('#spending-boost-required').evaluate(node => node.hidden)));
  await page.locator('#spending-boost-amount').fill('500');
  await page.locator('#spending-optimizer-run').click();
  assert.equal(await page.evaluate(() => window.requests.filter(request => request.url.endsWith('/optimize')).length), 1);
  await page.locator('#spending-boost-stop').fill('2026-09');
  assert((await page.locator('#spending-boost-boundary').textContent()).includes('after the plan start month'));
  await page.locator('#spending-boost-stop').fill('2057-01');
  assert((await page.locator('#spending-boost-boundary').textContent()).includes('outside the modeled horizon'));
  await page.locator('#spending-boost-stop').fill('2031-09');
  assert.equal(await page.locator('[data-spending-headline]').count(), 0);
  assert((await page.locator('#spending-optimizer-status').textContent()).includes('Inputs changed'));
  await page.locator('#spending-optimizer-run').click();
  await page.waitForSelector('[data-spending-outcome]');
  await page.locator('#spending-boost-amount').fill('501');
  assert.equal(await page.locator('[data-spending-outcome]').count(), 0);
  assert((await page.locator('#spending-optimizer-status').textContent()).includes('Inputs changed'));
  await page.locator('#spending-boost-amount').fill('500');
  await page.locator('#spending-optimizer-run').click();
  await page.waitForSelector('[data-spending-outcome]');
  await page.locator('#spending-boost-stop').fill('2031-10');
  assert.equal(await page.locator('[data-spending-outcome]').count(), 0);
  assert((await page.locator('#spending-optimizer-status').textContent()).includes('Inputs changed'));

  await minimum.fill('7000');
  await page.evaluate(() => { window.searchDelay = 250; });
  await page.locator('#spending-optimizer-run').click();
  await page.waitForTimeout(30);
  await minimum.fill('7100');
  await page.waitForTimeout(300);
  assert.equal(await page.locator('[data-spending-outcome]').count(), 0);
  await minimum.fill('7000');
  await page.locator('#spending-optimizer-run').click();
  await page.waitForFunction(() => !document.querySelector('#spending-optimizer-cancel').disabled);
  await page.locator('#spending-optimizer-cancel').click();
  assert((await page.locator('#spending-optimizer-status').textContent()).includes('cancelled'));
  assert(!(await page.locator('#spending-effective-range').textContent()).includes('Validating'));
  assert((await page.locator('#spending-effective-range').textContent()).includes('idle'));
  assert(await page.evaluate(() => window.requests.some(request => request.url.endsWith('/cancel'))));

  await page.evaluate(() => {
    const result = document.querySelector('#spending-optimizer-results');
    result.innerHTML = window.fixtureNilResults;
  });
  assert((await page.locator('#spending-optimizer-results').textContent()).includes('Unavailable'));
  assert(!(await page.locator('#spending-optimizer-results').textContent()).includes('0.00%'));
  assert(!(await page.locator('#spending-optimizer-results').textContent()).includes('0 / 0'));
  await page.evaluate(() => {
    const result = document.querySelector('#spending-optimizer-results');
    result.innerHTML = window.fixtureNoResults;
  });
  const noResultText = await page.locator('#spending-optimizer-results').textContent();
  assert(noResultText.includes('No spending option met your minimum'));
  assert(noResultText.includes('Below the minimum in 5 of 1,000 futures'));
  assert(noResultText.includes('Search reached the upper range'));

  await page.setViewportSize({width: 375, height: 800});
  await page.evaluate(() => { document.documentElement.classList.add('dark'); window.searchDelay = 0; });
  await minimum.fill('7001');
  await minimum.fill('7000');
  await page.locator('#spending-optimizer-run').click();
  await page.waitForSelector('[data-spending-outcome]');
  await page.waitForFunction(() => document.querySelector('#spending-optimizer-status').textContent.includes('Comparison complete'));
  await page.waitForFunction(() => document.querySelector('#spending-effective-range').textContent.includes('$6,500.00'));
  assert(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth));
  assert(await page.evaluate(() => [...document.querySelectorAll('[data-spending-scroll]')].every(node => node.scrollWidth >= node.clientWidth)));
  assert(!(await page.locator('#spending-optimizer-status').textContent()).includes('cancelled'));
  assert(!(await page.locator('#spending-effective-range').textContent()).includes('Validating'));
  await screenshot('task5-results-mobile-dark.png', '[data-spending-outcome]');
  await page.locator('[data-spending-graph="flex-9000"]').first().click();
  await page.waitForFunction(() => !document.querySelector('#spending-graph-preview').hidden);
  await page.locator('#spending-graph-view').selectOption('timeline');
  await page.waitForFunction(() => document.querySelector('#spending-graph-note').textContent.includes('Later base-case months are unavailable'));
  assert(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth));
  assert(await page.evaluate(() => {
    const plot = document.querySelector('#spending-graph-main');
    const panel = document.querySelector('#spending-graph-preview');
    return plot.getBoundingClientRect().width <= panel.getBoundingClientRect().width - 20;
  }));
  await screenshot('task5-timeline-mobile-dark.png', '#spending-graph-heading');
  await page.evaluate(() => { window.graphDelay = 180; window.applyDelay = 180; });
  const cleanupBefore = await page.evaluate(() => ({...window.cleanupStats, cancels: window.requests.filter(request => request.url.endsWith('/cancel')).length, graphs: window.requests.filter(request => request.url.endsWith('/graph')).length, applies: window.requests.filter(request => request.url.endsWith('/apply')).length}));
  await page.locator('[data-spending-apply="planned-8250"]').first().click();
  await page.waitForFunction(applies => window.requests.filter(request => request.url.endsWith('/apply')).length > applies, cleanupBefore.applies);
  await page.locator('[data-spending-graph="flex-9000"]').first().click();
  await page.waitForFunction(graphs => window.requests.filter(request => request.url.endsWith('/graph')).length > graphs, cleanupBefore.graphs);
  await page.evaluate(formMarkup => {
    const oldRoot = document.querySelector('#spending-optimizer');
    oldRoot.dispatchEvent(new CustomEvent('htmx:beforeCleanupElement', {bubbles: true, detail: {elt: oldRoot}}));
    oldRoot.outerHTML = formMarkup;
    const replacement = document.querySelector('#spending-optimizer');
    document.body.dispatchEvent(new CustomEvent('htmx:afterSwap', {detail: {target: replacement}}));
    document.body.dispatchEvent(new CustomEvent('htmx:afterSwap', {detail: {target: replacement}}));
  }, fixture.form);
  await page.waitForTimeout(220);
  const cleanupAfter = await page.evaluate(() => ({...window.cleanupStats, cancels: window.requests.filter(request => request.url.endsWith('/cancel')).length, lastGraphAborted: window.requests.filter(request => request.url.endsWith('/graph')).at(-1).aborted, lastApplyAborted: window.requests.filter(request => request.url.endsWith('/apply')).at(-1).aborted, ready: document.querySelector('#spending-optimizer').dataset.spendingReady}));
  assert(cleanupAfter.mutationDisconnects > cleanupBefore.mutationDisconnects);
  assert(cleanupAfter.resizeDisconnects > cleanupBefore.resizeDisconnects);
  assert(cleanupAfter.plotPurges > cleanupBefore.plotPurges);
  assert(cleanupAfter.cancels > cleanupBefore.cancels);
  assert(cleanupAfter.lastGraphAborted);
  assert(cleanupAfter.lastApplyAborted);
  assert.equal(cleanupAfter.ready, 'true');
  const prepareBeforeReplacementEdit = await page.evaluate(() => window.requests.filter(request => request.url.endsWith('/prepare')).length);
  await page.locator('#spending-minimum').fill('7200');
  await page.waitForTimeout(300);
  assert.equal(await page.evaluate(() => window.requests.filter(request => request.url.endsWith('/prepare')).length), prepareBeforeReplacementEdit + 1);
  assert.deepEqual(errors, []);
  if (process.env.SCREENSHOT_PATH) await page.screenshot({path: process.env.SCREENSHOT_PATH, fullPage: true});
  await browser.close();
  console.log(`PASS: server fixture, encoded requests, monthly timeline, independent evidence, nonheadline keyboard Apply, stale/cancel, root cleanup, graphs, theme, mobile overflow; grid contrast light=${lightGrid.ratio.toFixed(2)} dark=${darkGrid.ratio.toFixed(2)}`);
})().catch(error => { console.error(error); process.exit(1); });
