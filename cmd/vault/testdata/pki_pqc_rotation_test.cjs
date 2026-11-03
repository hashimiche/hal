const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
let now = 1700000000000, serial = 0, fail = false, requests = [];
const values = new Map(), logs = [];
const shared = {
  get(k) { const v = values.get(k); return v && (!v.until || v.until > now) ? v.value : undefined; },
  set(k, value, timeout) { values.set(k, {value, until: timeout ? now + timeout : 0}); },
  add(k, value, timeout) { if (this.get(k) !== undefined) return false; this.set(k,value,timeout); return true; },
  delete(k) { values.delete(k); }
};
const config = {token:'private-token', issue_path:'hal-pqc-int/issue/frontend', ttl_ms:10000, issue:{ttl:'10s'}};
const source = fs.readFileSync(path.join(__dirname,'../pki-pqc/rotation.js'),'utf8')
  .replace("import fs from 'fs';",'').replace('export default {bundle, cert, key, health, rotate};','');
const context = vm.createContext({fs:{readFileSync:()=>JSON.stringify(config)}, Date:{now:()=>now},
  ngx:{shared:{identities:shared},INFO:1,ERR:2,log:(_,message)=>logs.push(message),fetch:async(url,options)=>{
    requests.push({url,options});
    if(url.endsWith('renew-self')) return {ok:true,json:async()=>({auth:{renewable:true,lease_duration:3600}})};
    if(fail) return {ok:false,status:503,json:async()=>({private_key:'must-never-be-logged'})};
    const n=++serial;
    return {ok:true,json:async()=>({data:{certificate:'cert-'+n,private_key:'key-'+n,ca_chain:['intermediate'],expiration:now/1000+10,serial_number:String(n)}})};
  }}
});
vm.runInContext(source,context);
(async()=>{
  assert.equal(context.bundle({}), '');
  let status;
  context.health({return:code=>status=code});assert.equal(status,503);
  await context.rotate();
  const first=shared.get('current');assert.equal(JSON.parse(first).serial_number,'1');
  const request={variables:{identity_bundle:first}};
  assert.equal(context.cert(request),'cert-1\nintermediate');assert.equal(context.key(request),'key-1');
  context.health({return:code=>status=code});assert.equal(status,200);
  assert.equal(requests.length,2);assert.equal(requests[1].options.headers['X-Vault-Token'],'private-token');
  now+=4000;await context.rotate();assert.equal(serial,1);
  now+=1000;await context.rotate();assert.equal(serial,2);
  // An in-progress handshake retains its original matched certificate/key pair.
  assert.equal(context.cert(request),'cert-1\nintermediate');assert.equal(context.key(request),'key-1');
  const second=shared.get('current');
  now+=5000;fail=true;await context.rotate();assert.equal(shared.get('current'),second);
  assert(logs.some(v=>v.includes('HTTP 503')));assert(!logs.some(v=>/private-token|key-[12]|must-never/.test(v)));
  now+=6000;context.health({return:code=>status=code});assert.equal(status,503);
  fail=false;await context.rotate();assert.equal(serial,3);context.health({return:code=>status=code});assert.equal(status,200);
  shared.add('busy','1',5000);now+=6000;shared.add('busy','1',5000);await context.rotate();assert.equal(serial,3);
  shared.delete('busy');now+=1800000;await context.rotate();assert.equal(serial,4);
  assert.equal(requests.filter(r=>r.url.endsWith('renew-self')).length,2);
  console.log('Rotation: issuance cadence, atomic identities, retries, expiry, recovery, token renewal and overlap checks passed');
})().catch(e=>{console.error(e);process.exitCode=1});
