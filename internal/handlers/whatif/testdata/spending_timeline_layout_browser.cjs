// Long-horizon layout oracle using an actual captured copied-data graph.
// Generate the evidence with spending_optimizer_live_browser.cjs first.
const fs=require('node:fs'),os=require('node:os'),path=require('node:path'),assert=require('node:assert/strict'),child=require('node:child_process');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE);
(async()=>{
 const evidence=JSON.parse(fs.readFileSync(process.env.SPENDING_TIMELINE_EVIDENCE||'.superpowers/sdd/2026-09-10-spending-first-optimizer/task-6-browser/evidence.json','utf8'));
 const graph=evidence.graph;assert(graph.funding_timeline.annual_averages.length>=35,'needs actual long-horizon evidence');
 const fixturePath=path.join(os.tmpdir(),`spending-layout-${process.pid}.json`);
 const generated=child.spawnSync('rtk',['proxy','go','test','./internal/handlers/whatif','-run','^TestSpendingOptimizerBrowserFixture$','-count=1'],{encoding:'utf8',env:{...process.env,SPENDING_BROWSER_FIXTURE:fixturePath}});
 assert.equal(generated.status,0,generated.stdout+generated.stderr);const fixture=JSON.parse(fs.readFileSync(fixturePath,'utf8'));fs.unlinkSync(fixturePath);
 const out=process.env.SCREENSHOT_DIR||'.superpowers/sdd/2026-09-10-spending-first-optimizer/task-6-layout';fs.mkdirSync(out,{recursive:true});
 const browser=await chromium.launch({headless:true,executablePath:process.env.CHROMIUM_PATH,args:['--no-sandbox']});
 try{
  const page=await browser.newPage({viewport:{width:375,height:1000}});const errors=[];page.on('pageerror',e=>errors.push(e.message));
  await page.setContent(`<html><body class="bg-gray-100 dark:bg-gray-900"><main><div class="bg-white dark:bg-gray-800">${fixture.form}</div></main></body></html>`);
  await page.addStyleTag({path:'web/static/css/tailwind.css'});await page.addStyleTag({path:'web/static/css/styles.css'});await page.addScriptTag({path:process.env.PLOTLY_PATH});await page.addScriptTag({path:'web/static/js/charts.js'});
  await page.evaluate(({graph,results})=>{window.htmx={process(){}};window.fetch=async url=>{if(url.endsWith('/graph'))return {ok:true,json:async()=>graph};if(url.endsWith('/cancel'))return {ok:true};throw Error('Unexpected request '+url)};document.querySelector('#spending-optimizer-results').innerHTML=results},{graph,results:fixture.results});
  await page.addScriptTag({path:'web/static/js/whatif-spending-optimizer.js'});
  await page.locator('[data-spending-graph]').first().click();await page.waitForFunction(()=>!document.querySelector('#spending-graph-preview').hidden);
  for(const view of ['spending','worst','portfolio','timeline']){
   const timeline=view==='timeline';const expectedYears=timeline?graph.funding_timeline.annual_averages.map(r=>r.calendar_year):view==='worst'?graph.worst.years:graph.simulated.years;
   const expectedY=timeline?graph.funding_timeline.annual_averages.map(r=>r.taxes_paid_real):view==='worst'?graph.worst.LivingP50:view==='portfolio'?graph.simulated.PortfolioP10:graph.simulated.LivingP10;
   await page.locator('#spending-graph-view').selectOption(view);
   await page.waitForFunction(({years,values,title})=>{const p=document.querySelector('#spending-graph-main');return JSON.stringify(p?.data?.[0].x)===JSON.stringify(years)&&JSON.stringify(p?.data?.[0].y)===JSON.stringify(values)&&p?._fullLayout?.xaxis.title.text===title},{years:expectedYears,values:expectedY,title:timeline?'Calendar year':'Plan year'});
   for(const width of [375,650])for(const dark of [false,true]){
    await page.setViewportSize({width,height:1000});await page.evaluate(dark=>document.documentElement.classList.toggle('dark',dark),dark);
    await page.waitForFunction(()=>Math.abs(document.querySelector('#spending-graph-main')._fullLayout.width-document.querySelector('#spending-graph-main').clientWidth)<3);
    const rendered=await page.evaluate(()=>{const p=document.querySelector('#spending-graph-main');return {years:p.data[0].x,values:p.data[0].y,ticks:[...p.querySelectorAll('.xaxislayer-above .xtick text')].map(e=>{const r=e.getBoundingClientRect();return {text:e.textContent,left:r.left,right:r.right}}),monthRows:document.querySelector('#spending-graph-data table').tBodies[0].rows.length,yearRows:document.querySelectorAll('#spending-graph-data table')[1]?.tBodies[0].rows.length}});
    await page.locator('#spending-graph-main').evaluate(e=>e.scrollIntoView({block:'start'}));await page.screenshot({path:path.join(out,`${view}-${width}-${dark?'dark':'light'}.png`)});
    assert(rendered.ticks.length>=2 && rendered.ticks.length<=10,`${view} bounded visible integer year ticks: ${rendered.ticks.length}`);
    for(let i=1;i<rendered.ticks.length;i++)assert(rendered.ticks[i-1].right+4<=rendered.ticks[i].left,`${view} labels overlap at ${width}px: ${JSON.stringify(rendered.ticks)}`);
    assert(rendered.ticks.every(t=>timeline?/^\d{4}$/.test(t.text):/^\d+$/.test(t.text)),`${view} integer year labels`);
    assert.deepEqual(rendered.years,expectedYears);assert.deepEqual(rendered.values,expectedY);
    assert.equal(rendered.monthRows,timeline?graph.funding_timeline.months.length:expectedYears.length);if(timeline)assert.equal(rendered.yearRows,graph.funding_timeline.annual_averages.length);
   }
  }
  assert.deepEqual(errors,[]);console.log(`PASS: actual ${graph.funding_timeline.months.length}-month/${graph.funding_timeline.annual_averages.length}-calendar-year timeline; spending/worst/portfolio/timeline ticks legible at375/650px in light/dark; complete trace and table rows retained`);
 }finally{await browser.close()}
})().catch(e=>{console.error(e);process.exitCode=1});
