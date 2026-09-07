// Real-browser layout/lifecycle fixture: actual shared refresh JS, CSS and HTMX.
// Synthetic HTML positions a focused input at the viewport edge to catch lost
// visibility on insertion; no production data/routes are mutated.
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
const {chromium}=require(process.env.DI5_PLAYWRIGHT||'/home/darrell/.npm/_npx/e41f203b7505f1fb/node_modules/playwright');
const root=process.env.DI5_SOURCE_ROOT,out=process.env.DI5_OUTPUT;
assert.ok(root&&root.startsWith('/tmp/')&&out);fs.mkdirSync(out,{recursive:true});
(async()=>{
 const b=await chromium.launch({headless:true,executablePath:process.env.DI5_CHROME||'/home/darrell/.cache/ms-playwright/chromium-1243/chrome-linux64/chrome',args:['--no-sandbox']});
 const results=[];
 try{for(const dark of [false,true])for(const width of [390,768,1440])for(const hasMain of [true,false]){
  const ctx=await b.newContext({viewport:{width,height:900},reducedMotion:'reduce'}),p=await ctx.newPage();let revision=0;
  const content='<h1>Refresh layout fixture</h1><div style="height:800px"></div><label for="edit">Unsaved note</label><input id="edit" value="saved"><div id="partial">Original partial</div><div style="height:1000px"></div>';
  const html='<!doctype html><html lang="en" class="'+(dark?'dark':'')+'"><head><title>Refresh fixture</title><link rel="stylesheet" href="/static/css/tailwind.css"></head><body>'+(hasMain?'<main>'+content+'</main>':content)+'<script src="/static/vendor/htmx.min.js"></script><script src="/static/js/page-refresh.js"></script></body></html>';
  await p.route('**/*',r=>{
   const u=new URL(r.request().url());if(u.origin!=='http://di5-layout.test'||r.request().method()!=='GET')return r.abort();
   if(u.pathname==='/api/ui-refresh')return r.fulfill({json:{epoch:'one',revision}});
   if(u.pathname==='/')return r.fulfill({contentType:'text/html',body:html});
   if(u.pathname==='/partial')return r.fulfill({contentType:'text/html',body:'Updated partial'});
   if(u.pathname==='/main')return r.fulfill({contentType:'text/html',body:'<main><h1>Replacement heading</h1><label for="edit">Unsaved note</label><input id="edit" value="unsaved"></main>'});
   if(['/static/js/page-refresh.js','/static/vendor/htmx.min.js','/static/css/tailwind.css'].includes(u.pathname))return r.fulfill({contentType:u.pathname.endsWith('.css')?'text/css':'text/javascript',body:fs.readFileSync(path.join(root,'web',u.pathname))});
   return r.abort();
  });
  await p.goto('http://di5-layout.test/');await p.waitForTimeout(150);
  const input=p.locator('#edit');await input.fill('unsaved');await input.focus();
  await p.evaluate(()=>{window.editBefore=document.activeElement;window.scrollTo(0,0);});
  const before=await input.boundingBox();assert.ok(before.y+before.height<=900);
  revision=1;await p.evaluate(()=>dispatchEvent(new Event('focus')));
  const notice=p.getByRole('region',{name:'Page refresh'});await notice.waitFor();
  const arrival=await p.evaluate(()=>{
   const e=document.querySelector('#edit'),r=e.getBoundingClientRect(),n=document.querySelector('section[aria-label="Page refresh"]');
   window.noticeBefore=n;window.statusBefore=n.querySelector('[role=status]');
   const points=[];for(const x of [.15,.5,.85])for(const y of [.15,.5,.85])points.push(document.elementFromPoint(r.left+r.width*x,r.top+r.height*y)===e);
   return {same:e===window.editBefore&&e===document.activeElement,value:e.value,rect:r.toJSON(),notice:n.getBoundingClientRect().toJSON(),position:getComputedStyle(n).position,parent:n.parentElement.tagName,first:n.parentElement.firstElementChild===n,points,scroll:scrollY};
  });
  assert.ok(arrival.same);assert.equal(arrival.value,'unsaved');assert.equal(arrival.position,'static');
  assert.equal(arrival.parent,hasMain?'MAIN':'BODY');assert.ok(arrival.first);
  assert.ok(arrival.rect.top>=0&&arrival.rect.bottom<=900);assert.ok(arrival.points.every(Boolean),'entire focused input remains exposed');
  assert.ok(arrival.scroll>0,'fixture actually displaces focused input');
  await p.screenshot({path:path.join(out,'arrival-'+dark+'-'+width+'-'+hasMain+'.png')});
  await p.evaluate(()=>htmx.ajax('GET','/partial',{target:'#partial',swap:'innerHTML'}));
  assert.equal(await p.locator('#partial').innerText(),'Updated partial');
  assert.ok(await p.evaluate(()=>window.noticeBefore===document.querySelector('section[aria-label="Page refresh"]')&&window.statusBefore.isConnected));
  if(hasMain){
   await p.evaluate(()=>htmx.ajax('GET','/main',{target:'main',swap:'outerHTML'}));
   assert.equal(await notice.count(),1);assert.equal(await notice.getByRole('status').count(),1);
   assert.ok(await p.evaluate(()=>window.noticeBefore===document.querySelector('main').firstElementChild&&window.statusBefore===window.noticeBefore.querySelector('[role=status]')));
   assert.equal(await input.inputValue(),'unsaved');
  }
  await notice.getByRole('button',{name:'Keep editing'}).click();assert.equal(await notice.count(),0);assert.equal(await p.getByRole('status').count(),0);
  await p.evaluate(()=>dispatchEvent(new Event('focus')));await p.waitForTimeout(100);assert.equal(await notice.count(),0);
  results.push({dark,width,hasMain,before,arrival,partialRetained:true,mainRetained:hasMain,dismissal:true});console.log(JSON.stringify({dark,width,hasMain,scroll:arrival.scroll}));
  fs.writeFileSync(path.join(out,'results.json'),JSON.stringify(results,null,2));await ctx.close();
 }}finally{await b.close();}
 assert.equal(results.length,12);
})().catch(e=>{console.error(e);process.exitCode=1;});

