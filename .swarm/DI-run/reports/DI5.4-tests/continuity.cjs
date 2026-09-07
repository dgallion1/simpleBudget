const fs=require('fs'),crypto=require('crypto');
const hash=f=>crypto.createHash('sha256').update(fs.readFileSync(f)).digest('hex');
const prior=JSON.parse(fs.readFileSync('/tmp/DI5-Go-checker.vZNpAR/tmp/checker-evidence/source-hashes.json'));
const differences=Object.entries(prior.hashes).filter(([p,h])=>hash('/tmp/DI5-fourth-tests.QbdTEf/'+p)!==h).map(([p])=>p);
const goSame=prior.goPromotions.every(p=>hash('/tmp/DI5-fourth-tests.QbdTEf/'+p)===prior.hashes[p]);
console.log(JSON.stringify({prior:'/tmp/DI5-Go-checker.vZNpAR/tmp/checker-evidence/source-hashes.json',count:Object.keys(prior.hashes).length,differences,goSame},null,2));
if(!goSame||differences.some(p=>!['web/static/js/page-refresh.js','web/static/js/page-refresh.test.cjs'].includes(p)))process.exitCode=1;
