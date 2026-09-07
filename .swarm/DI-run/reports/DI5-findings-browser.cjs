// Requires DI5_ARTIFACT_DIR findings.html from TestDI5HTTPFindingsLinks.
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
const {chromium}=require(process.env.DI5_PLAYWRIGHT||'/home/darrell/.npm/_npx/e41f203b7505f1fb/node_modules/playwright');
const base=process.env.DI5_BASE,out=process.env.DI5_OUTPUT,file=process.env.DI5_FINDINGS,u=new URL(base);
assert.equal(u.hostname,'127.0.0.1');assert.ok(u.port&&!['8080','8081'].includes(u.port));assert.ok(file&&out);fs.mkdirSync(out,{recursive:true});
(async()=>{const browser=await chromium.launch({headless:true,executablePath:process.env.DI5_CHROME||'/home/darrell/.cache/ms-playwright/chromium-1243/chrome-linux64/chrome',args:['--no-sandbox']});
try{for(const dark of [false,true])for(const width of [390,768,1440]){
 const ctx=await browser.newContext({viewport:{width,height:900},reducedMotion:'reduce'}),p=await ctx.newPage();
 await p.route('**/*',r=>{const u=new URL(r.request().url());if(u.origin!==base||!['GET','HEAD'].includes(r.request().method()))return r.abort();if(u.pathname==='/di5-findings')return r.fulfill({contentType:'text/html',body:fs.readFileSync(file,'utf8')});return r.continue();});
 await p.goto(base+'/di5-findings');await p.evaluate(d=>{document.documentElement.classList.toggle('dark',d);window.dispatchEvent(new Event('themechange'));},dark);await p.waitForTimeout(500);
 assert.equal(await p.locator('#findings-preview > li').count(),5);const disclosure=p.locator('#all-findings'),summary=disclosure.locator('summary');
 assert.equal(await disclosure.locator('li').count(),6);assert.equal(await disclosure.locator('li').first().isVisible(),false);
 const names=await disclosure.locator('a').allTextContents();const closed=await disclosure.ariaSnapshot();for(const n of names)assert.ok(!closed.includes(n));
 await summary.focus();await p.keyboard.press('Enter');assert.ok(await disclosure.locator('li').last().isVisible());
 const opened=await disclosure.ariaSnapshot();for(const n of names)assert.ok(opened.includes(n));await p.keyboard.press('Tab');assert.ok(await disclosure.locator('a').first().evaluate(e=>e===document.activeElement));
 assert.equal(await p.evaluate(()=>document.documentElement.scrollWidth),width);
 await p.screenshot({path:path.join(out,'findings-'+dark+'-'+width+'.png'),fullPage:true});
 await summary.focus();await p.keyboard.press('Space');assert.equal(await disclosure.locator('li').first().isVisible(),false);const reclosed=await disclosure.ariaSnapshot();for(const n of names)assert.ok(!reclosed.includes(n));
 fs.writeFileSync(path.join(out,'findings-'+dark+'-'+width+'.json'),JSON.stringify({dark,width,preview:5,remaining:6,closed,opened,reclosed},null,2));
 console.log(JSON.stringify({dark,width,preview:5,remaining:6,keyboard:true,AX:true}));await ctx.close();
}}finally{await browser.close();}})().catch(e=>{console.error(e);process.exitCode=1;});
