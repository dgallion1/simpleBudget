const fs=require('fs'),path=require('path'),http=require('http'),crypto=require('crypto');
const {chromium}=require(process.env.DI5_PLAYWRIGHT||'/home/darrell/.npm/_npx/e41f203b7505f1fb/node_modules/playwright');
const root=__dirname,source=__dirname,base=process.env.DI5_BASE||'http://127.0.0.1:18873';
const parsed=new URL(base);if(parsed.hostname!=='127.0.0.1'||!parsed.port||['8080','8081'].includes(parsed.port))throw Error('Dedicated loopback required');
const out=path.join(root,'artifacts');fs.mkdirSync(out,{recursive:true});
const hash=b=>crypto.createHash('sha256').update(b).digest('hex');
const save=(n,v)=>fs.writeFileSync(path.join(out,n),typeof v==='string'?v:JSON.stringify(v,null,2));
const nav=fs.readFileSync(path.join(source,'web/templates/layouts/base.html'),'utf8');
const routes=[...new Set([...nav.matchAll(/href="(?:\{\{withRange ")?(\/[a-z][a-z-]*)/g)].map(m=>m[1]))].filter(p=>!p.startsWith('/static'));
// Restrict the inventory to the desktop navigation block, retaining the conditional Duplicates item.
const desktop=nav.slice(nav.indexOf('<nav'),nav.indexOf('<!-- Theme Toggle -->'));
const inventory=routes.filter(r=>desktop.includes('"'+r+'"'));save('navigation.json',inventory);
const hashes=[];
function walk(dir){for(const e of fs.readdirSync(path.join(source,dir),{withFileTypes:true})){const rel=path.join(dir,e.name);if(rel==='internal/config/data')continue;if(e.isDirectory())walk(rel);else if(e.isFile()&&/\.(go|html|js|cjs|css)$/.test(e.name)){const bytes=fs.readFileSync(path.join(source,rel)),copy=fs.readFileSync(path.join(root,rel));if(!bytes.equals(copy))throw Error('Baseline drift: '+rel);hashes.push({path:rel,sha256:hash(bytes),bytes:bytes.length});}}}
for(const d of ['cmd','internal','web'])walk(d);
for(const rel of ['ACCESSIBILITY.md','go.mod','go.sum','Makefile','tailwind.config.js']){const data=fs.readFileSync(path.join(source,rel));hashes.push({path:rel,sha256:hash(data),bytes:data.length});}
save('source-hashes.json',{source,files:hashes});
const docs={},summary=[];
(async()=>{
const browser=await chromium.launch({headless:true,executablePath:process.env.DI5_CHROME||'/home/darrell/.cache/ms-playwright/chromium-1243/chrome-linux64/chrome',args:['--no-sandbox']});
save('environment.json',{date:new Date().toISOString(),browser:browser.version(),viewport:{width:1440,height:900},source,base,routes:inventory,node:process.version});
for(const route of inventory)for(const theme of ['light','dark']){
 const id=route.slice(1)+'-'+theme,rec={id,route,theme,requestedURL:base+route,responses:[],requestFailures:[],blocked:[],pageErrors:[],consoleErrors:[]};
 const ctx=await browser.newContext({viewport:{width:1440,height:900},colorScheme:theme==='dark'?'dark':'light',reducedMotion:'reduce'});const p=await ctx.newPage();p.setDefaultTimeout(15000);
 await p.route('**/*',r=>{const req=r.request(),url=new URL(req.url());if(url.origin!==base||!['GET','HEAD'].includes(req.method())){rec.blocked.push({url:req.url(),method:req.method()});return r.abort();}return r.continue();});
 p.on('pageerror',e=>rec.pageErrors.push(e.message));p.on('console',m=>{if(m.type()==='error')rec.consoleErrors.push(m.text());});p.on('requestfailed',r=>rec.requestFailures.push({url:r.url(),error:r.failure()?.errorText}));p.on('response',r=>{if(r.status()>=400)rec.responses.push({url:r.url(),status:r.status()});});
 try{
  const response=await p.goto(base+route,{waitUntil:'domcontentloaded',timeout:20000});rec.status=response?.status();rec.finalURL=p.url();
  await p.evaluate(d=>{document.documentElement.classList.toggle('dark',d);window.dispatchEvent(new Event('themechange'));},theme==='dark');
  await p.waitForTimeout(2000);
  rec.dom=await p.evaluate(()=>{
   const visible=e=>e.checkVisibility({checkOpacity:true,checkVisibilityCSS:true});
   const compact=e=>({tag:e.tagName,id:e.id,text:e.textContent.trim().replace(/\s+/g,' ').slice(0,150)});
   return {title:document.title,theme:document.documentElement.className,width:innerWidth,scrollWidth:document.documentElement.scrollWidth,
    headings:[...document.querySelectorAll('h1,h2,h3,h4,h5,h6')].filter(visible).map(compact),landmarks:['main','nav','header','footer'].map(s=>({name:s,count:document.querySelectorAll(s).length})),
    unscopedHeaders:[...document.querySelectorAll('th:not([scope])')].filter(visible).map(compact),
    forms:[...document.querySelectorAll('input,select,textarea')].filter(visible).map(e=>({tag:e.tagName,id:e.id,type:e.type,required:e.required,label:[...e.labels||[]].map(l=>l.textContent.trim()).join(' '),ariaLabel:e.getAttribute('aria-label'),labelledby:e.getAttribute('aria-labelledby'),describedby:e.getAttribute('aria-describedby')})),
    clickable:[...document.querySelectorAll('[onclick],[role=button]')].filter(visible).map(e=>({...compact(e),role:e.getAttribute('role'),tabIndex:e.tabIndex})),
    live:[...document.querySelectorAll('[aria-live],[role=status],[role=alert]')].map(e=>({...compact(e),visible:visible(e),live:e.getAttribute('aria-live')})),
    hiddenText:[...document.querySelectorAll('.sr-only,[aria-hidden=true]')].filter(e=>e.textContent.trim()).map(compact),
    images:[...document.querySelectorAll('img')].map(e=>({src:e.getAttribute('src'),alt:e.getAttribute('alt')})),
    dialogs:[...document.querySelectorAll('[role=dialog],[role=alertdialog]')].map(e=>({...compact(e),visible:visible(e),modal:e.getAttribute('aria-modal'),name:e.getAttribute('aria-label'),labelledby:e.getAttribute('aria-labelledby')})),
    animations:[...document.querySelectorAll('body *')].filter(visible).filter(e=>getComputedStyle(e).animationName!=='none').slice(0,20).map(e=>({...compact(e),animation:getComputedStyle(e).animationName,duration:getComputedStyle(e).animationDuration})),
    charts:[...document.querySelectorAll('.chart-container[data-chart-url],.js-plotly-plot')].filter((e,i,a)=>a.indexOf(e)===i).map(e=>({id:e.id,visible:visible(e),ready:!!e.data,series:e.data?.length,font:e._fullLayout?.font?.color,adjacentTable:!!document.getElementById(e.id+'-data-table'),parentTables:e.parentElement.querySelectorAll('table').length})),
    nav:[...document.querySelectorAll('nav a')].filter(visible).map(e=>({text:e.textContent.trim(),href:e.getAttribute('href')}))};
  });
  rec.htmlFile=id+'.html';docs['/'+id+'.html']=(await p.content()).replace(/<script\b[^>]*>[\s\S]*?<\/script>/gi,'');save(rec.htmlFile,docs['/'+id+'.html']);
  save(id+'.ax.txt',await p.locator('body').ariaSnapshot());await p.screenshot({path:path.join(out,id+'.png')});
 }catch(e){rec.loadError=e.message;}
 save(id+'.meta.json',rec);summary.push(rec);console.log(JSON.stringify({id,status:rec.status,loadError:rec.loadError,errors:rec.pageErrors.length,blocked:rec.blocked.length,charts:rec.dom?.charts.length}));await ctx.close();
}
await browser.close();save('capture-summary.json',summary);
const server=http.createServer((req,res)=>{const pathname=new URL(req.url,'http://127.0.0.1').pathname;if(docs[pathname]){res.setHeader('Content-Type','text/html');return res.end(docs[pathname]);}if(pathname.startsWith('/static/')){const file=path.join(root,'web',pathname);if(fs.existsSync(file)&&fs.statSync(file).isFile()){res.setHeader('Content-Type',file.endsWith('.css')?'text/css':file.endsWith('.svg')?'image/svg+xml':'application/octet-stream');return res.end(fs.readFileSync(file));}}res.writeHead(404);res.end();});
await new Promise(r=>server.listen(18874,'127.0.0.1',r));console.log('ARTIFACTS READY :18874; '+summary.length+' page/theme captures');
})().catch(e=>{console.error(e);process.exit(1)});
