const fs = require('node:fs');
const assert = require('node:assert/strict');
const { chromium } = require(process.env.DI5_PLAYWRIGHT || '/home/darrell/.npm/_npx/e41f203b7505f1fb/node_modules/playwright');
const base = process.env.DI5_BASE;
const u=new URL(base);assert.equal(u.hostname,'127.0.0.1');assert.ok(u.port&&!['8080','8081'].includes(u.port));
const output = process.env.DI5_OUTPUT;assert.ok(output);fs.mkdirSync(output,{recursive:true});
(async () => {
 const browser = await chromium.launch({headless:true, executablePath:process.env.DI5_CHROME || '/home/darrell/.cache/ms-playwright/chromium-1243/chrome-linux64/chrome', args:['--no-sandbox']});
 try {
  for (const dark of [false,true]) for (const width of [390,768,1440]) {
   const context = await browser.newContext({viewport:{width,height:900}, reducedMotion:'reduce', colorScheme:dark?'dark':'light'});
   const page = await context.newPage();
   await page.route('**/*', route => new URL(route.request().url()).origin === base && ['GET','HEAD'].includes(route.request().method()) ? route.continue() : route.abort());
   await page.goto(base+'/dashboard?start=2026-01-01&end=2026-08-28');
   await page.evaluate(d => {document.documentElement.classList.toggle('dark',d);window.dispatchEvent(new Event('themechange'));}, dark);
   await page.waitForFunction(() => document.querySelector('#chart-major-expense')?.data?.length);
   await page.waitForTimeout(300);
   const chartTables = await page.locator('[data-dashboard-chart-table]').count();
   const dimensions = await page.evaluate(() => ({width:innerWidth,scroll:document.documentElement.scrollWidth,overflow:[...document.querySelectorAll('main *')].filter(e=>e.getBoundingClientRect().right>innerWidth+1).slice(0,12).map(e=>e.id||e.tagName)}));
   const figures = await page.locator('#dashboard-overview').innerText();
   const headings = await page.locator('h1').count();
   await page.screenshot({path:output+`/DI5-dashboard-${dark?'dark':'light'}-${width}.png`,fullPage:true});
   assert.equal(dimensions.scroll,width,JSON.stringify(dimensions));
   assert.equal(headings,1);
   assert.equal(await page.locator('#dashboard-overview h2').count(),4);
   assert.equal(chartTables,5);
   const readChartParity=()=>page.evaluate(()=>[...document.querySelectorAll(".chart-container[data-chart-url]")].map(p=>{
    const expected=[];
    for(const t of p.data){
     const labels=t.labels || (t.orientation==="h"?t.y:t.x) || [];
     const values=t.values || (t.orientation==="h"?t.x:t.y) || [];
     labels.forEach((x,i)=>{if(values[i]!=null)expected.push([t.name||"Spending",String(x),String(values[i])]);});
    }
    for(const s of p.layout.shapes||[])if(s.type==="line"&&s.y0===s.y1&&s.yref==="y")expected.push(["Target","Across plotted months",String(s.y0)]);
    const actual=[...document.getElementById(p.id+"-data-table").querySelectorAll("tbody tr")].map(r=>[...r.cells].map(c=>c.textContent));
    return {id:p.id,expected,actual};
   }));
   const chartParity=await readChartParity();
   for(const p of chartParity)assert.deepEqual(p.actual,p.expected,p.id+" table/plot parity");
   console.log(JSON.stringify({dark,width,chartParity:chartParity.map(p=>({id:p.id,rows:p.actual.length}))}));
   for(const kind of ['income','expenses','savings','living','healthcare']) {
   const button=page.locator('[data-kpi-detail="'+kind+'"]');
   await button.focus(); await page.keyboard.press('Enter');
   await page.getByRole('dialog').waitFor();
   assert.ok(await page.getByRole('dialog').evaluate(e=>e.contains(document.activeElement)));
   for(let i=0;i<18;i++){await page.keyboard.press(i<9?'Tab':'Shift+Tab');assert.ok(await page.getByRole('dialog').evaluate(e=>e.contains(document.activeElement)));}
   await page.keyboard.press('Escape');
   assert.equal(await page.getByRole('dialog').count(),0);
   assert.ok(await button.evaluate(e=>document.activeElement===e));
   }
   for(const d of await page.locator('[data-dashboard-chart-table]').all()) {
    await d.locator('summary').focus();await page.keyboard.press('Enter');
    assert.ok(await d.locator('table').isVisible());
    assert.equal(await d.locator('th:not([scope])').count(),0);
    const region=d.locator('div.overflow-auto');assert.equal(await region.count(),1);
    if(await region.evaluate(e=>e.scrollWidth>e.clientWidth)) {
      await page.keyboard.press('Tab');assert.ok(await region.evaluate(e=>e===document.activeElement || e.contains(document.activeElement)));
      const before=await region.evaluate(e=>e.scrollLeft);await page.keyboard.press('ArrowRight');await page.waitForTimeout(200);assert.ok(await region.evaluate((e,b)=>e.scrollLeft>b,before));
    }
   }
   assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth),width,'open disclosures overflow page');
   const request=page.waitForResponse(r=>r.url().includes('/dashboard/kpis?'));
   const charts=Promise.all(['major-expense','spending-trend','merchants','cumulative','budget-vs-actual'].map(kind=>page.waitForResponse(r=>r.url().includes('/dashboard/charts/data/'+kind+'?')&&r.url().includes('start=2026-08-01'))));
   await page.locator('#dashboard-date-start').fill('2026-08-01');await page.locator('#dashboard-date-start').dispatchEvent('change');await request;
   await page.waitForFunction(()=>document.querySelector('#kpis-container a[href^="/insights?"]')?.href.includes('start=2026-08-01'));
   assert.ok(await page.locator('#dashboard-date-start').evaluate(e=>document.activeElement===e));
   await page.waitForFunction(()=>document.querySelectorAll('[data-dashboard-chart-table]').length===5);
   await charts;await page.waitForTimeout(300);
   for(const p of await readChartParity())assert.deepEqual(p.actual,p.expected,p.id+" post-HTMX table/plot parity");
   const evidence={dark,width,dimensions,figures,chartTables,keyboard:'five dialogs open/forward-backward trap/Escape/return; open disclosure overflow/scroll; selected-range HTMX focus and Insights link'};
   fs.writeFileSync(output+`/DI5-dashboard-${dark?'dark':'light'}-${width}.json`,JSON.stringify(evidence,null,2));
   console.log(JSON.stringify(evidence));
   await context.close();
  }
 } finally {await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
