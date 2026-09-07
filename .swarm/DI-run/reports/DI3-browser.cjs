const fs = require('node:fs');
const assert = require('node:assert/strict');
const { chromium } = require('/home/darrell/.npm/_npx/e41f203b7505f1fb/node_modules/playwright');
const base = 'http://127.0.0.1:18773';
const output = process.env.DI3_EVIDENCE || '/tmp/budget2-DI3-visual.eCMIJ3';
(async () => {
 const browser = await chromium.launch({headless:true, executablePath:'/home/darrell/.cache/ms-playwright/chromium-1243/chrome-linux64/chrome', args:['--no-sandbox']});
 try {
  for (const dark of [false,true]) for (const width of [390,768,1440]) {
   const context = await browser.newContext({viewport:{width,height:900}, reducedMotion:'reduce', colorScheme:dark?'dark':'light'});
   const page = await context.newPage();
   await page.route('**/*', route => new URL(route.request().url()).origin === base ? route.continue() : route.abort());
   await page.goto(base+'/dashboard?start=2026-01-01&end=2026-08-28');
   await page.evaluate(d => {document.documentElement.classList.toggle('dark',d);window.dispatchEvent(new Event('themechange'));}, dark);
   await page.waitForFunction(() => document.querySelector('#chart-major-expense')?.data?.length);
   await page.waitForTimeout(300);
   const chartTables = await page.locator('[data-dashboard-chart-table]').count();
   if (process.env.DI3_A11Y) {
    await page.addScriptTag({path:'/tmp/claude-1000/-home-darrell-work-agents2/fdc3d3ad-6feb-42fa-95ef-64e528e5cac8/scratchpad/a11y-u12/node_modules/axe-core/axe.min.js'});
    const result=await page.evaluate(async()=>await axe.run(document,{runOnly:{type:'tag',values:['wcag2a','wcag2aa','wcag21aa','wcag22aa']}}));
    fs.writeFileSync(output+`/DI3-axe-${dark?'dark':'light'}-${width}.json`,JSON.stringify(result,null,2));
    console.log(JSON.stringify({dark,width,violations:result.violations.map(v=>({id:v.id,nodes:v.nodes.map(n=>({target:n.target,summary:n.failureSummary}))}))}));
    await context.close();continue;
   }
   if (process.env.DI3_RED) {
    assert.ok(chartTables>=2, 'Decision charts need adjacent data tables');
    const disclosure=page.locator('#chart-budget-vs-actual-data-table');
    await disclosure.locator('summary').focus();await page.keyboard.press('Enter');
    assert.ok((await disclosure.innerText()).includes('Target'), 'Budget chart alternative must include its target line');
    await context.close();continue;
   }
   const dimensions = await page.evaluate(() => ({width:innerWidth,scroll:document.documentElement.scrollWidth,overflow:[...document.querySelectorAll('main *')].filter(e=>e.getBoundingClientRect().right>innerWidth+1).slice(0,12).map(e=>e.id||e.tagName)}));
   const figures = await page.locator('#dashboard-overview').innerText();
   const headings = await page.locator('h1').count();
   await page.screenshot({path:output+`/DI3-${dark?'dark':'light'}-${width}.png`,fullPage:true});
   assert.equal(dimensions.scroll,width,JSON.stringify(dimensions));
   assert.equal(headings,1);
   assert.equal(await page.locator('#dashboard-overview h2').count(),4);
   assert.ok(chartTables>=2);
   const button=page.getByRole('button',{name:'Cash-flow details',exact:true});
   await button.focus(); await page.keyboard.press('Enter');
   await page.getByRole('dialog').waitFor();
   assert.ok(await page.getByRole('dialog').evaluate(e=>e.contains(document.activeElement)));
   for(let i=0;i<18;i++){await page.keyboard.press('Tab');assert.ok(await page.getByRole('dialog').evaluate(e=>e.contains(document.activeElement)));}
   await page.keyboard.press('Escape');
   assert.equal(await page.getByRole('dialog').count(),0);
   assert.ok(await button.evaluate(e=>document.activeElement===e));
   const picker=page.locator('#drop-zone');await picker.focus();
   const chooser=page.waitForEvent('filechooser');await page.keyboard.press('Enter');await (await chooser).setFiles([]);
   const request=page.waitForResponse(r=>r.url().includes('/dashboard/kpis?'));
   await page.locator('#dashboard-date-start').fill('2026-08-01');await page.locator('#dashboard-date-start').dispatchEvent('change');await request;
   await page.waitForFunction(()=>document.querySelector('#kpis-container a[href^="/insights?"]')?.href.includes('start=2026-08-01'));
   assert.ok(await page.locator('#dashboard-date-start').evaluate(e=>document.activeElement===e));
   const evidence={dark,width,dimensions,figures,chartTables,keyboard:'modal open/trap/Escape/return; import picker; selected-range HTMX focus and Insights link'};
   fs.writeFileSync(output+`/DI3-${dark?'dark':'light'}-${width}.json`,JSON.stringify(evidence,null,2));
   console.log(JSON.stringify(evidence));
   await context.close();
  }
 } finally {await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
