// M14: actual freshly rendered Dashboard HTML with only refresh JS active.
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
const {chromium}=require(process.env.DI5_PLAYWRIGHT||'/home/darrell/.npm/_npx/e41f203b7505f1fb/node_modules/playwright');
const base=process.env.DI5_BASE,root=process.env.DI5_SOURCE_ROOT,out=process.env.DI5_OUTPUT,u=new URL(base);
assert.equal(u.hostname,'127.0.0.1');assert.ok(u.port&&!['8080','8081'].includes(u.port));assert.ok(root&&out);fs.mkdirSync(out,{recursive:true});
(async()=>{
 const response=await fetch(base+'/dashboard?start=2026-08-01&end=2026-08-28');assert.equal(response.status,200);
 const raw=(await response.text()).replace(/<script\b[^>]*>[\s\S]*?<\/script>/gi,'');
 const script=fs.readFileSync(path.join(root,'web/static/js/page-refresh.js'),'utf8');
 const browser=await chromium.launch({headless:true,executablePath:process.env.DI5_CHROME||'/home/darrell/.cache/ms-playwright/chromium-1243/chrome-linux64/chrome',args:['--no-sandbox']});
 const results=[];
 try{for(const dark of [false,true])for(const width of [390,768,1440]){
  let revision=0,epoch='one',loads=0;const obstructions=[];
  const ctx=await browser.newContext({viewport:{width,height:900},reducedMotion:'reduce'}),p=await ctx.newPage();
  await p.route('**/*',r=>{
   const u=new URL(r.request().url());if(u.origin!=='http://di5.test'||r.request().method()!=='GET')return r.abort();
   if(u.pathname==='/api/ui-refresh')return r.fulfill({json:{epoch,revision}});
   if(u.pathname.startsWith('/static/css/'))return r.fulfill({contentType:'text/css',body:fs.readFileSync(path.join(root,'web',u.pathname))});
   if(u.pathname==='/probe'){loads++;return r.fulfill({contentType:'text/html',body:raw.replace('<html lang="en"','<html class="'+(dark?'dark':'')+'" lang="en"').replace('</body>','<script>'+script+'</script></body>')});}
   return r.abort();
  });
  const signal=async()=>{await p.evaluate(()=>window.dispatchEvent(new Event('focus')));await p.waitForTimeout(120);};
  await p.goto('http://di5.test/probe?scenario=demo#settings');await p.waitForTimeout(100);
  revision=1;await signal();await p.waitForFunction(()=>document.readyState==='complete');await p.waitForTimeout(200);
  assert.equal(loads,2);assert.equal(p.url(),'http://di5.test/probe?scenario=demo#settings');await p.waitForTimeout(2100);assert.equal(loads,2);
  const input=p.locator('#dashboard-date-start');await input.fill('2026-07-01');await input.focus();revision=2;await signal();
  const notice=p.getByRole('region',{name:'Page refresh'});await notice.waitFor();assert.ok(await input.evaluate(e=>e===document.activeElement));
  const refresh=notice.getByRole('button',{name:'Refresh page',exact:true}),dismiss=notice.getByRole('button',{name:'Keep editing'});
  let tabs=0;while(!(await refresh.evaluate(e=>e===document.activeElement))&&tabs++<250){
   await p.keyboard.press('Tab');
   const obscured=await p.evaluate(()=>{const e=document.activeElement,n=document.querySelector('section[aria-label="Page refresh"]'),r=e.getBoundingClientRect(),b=n.getBoundingClientRect();return !n.contains(e)&&r.width>0&&r.height>0&&r.left>=b.left&&r.right<=b.right&&r.top>=b.top&&r.bottom<=b.bottom;});
   if(obscured){const detail=await p.evaluate(()=>({id:document.activeElement.id,tag:document.activeElement.tagName,text:document.activeElement.innerText,rect:document.activeElement.getBoundingClientRect().toJSON(),notice:document.querySelector('section[aria-label="Page refresh"]').getBoundingClientRect().toJSON()}));obstructions.push(detail);fs.writeFileSync(path.join(out,'obscured-'+dark+'-'+width+'.json'),JSON.stringify(obstructions,null,2));await p.screenshot({path:path.join(out,'obscured-'+dark+'-'+width+'.png')});}
  }
  assert.ok(tabs<250);p.once('dialog',d=>d.dismiss());await p.keyboard.press('Enter');assert.equal(loads,2);assert.equal(await input.inputValue(),'2026-07-01');
  await p.keyboard.press('Tab');assert.ok(await dismiss.evaluate(e=>e===document.activeElement));
  await p.screenshot({path:path.join(out,'refresh-'+dark+'-'+width+'.png')});
  await p.keyboard.press('Enter');assert.equal(await notice.count(),0);assert.equal(await p.getByRole('status').count(),0);assert.ok(await input.evaluate(e=>e===document.activeElement));
  await signal();assert.equal(await notice.count(),0);
  revision=3;await signal();await notice.waitFor();epoch='two';revision=0;await signal();assert.equal(await notice.count(),0);assert.equal(await p.getByRole('status').count(),0);assert.equal(loads,2);assert.equal(await input.inputValue(),'2026-07-01');
  revision=1;await signal();await notice.waitFor();p.once('dialog',d=>d.accept());await refresh.click();await p.waitForTimeout(250);assert.equal(loads,3);assert.equal(p.url(),'http://di5.test/probe?scenario=demo#settings');
  await p.waitForTimeout(2100);assert.equal(loads,3);revision=2;await signal();await p.waitForTimeout(200);assert.equal(loads,4);
  results.push({dark,width,loads,tabs,obstructions,refusal:true,dismissal:true,restart:true,acceptance:true,cleanReload:true});console.log(JSON.stringify(results.at(-1)));await ctx.close();
 }}finally{await browser.close();fs.writeFileSync(path.join(out,'refresh.json'),JSON.stringify(results,null,2));}
 assert.equal(results.length,6);
 for(const r of results)assert.deepEqual(r.obstructions,[],'refresh focus obstruction at '+r.dark+'/'+r.width);
})().catch(e=>{console.error(e);process.exitCode=1;});
