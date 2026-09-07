// Copies only explicit author correction/evidence paths; never copies a live tree.
const fs=require('node:fs'),path=require('node:path'),cp=require('node:child_process'),crypto=require('node:crypto'),assert=require('node:assert/strict');
const root='/tmp/DI5-fourth-worker.1ptHqE',live='/home/darrell/bin/ai/budget2/.worktrees/dashboard-insights';
const dir='.swarm/DI-run/reports/DI5.4';
const hash=p=>crypto.createHash('sha256').update(fs.readFileSync(p)).digest('hex');
const baseline=JSON.parse(fs.readFileSync(path.join(root,dir,'baseline.json')));
const corrections=['web/static/js/page-refresh.js','.swarm/DI-run/reports/DI5-focus-ownership.cjs'];
const scripts=['DI5-fallback-lifecycle.cjs','DI5.4-run.cjs','DI5.4-preserve.cjs','DI5.4-handoff.cjs'].map(p=>'.swarm/DI-run/reports/'+p);
function apply(file,content){
 const old=fs.existsSync(file)?fs.readFileSync(file,'utf8'):null;if(old===content)return;
 const lines=s=>s.replace(/\n$/,'').split('\n');
 const body=old===null?'*** Add File: '+file+'\n'+lines(content).map(s=>'+'+s).join('\n'):
  '*** Update File: '+file+'\n@@\n'+lines(old).map(s=>'-'+s).join('\n')+'\n'+lines(content).map(s=>'+'+s).join('\n');
 const r=cp.spawnSync('rtk',['proxy','apply_patch'],{input:'*** Begin Patch\n'+body+'\n*** End Patch\n',encoding:'utf8'});
 assert.equal(r.status,0,r.stderr||r.stdout);console.log(r.stdout.trim());
}
if(process.argv[2]==='corrections'){
 for(const p of corrections){const old=baseline.prior.find(r=>r.path===p);assert.equal(hash(path.join(live,p)),old.sha256,'Concurrent correction to '+p);}
 for(const p of [...corrections,...scripts]){if(scripts.includes(p))assert.ok(!fs.existsSync(path.join(live,p)),'New path already exists: '+p);apply(path.join(live,p),fs.readFileSync(path.join(root,p),'utf8'));}
}else if(process.argv[2]==='evidence'){
 const generated=[];
 function walk(d){for(const e of fs.readdirSync(path.join(root,d),{withFileTypes:true})){const p=path.join(d,e.name);if(e.isDirectory())walk(p);else if(e.isFile())generated.push(p);}}
 walk(dir);
 for(const p of generated){const dst=path.join(live,p);assert.ok(!fs.existsSync(dst),'Evidence already exists: '+p);fs.mkdirSync(path.dirname(dst),{recursive:true});fs.copyFileSync(path.join(root,p),dst);}
 apply(path.join(root,'.swarm/DI-run/reports/DI5.4.md'),fs.readFileSync(path.join(live,'.swarm/DI-run/reports/DI5.4.md'),'utf8'));
 const manifests=['.swarm/DI-run/manifests/DI5.4.files','.swarm/manifests/DI5.4.files'];
 const all=[...new Set([...baseline.prior.map(r=>r.path),...scripts,...generated,'.swarm/DI-run/reports/DI5.4.md',...manifests])].sort();
 for(const p of manifests){apply(path.join(live,p),all.join('\n')+'\n');apply(path.join(root,p),all.join('\n')+'\n');}
 for(const p of all)assert.ok(fs.existsSync(path.join(live,p)),p);
 console.log('Cumulative manifests: '+all.length+' paths, including all396 prior paths.');
}else throw Error('Choose corrections or evidence');
