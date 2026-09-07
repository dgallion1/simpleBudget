// Promoted from /tmp/DI5-final-a11y.JYHU8N/focus-obstruction.cjs.
// All production scripts remain active; only read-only refresh responses are mocked.
// Prerequisites: isolated synthetic server, cached Playwright/Chromium. Never 8080/8081.
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
const {chromium}=require(process.env.DI5_PLAYWRIGHT||'/home/darrell/.npm/_npx/e41f203b7505f1fb/node_modules/playwright');
const base=process.env.DI5_BASE,out=process.env.DI5_OUTPUT,u=new URL(base);
assert.equal(u.hostname,'127.0.0.1');assert.ok(u.port&&!['8080','8081'].includes(u.port));assert.ok(out);fs.mkdirSync(out,{recursive:true});
(async()=>{
 const browser=await chromium.launch({headless:true,executablePath:process.env.DI5_CHROME||'/home/darrell/.cache/ms-playwright/chromium-1243/chrome-linux64/chrome',args:['--no-sandbox']});
 const results=[];
 try {for(const dark of [false,true])for(const width of [390,768,1440]){
  const ctx=await browser.newContext({viewport:{width,height:900},reducedMotion:'reduce'}),p=await ctx.newPage();let revision=0,calls=0;
  await p.route('**/*',r=>{const url=new URL(r.request().url());if(url.origin!==base||!['GET','HEAD'].includes(r.request().method()))return r.abort();if(url.pathname==='/api/ui-refresh'){calls++;return r.fulfill({json:{epoch:'checker',revision}});}return r.continue();});
  await p.goto(base+'/dashboard?start=2026-08-01&end=2026-08-28');
  await p.evaluate(d=>{document.documentElement.classList.toggle('dark',d);dispatchEvent(new Event('themechange'));},dark);
  await p.waitForTimeout(800);assert.ok(calls>0,'real refresh script establishes baseline');
  const input=p.locator('input[name=start]').first();await input.fill('2026-07-01');await input.focus();
  revision=1;await p.evaluate(()=>dispatchEvent(new Event('focus')));
  const notice=p.getByRole('region',{name:'Page refresh'});await notice.waitFor();
  const arrival=await input.evaluate(e=>({same:e===document.activeElement,value:e.value,rect:e.getBoundingClientRect().toJSON(),height:innerHeight}));
  assert.ok(arrival.same,'notice must not steal focus');assert.equal(arrival.value,'2026-07-01');
  assert.ok(arrival.rect.top>=0&&arrival.rect.bottom<=arrival.height,'focused input remains in viewport on arrival');
  const trace=[],hits=[];let reachedRefresh=false;
  for(let i=0;i<180;i++){
   await p.keyboard.press('Tab');await p.waitForTimeout(40);
   const rec=await p.evaluate(()=>{
    const e=document.activeElement,r=e.getBoundingClientRect(),n=document.querySelector('section[aria-label="Page refresh"]'),nr=n.getBoundingClientRect(),points=[];
    for(const x of [.15,.5,.85])for(const y of [.15,.5,.85]){const px=r.left+r.width*x,py=r.top+r.height*y,hit=document.elementFromPoint(px,py);points.push({x:px,y:py,hit:hit?.tagName,text:hit?.textContent?.slice(0,90),self:!!hit&&(hit===e||e.contains(hit)),notice:!!hit&&n.contains(hit)});}
    const contained=r.left>=nr.left&&r.right<=nr.right&&r.top>=nr.top&&r.bottom<=nr.bottom;
    return {tag:e.tagName,text:(e.innerText||e.getAttribute('aria-label')||'').slice(0,100),key:e.dataset.kpiDetail,rect:r.toJSON(),notice:nr.toJSON(),scroll:scrollY,outline:getComputedStyle(e).outline,points,contained,covered:!n.contains(e)&&r.width>0&&r.height>0&&contained&&points.every(t=>t.notice),isRefresh:n.contains(e)&&e.textContent==='Refresh page'};
   });
   trace.push(rec);if(rec.covered){hits.push(rec);await p.screenshot({path:path.join(out,'covered-'+dark+'-'+width+'-'+hits.length+'.png')});}
   if(rec.isRefresh){reachedRefresh=true;break;}
  }
  assert.ok(reachedRefresh,'notice buttons keyboard reachable');assert.equal(await notice.count(),1);
  for(const text of ['Income details','Cash-flow details','Budget details','Healthcare details']){
   assert.ok(trace.some(r=>r.text===text),'keyboard trace must exercise '+text);
  }
  fs.writeFileSync(path.join(out,'notice-'+dark+'-'+width+'.ax.txt'),await notice.ariaSnapshot());
  await p.screenshot({path:path.join(out,'notice-'+dark+'-'+width+'.png')});
  const result={dark,width,calls,arrival,hits,trace};results.push(result);
  fs.writeFileSync(path.join(out,'results.json'),JSON.stringify(results,null,2));
  console.log(JSON.stringify({dark,width,hits:hits.map(h=>({text:h.text,rect:h.rect,notice:h.notice,points:h.points.map(p=>p.notice)})),tabs:trace.length}));
  await ctx.close();
 }}finally{await browser.close();}
 assert.equal(results.length,6);
 for(const r of results)assert.deepEqual(r.hits,[],'fully obscured keyboard focus '+r.dark+'/'+r.width);
})().catch(e=>{console.error(e);process.exitCode=1;});
