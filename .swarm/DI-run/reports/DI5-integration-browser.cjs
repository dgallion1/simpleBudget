// Prerequisites: isolated frozen synthetic server, DI5_BASE and DI5_OUTPUT.
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
const {chromium}=require(process.env.DI5_PLAYWRIGHT||'/home/darrell/.npm/_npx/e41f203b7505f1fb/node_modules/playwright');
const base=process.env.DI5_BASE,out=process.env.DI5_OUTPUT,u=new URL(base);
assert.equal(u.hostname,'127.0.0.1');assert.ok(u.port&&!['8080','8081'].includes(u.port));assert.ok(out);fs.mkdirSync(out,{recursive:true});
let session,id=0;
async function rpc(method,params){
 const headers={'Content-Type':'application/json','Accept':'application/json, text/event-stream'};if(session)headers['Mcp-Session-Id']=session;
 const r=await fetch(base+'/mcp',{method:'POST',headers,body:JSON.stringify({jsonrpc:'2.0',id:++id,method,params})});
 assert.equal(r.status,200);session=r.headers.get('mcp-session-id')||session;const text=await r.text();
 const obj=text.startsWith('event:')||text.startsWith('data:')?JSON.parse(text.split('\n').find(l=>l.startsWith('data:')).slice(5)):JSON.parse(text);
 assert.ok(!obj.error,JSON.stringify(obj));return obj.result;
}
async function tool(name,args){const r=await rpc('tools/call',{name,arguments:args});assert.ok(!r.isError,JSON.stringify(r));return r.structuredContent||JSON.parse(r.content[0].text);}
(async()=>{
 await rpc('initialize',{protocolVersion:'2025-06-18',capabilities:{},clientInfo:{name:'DI5-synthetic',version:'1'}});
 const trends=await tool('get_trends',{start_date:'2026-07-01',end_date:'2026-07-31'});
 assert.equal(trends.previous_start,'2026-06-01');assert.equal(trends.previous_end,'2026-06-30');
 assert.equal(trends.period.history_available,true);assert.equal(trends.period.forecast_available,false);
 const expected=[['Home Maintenance & Appliances',1199,300,899],['Shopping & Household Supplies',201.15,92.84,108.31],['Cash Spending',100,0,100]];
 for(let i=0;i<3;i++){const r=trends.major_expense_trends[i];assert.deepEqual([r.category,r.current_amount,r.previous_amount,r.change_amount],expected[i]);}
 const recurring=await tool('get_recurring',{reference_date:'2026-08-28'});
 assert.equal(recurring.count,22);const counts={subscription:0,bill:0,other:0};
 for(const p of recurring.payments){counts[p.classification]++;assert.ok(p.classification_reason);}
 assert.deepEqual(counts,{subscription:3,bill:7,other:12});
 assert.deepEqual(recurring.payments.filter(p=>p.classification==='subscription').map(p=>p.description).sort(),['cloud storage','netflix','spotify']);
 fs.writeFileSync(path.join(out,'demo-tools.json'),JSON.stringify({trends,recurring},null,2));
 const browser=await chromium.launch({headless:true,executablePath:process.env.DI5_CHROME||'/home/darrell/.cache/ms-playwright/chromium-1243/chrome-linux64/chrome',args:['--no-sandbox']});
 try{for(const dark of [false,true])for(const width of [390,768,1440]){
  const ctx=await browser.newContext({viewport:{width,height:900},reducedMotion:'reduce'}),p=await ctx.newPage();
  await p.route('**/*',r=>new URL(r.request().url()).origin===base&&['GET','HEAD'].includes(r.request().method())?r.continue():r.abort());
  const q='?start=2026-07-01&end=2026-07-31';
  await p.goto(base+'/dashboard'+q);await p.evaluate(d=>{document.documentElement.classList.toggle('dark',d);window.dispatchEvent(new Event('themechange'));},dark);
  const net=await p.locator('#dashboard-overview').innerText();
  const money=net.match(/Net spending\s+(-?\$[\d,]+\.\d{2})/)[1];
  // Follow actual navigation, preserving the selected dates.
  if(width<1280){await p.locator('#mobile-nav-toggle').focus();await p.keyboard.press('Enter');}
  const nav=p.locator('nav a[href^="/insights"]').filter({visible:true}).first();
  const navQuery=new URL(await nav.getAttribute('href'),base).searchParams;
  assert.equal(navQuery.get('start'),'2026-07-01');assert.equal(navQuery.get('end'),'2026-07-31');
  await nav.click();await p.waitForFunction(()=>document.querySelector('#chart-trends')?.data?.length);
  await p.evaluate(d=>{document.documentElement.classList.toggle('dark',d);window.dispatchEvent(new Event('themechange'));},dark);await p.waitForTimeout(500);
  assert.ok((await p.locator('#insights-change').innerText()).includes('Selected period: '+money));
  assert.deepEqual(await p.locator('[data-contributor]').evaluateAll(es=>es.map(e=>e.dataset.contributor)),expected.map(r=>r[0]));
  for(const r of expected){const text=await p.locator('[data-contributor]').filter({hasText:r[0]}).innerText();assert.ok(text.includes('+$'+r[3].toFixed(2)));}
  const chart=await p.evaluate(()=>{const c=document.querySelector('#chart-trends');return {traces:c.data.map(t=>({x:t.x,y:t.y})),rows:[...document.querySelectorAll('#category-trends-table tbody tr')].map(r=>({name:r.dataset.category,current:Number(r.dataset.current),previous:Number(r.dataset.previous)}))};});
  assert.equal(chart.traces[0].x.length,chart.rows.length);for(let i=0;i<chart.rows.length;i++){assert.equal(chart.traces[0].x[i],chart.rows[i].current);assert.equal(chart.traces[1].x[i],chart.rows[i].previous);assert.equal(chart.traces[0].y[i],chart.rows[i].name);}
  assert.equal(await p.evaluate(()=>document.documentElement.scrollWidth),width);
  await p.screenshot({path:path.join(out,'insights-'+(dark?'dark':'light')+'-'+width+'.png'),fullPage:true});
  for(const selector of ['#insights-change','#insights-recurring','#insights-supporting']){await p.locator(selector).scrollIntoViewIfNeeded();await p.screenshot({path:path.join(out,'insights-'+(dark?'dark':'light')+'-'+width+'-'+selector.slice(1)+'.png')});}
  const input=p.locator('#insights-date-start');await input.focus();await input.fill('2026-07-02');
  const response=p.waitForResponse(r=>r.url().includes('/insights?')&&r.request().headers()['hx-request']==='true');
  await input.dispatchEvent('change');await response;await p.waitForFunction(()=>document.querySelector('#period-context')?.innerText.includes('2026-07-02'));
  assert.ok(await p.locator('#insights-date-start').evaluate(e=>document.activeElement===e));
  assert.equal(await p.locator('#insights-wrapper').count(),1);
  for(const href of await p.locator('#insights-findings a,#insights-change a').evaluateAll(es=>es.map(e=>e.href))){assert.equal(new URL(href).searchParams.get('start'),'2026-07-02');assert.equal(new URL(href).searchParams.get('end'),'2026-07-31');}
  await p.goto(base+'/insights/recurring?start=2026-08-01&end=2026-08-28');
  for(const [name,count] of [['subscriptions',3],['bills',7],['other',12]]){assert.equal(await p.locator('section[aria-labelledby="recurring-'+name+'"] tbody tr').count(),count);}
  for(const row of recurring.payments)assert.ok((await p.locator('body').innerText()).includes(row.description));
  console.log(JSON.stringify({dark,width,selectedNet:money,contributors:expected,recurring:counts,HTMX:true}));
  await ctx.close();
 }}finally{await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
