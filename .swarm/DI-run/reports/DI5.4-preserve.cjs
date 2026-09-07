const fs=require('node:fs'),path=require('node:path'),crypto=require('node:crypto'),assert=require('node:assert/strict');
const live='/home/darrell/bin/ai/budget2/.worktrees/dashboard-insights',root='/tmp/DI5-fourth-worker.1ptHqE';
const out=path.join(root,'.swarm/DI-run/reports/DI5.4');fs.mkdirSync(out,{recursive:true});
const hash=p=>crypto.createHash('sha256').update(fs.readFileSync(p)).digest('hex');
const prior=fs.readFileSync(path.join(live,'.swarm/DI-run/manifests/DI5.3.files'),'utf8').trim().split('\n');assert.equal(prior.length,396);
const allowed=['web/static/js/page-refresh.js','.swarm/DI-run/reports/DI5-focus-ownership.cjs'];
const sources=[];
function walk(dir){for(const e of fs.readdirSync(path.join(root,dir),{withFileTypes:true})){const p=path.join(dir,e.name);if(e.isDirectory())walk(p);else if(e.isFile())sources.push(p);}}
for(const d of ['cmd','internal','web'])walk(d);
if(process.argv[2]==='baseline'){
 fs.writeFileSync(path.join(out,'baseline.json'),JSON.stringify({prior:prior.map(p=>({path:p,sha256:hash(path.join(live,p))})),sources:sources.map(p=>({path:p,sha256:hash(path.join(root,p)),live:hash(path.join(live,p))}))},null,2)+'\n');
 console.log('Baseline recorded: '+prior.length+' prior paths; '+sources.length+' source files');
}else{
 const before=JSON.parse(fs.readFileSync(path.join(out,'baseline.json')));
 const result={prior:before.prior.map(r=>({...r,after:hash(path.join(live,r.path))})),sources:before.sources.map(r=>({...r,after:hash(path.join(root,r.path)),liveAfter:hash(path.join(live,r.path))}))};
 for(const r of result.prior)if(!allowed.includes(r.path))assert.equal(r.after,r.sha256,r.path);
 for(const r of result.sources){if(!allowed.includes(r.path))assert.equal(r.after,r.sha256,r.path);assert.equal(r.after,r.liveAfter,r.path+' live/frozen');}
 assert.equal(result.prior.filter(r=>r.path.endsWith('.go')).length,18);
 fs.writeFileSync(path.join(out,'preservation.json'),JSON.stringify(result,null,2)+'\n');
 console.log('Preserved all prior paths except two authorized corrections; 18 Go files unchanged; '+result.sources.length+' live/frozen source files match');
}
