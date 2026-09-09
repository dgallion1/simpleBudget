// GV2 attempt 2 browser regression (ruling GV-2026-09-09d): checker-a11y's
// FAIL came from a manual contrast walk because axe cannot see Plotly's SVG
// strokes. This probe closes that gap with a REAL browser: it loads the
// isolated test server's /whatif page (guardrails already enabled by the Go
// harness before this script runs), reads the ACTUAL rendered SVG legend
// swatch colors of each tone trace (not just the JS data model that set
// them), computes WCAG contrast against the ACTUAL computed card background
// via getComputedStyle, in light mode; then toggles the app theme (the
// `dark` class on <html>, driven through the real #theme-toggle button) and
// repeats. It also asserts #chart-projection stays >=540px tall after the
// theme toggle and after switching tabs away (Risk) and back (Overview).
//
// Run only against an isolated synthetic server, never main or demo:
// BUDGET2_TEST_URL=http://127.0.0.1:PORT PLAYWRIGHT_MODULE=/path/to/playwright
// CHROMIUM_EXECUTABLE=/path/to/chrome node cmd/server/testdata/guardrail_tone_contrast_browser.cjs
const assert = require('node:assert/strict');
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const base = new URL(process.env.BUDGET2_TEST_URL || '');
assert(['localhost', '127.0.0.1'].includes(base.hostname),
    'Use the isolated synthetic test server, never main or demo.');

// Tone traces that must be present whenever guardrails are enabled,
// regardless of whether any GuardrailEvent actually fired (the markers
// trace is event-dependent and is not required here; its colors are the
// SAME hex values as these lines and are covered exhaustively, numerically,
// by TestBuildProjectionChartDataTraceTonesAndContrast in
// internal/handlers/whatif/gv2_chart_test.go).
const TONE_LINE_TRACES = ['Cut trigger', 'Raise trigger', 'Planned', 'After guardrails'];

function parseRgb(rgbString) {
    const m = /rgba?\(\s*([\d.]+)\s*,\s*([\d.]+)\s*,\s*([\d.]+)/.exec(rgbString || '');
    if (!m) return null;
    return [Number(m[1]), Number(m[2]), Number(m[3])];
}

function srgbToLinear(c) {
    c = c / 255;
    return c <= 0.04045 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4);
}

function relativeLuminance([r, g, b]) {
    return 0.2126 * srgbToLinear(r) + 0.7152 * srgbToLinear(g) + 0.0722 * srgbToLinear(b);
}

function contrastRatio(rgb1, rgb2) {
    let l1 = relativeLuminance(rgb1);
    let l2 = relativeLuminance(rgb2);
    if (l1 < l2) [l1, l2] = [l2, l1];
    return (l1 + 0.05) / (l2 + 0.05);
}

async function legendSwatchRgb(page, name) {
    return await page.evaluate((traceName) => {
        const groups = Array.from(document.querySelectorAll('#chart-projection .legend .traces'));
        for (const g of groups) {
            const text = g.querySelector('.legendtext');
            if (text && text.textContent.trim() === traceName) {
                const lineEl = g.querySelector('.legendlines path, .legendlines line');
                if (lineEl) return getComputedStyle(lineEl).stroke;
                const ptEl = g.querySelector('.legendpoints path');
                if (ptEl) return getComputedStyle(ptEl).fill;
                return null;
            }
        }
        return null;
    }, name);
}

async function cardBackgroundRgb(page) {
    return await page.evaluate(() => {
        const chart = document.getElementById('chart-projection');
        const card = chart.closest('[data-whatif-projection-card]');
        return getComputedStyle(card).backgroundColor;
    });
}

async function assertAllTonesContrast(page, stage) {
    const bg = parseRgb(await cardBackgroundRgb(page));
    assert(bg, `${stage}: could not read card background color`);
    for (const name of TONE_LINE_TRACES) {
        const swatch = await legendSwatchRgb(page, name);
        const rgb = parseRgb(swatch);
        assert(rgb, `${stage}: could not read legend swatch color for "${name}" (got ${swatch})`);
        const ratio = contrastRatio(rgb, bg);
        assert(ratio >= 3.0, `${stage}: "${name}" contrast ${ratio.toFixed(2)}:1 vs card bg rgb(${bg.join(',')}) — want >= 3:1 (swatch ${swatch})`);
    }
}

(async () => {
    const browser = await chromium.launch({
        headless: true,
        executablePath: process.env.CHROMIUM_EXECUTABLE,
        args: ['--no-sandbox'],
    });
    try {
        // #theme-toggle (desktop) is hidden below Tailwind's xl breakpoint
        // (1280px) in favor of #theme-toggle-mobile; use a wide viewport so
        // the real desktop control is the one this probe drives.
        const page = await browser.newPage({ viewport: { width: 1400, height: 900 } });
        await page.goto(new URL('/whatif', base).href, { waitUntil: 'load' });
        await page.waitForFunction(() => {
            const el = document.getElementById('chart-projection');
            return el && el.data && el.data.length > 0;
        }, { timeout: 20000 });
        // Guardrails must actually be enabled (two-panel chart) for this
        // probe to be checking anything — fail loudly, not silently, if the
        // harness's settings seed didn't take.
        const hasCutTrigger = await page.evaluate(() =>
            (document.getElementById('chart-projection').data || []).some(t => t.name === 'Cut trigger'));
        assert(hasCutTrigger, 'guardrails were not enabled on the seeded test settings — fixture defect');

        await assertAllTonesContrast(page, 'light');

        const heightAfterLight = await page.evaluate(() =>
            document.getElementById('chart-projection').getBoundingClientRect().height);
        assert(heightAfterLight >= 540, `light: #chart-projection height ${heightAfterLight} < 540`);

        // Toggle to dark mode via the real control, then re-render settles.
        await page.locator('#theme-toggle').click();
        await page.waitForFunction(() => document.documentElement.classList.contains('dark'));
        await page.waitForFunction(() => {
            const el = document.getElementById('chart-projection');
            return el && el._fullLayout;
        });
        // Let the themechange restyle (Plotly.restyle calls) settle.
        await page.waitForTimeout(150);

        await assertAllTonesContrast(page, 'dark');

        const heightAfterDark = await page.evaluate(() =>
            document.getElementById('chart-projection').getBoundingClientRect().height);
        assert(heightAfterDark >= 540, `dark: #chart-projection height ${heightAfterDark} < 540`);

        // Switch tabs away (Risk) and back (Overview); the tab-activation
        // resize path (whatif-tabs.js resizeChartsIn) must not collapse the
        // two-panel chart back to a single-panel height.
        await page.locator('#wf-tab-risk').click();
        await page.waitForFunction(() => document.getElementById('wf-panel-risk').getAttribute('class').indexOf('hidden') === -1);
        await page.locator('#wf-tab-overview').click();
        await page.waitForFunction(() => document.getElementById('wf-panel-overview').getAttribute('class').indexOf('hidden') === -1);
        await page.waitForTimeout(150);

        const heightAfterTabs = await page.evaluate(() =>
            document.getElementById('chart-projection').getBoundingClientRect().height);
        assert(heightAfterTabs >= 540, `after tab switch: #chart-projection height ${heightAfterTabs} < 540`);

        await browser.close();
        console.log('PASS: tone contrast (light+dark) and chart height after theme toggle + tab switch');
    } catch (error) {
        await browser.close().catch(() => {});
        console.error(error);
        process.exit(1);
    }
})().catch((error) => {
    console.error(error);
    process.exit(1);
});
