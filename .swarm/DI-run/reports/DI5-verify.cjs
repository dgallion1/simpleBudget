// Run only in an isolated baseline+DI4.2+DI5 snapshot. No git or deployment.
const fs=require('node:fs'),path=require('node:path'),crypto=require('node:crypto'),{spawn}=require('node:child_process'),assert=require('node:assert/strict');
const root=process.env.DI5_SOURCE_ROOT,out=process.env.DI5_OUTPUT,agents=process.env.DI5_AGENTS_ROOT;
assert.ok(root&&root.startsWith('/tmp/'));assert.ok(out&&out.startsWith(root+'/'));assert.ok(agents);fs.mkdirSync(out,{recursive:true});
const commands=[
 ['build',['go','build','-buildvcs=false','./...'],root],
 ['vet',['go','vet','./...'],root],
 ['staticcheck',['staticcheck','./...'],root],
 ['tests',['go','test','-count=1','./...'],root],
 ['promoted',['go','test','-count=1','-v','./internal/services/insights','./internal/handlers/dashboard','./internal/handlers/insights','./internal/templates','./internal/services/mcpsvc/spend','./internal/services/mcpsvc/ledger','./cmd/server','-run','^TestDI5'],root],
 ['css',['make','COMMIT=di5-frozen','css-verify'],root],
 ['agents2-smoketest',['bash','smoketest/gate/run_tests.sh'],agents]
];
(async()=>{const results=[];for(const [name,args,cwd] of commands){
 const log=fs.openSync(path.join(out,name+'.log'),'w'),start=new Date().toISOString();
 const code=await new Promise((resolve,reject)=>{const p=spawn('rtk',['proxy',...args],{cwd,stdio:['ignore',log,log]});p.on('error',reject);p.on('close',resolve);});
 fs.closeSync(log);results.push({name,command:['rtk','proxy',...args],cwd,start,exit:code});fs.writeFileSync(path.join(out,'commands.json'),JSON.stringify(results,null,2));console.log(name+': exit '+code);assert.equal(code,0,name+' failed; see '+path.join(out,name+'.log'));
}})().catch(e=>{console.error(e);process.exitCode=1;});
