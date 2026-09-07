// Full-site final-state audit; incomplete rules remain unresolved evidence.
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
const {chromium}=require(process.env.DI5_PLAYWRIGHT||'/home/darrell/.npm/_npx/e41f203b7505f1fb/node_modules/playwright');
const base=process.env.DI5_BASE,out=process.env.DI5_OUTPUT,root=process.env.DI5_SOURCE_ROOT,u=new URL(base);
assert.equal(u.hostname,'127.0.0.1');assert.ok(u.port&&!['8080','8081'].includes(u.port));assert.ok(out&&root);fs.mkdirSync(out,{recursive:true});
const nav=fs.readFileSync(path.join(root,'web/templates/layouts/base.html'),'utf8');
const desktop=nav.slice(nav.indexOf('<nav'),nav.indexOf('<!-- Theme Toggle -->'));
const routes=[...new Set([...desktop.matchAll(/href="(?:\{\{withRange ")?(\/[a-z][a-z-]*)/g)].map(m=>m[1]))].filter(r=>!r.startsWith('/static'));
assert.equal(routes.length,9);
(async()=>{const browser=await chromium.launch({headless:true,executablePath:process.env.DI5_CHROME||'/home/darrell/.cache/ms-playwright/chromium-1243/chrome-linux64/chrome',args:['--no-sandbox']});const results=[];
try{for(const route of routes)for(const dark of [false,true]){
 const id=route.slice(1)+'-'+(dark?'dark':'light'),ctx=await browser.newContext({viewport:{width:1440,height:900},reducedMotion:'reduce'}),p=await ctx.newPage(),errors=[];
 p.on('pageerror',e=>errors.push(e.message));await p.route('**/*',r=>new URL(r.request().url()).origin===base&&['GET','HEAD'].includes(r.request().method())?r.continue():r.abort());
 const response=await p.goto(base+route);assert.equal(response.status(),200);
 await p.evaluate(d=>{document.documentElement.classList.toggle('dark',d);window.dispatchEvent(new Event('themechange'));},dark);await p.waitForTimeout(2000);
 await p.addScriptTag({path:process.env.DI5_AXE||'/home/darrell/.config/nvm/versions/node/v24.12.0/lib/node_modules/@axe-core/cli/node_modules/axe-core/axe.min.js'});
 const audit=await p.evaluate(()=>axe.run(document,{runOnly:{type:'tag',values:['wcag2a','wcag2aa','wcag21a','wcag21aa','wcag22aa']}}));
 fs.writeFileSync(path.join(out,id+'-axe.json'),JSON.stringify(audit,null,2));
 const dom=await p.evaluate(()=>({h1:document.querySelectorAll('h1').length,landmarks:['main','nav','header'].map(s=>[s,document.querySelectorAll(s).length]),headings:[...document.querySelectorAll('h1,h2,h3,h4,h5,h6')].map(e=>({level:e.tagName,text:e.innerText})),unlabelled:[...document.querySelectorAll('input:not([type=hidden]),select,textarea')].filter(e=>e.checkVisibility()&&!e.labels?.length&&!e.getAttribute('aria-label')&&!e.getAttribute('aria-labelledby')).map(e=>({id:e.id,type:e.type,placeholder:e.getAttribute('placeholder')})),animations:[...document.querySelectorAll('main *')].filter(e=>e.checkVisibility()&&getComputedStyle(e).animationName!=='none').map(e=>({id:e.id,name:getComputedStyle(e).animationName}))}));
 const rec={id,route,dark,errors,dom,violations:audit.violations.map(v=>({id:v.id,targets:v.nodes.map(n=>n.target)})),incomplete:audit.incomplete.map(v=>({id:v.id,targets:v.nodes.map(n=>n.target)}))};results.push(rec);
 fs.writeFileSync(path.join(out,id+'.html'),await p.content());fs.writeFileSync(path.join(out,id+'.ax.txt'),await p.locator('body').ariaSnapshot());await p.screenshot({path:path.join(out,id+'.png')});
 console.log(JSON.stringify({id,violations:rec.violations.length,incomplete:rec.incomplete.length,errors}));
 await ctx.close();
}}finally{await browser.close();fs.writeFileSync(path.join(out,'summary.json'),JSON.stringify(results,null,2));}
for(const r of results){assert.deepEqual(r.errors,[]);assert.deepEqual(r.violations,[],'new violations versus zero-violation baseline: '+r.id);assert.equal(r.dom.h1,1);for(const [name,n] of r.dom.landmarks)assert.ok(n>0,r.id+' '+name);}
assert.equal(results.length,18);
})().catch(e=>{console.error(e);process.exitCode=1;});
