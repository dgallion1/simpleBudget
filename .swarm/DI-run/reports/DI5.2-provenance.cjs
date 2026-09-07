// Correction-only provenance and retained DI5.1 coverage. Read-only live sources.
const fs=require('node:fs'),path=require('node:path'),crypto=require('node:crypto'),assert=require('node:assert/strict');
const root=process.env.DI5_SOURCE_ROOT,live=process.env.DI5_LIVE_ROOT,out=process.env.DI5_OUTPUT;
assert.ok(root&&root.startsWith('/tmp/')&&live&&out);fs.mkdirSync(out,{recursive:true});
const hash=p=>crypto.createHash('sha256').update(fs.readFileSync(p)).digest('hex');
const before=JSON.parse(fs.readFileSync(path.join(root,'evidence/DI5.1-before.json')));
assert.equal(Object.keys(before).length,193);
const allowed='.swarm/DI-run/reports/DI5-refresh-browser.cjs';
const retained=Object.entries(before).map(([p,sha])=>{
 const current=hash(path.join(live,p));if(p!==allowed)assert.equal(current,sha,'DI5.1 preservation: '+p);
 return {path:p,before:sha,after:current,strictRefreshStrengthening:p===allowed};
});
const map=JSON.parse(fs.readFileSync('/tmp/DI5-a11y-baseline.Yd8pno/artifacts/source-hashes.json'));
const di4=fs.readFileSync(path.join(live,'.swarm/DI-run/manifests/DI4.2.files'),'utf8').trim().split('\n').filter(p=>/^(web|internal|cmd)\//.test(p));
const corrections=['web/static/js/page-refresh.js','web/static/js/page-refresh.test.cjs'];
const permitted=new Set([...di4,...corrections]);
const sources=map.files.map(f=>{
 const current=hash(path.join(root,f.path));if(!permitted.has(f.path))assert.equal(current,f.sha256,'unexpected source change: '+f.path);
 if(permitted.has(f.path))assert.equal(current,hash(path.join(live,f.path)),'live/snapshot mismatch: '+f.path);
 return {path:f.path,before:f.sha256,after:current,DI4:di4.includes(f.path),correction:corrections.includes(f.path)};
});
for(const f of retained.filter(f=>f.path.endsWith('.go')))assert.equal(hash(path.join(root,f.path)),f.after);
fs.writeFileSync(path.join(out,'preservation.json'),JSON.stringify({baseline:'/tmp/budget2-DI4-base.KPl7oY',overlay:'DI4.2 + frozen DI5.1 + explicit DI5.2 corrections',retained,sources,corrections},null,2));
console.log('193 DI5.1 paths retained;192 byte-identical, only strict refresh harness strengthened. '+sources.length+' source files checked; only two scoped refresh source/test changes beyond DI4.2.');

