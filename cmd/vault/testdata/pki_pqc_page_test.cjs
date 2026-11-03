const assert=require('node:assert/strict'), fs=require('node:fs'), path=require('node:path'), vm=require('node:vm');
const elements=new Map();
function element(){return {textContent:'',className:'',children:[],setAttribute(){},replaceChildren(){this.children=[]},append(...children){this.children.push(...children)}}}
const document={getElementById(id){if(!elements.has(id))elements.set(id,element());return elements.get(id)},createElement:element};
const page=fs.readFileSync(path.join(__dirname,'../pki-pqc/index.html'),'utf8');
let serial='one', fail=false, exchangeGroup='MLKEM768';
const context=vm.createContext({document,Date,console,setInterval(){},fetch:async()=>({ok:!fail,json:async()=>fail?{error:'TLS verification failed'}:{
 frontend_verification:{chain_and_hostname_verified:true,backend_dns:'backend.hal-pqc.svc.cluster.local'},
 browser_connection:{protocol:'TLSv1.3',key_exchange_group:'X25519'},
 backend_response:{authenticated:true,connection:{peer_certificate:{chain_verified:true,public_key_algorithm:'ML-DSA-65',serial_number:serial,
 valid_from:new Date(Date.now()-1000).toISOString(),valid_until:new Date(Date.now()+10000).toISOString()},tls_session:{protocol:'TLSv1.3',peer_handshake_signature_scheme:'mldsa65',local_handshake_signature_scheme:'mldsa65',key_exchange_group:exchangeGroup}}}
}})});
vm.runInContext(page.split('<script>')[1].split('</script>')[0],context);
(async()=>{
 const button=document.getElementById('test');
 await button.onclick();assert.match(document.getElementById('rotation-result').textContent,/First observed/);
 assert.match(document.getElementById('cert-lifetime').textContent,/remaining.*one/);
 serial='two';await button.onclick();assert.match(document.getElementById('rotation-result').textContent,/Certificate rotated.*two.*one/);
 await button.onclick();assert.match(document.getElementById('rotation-result').textContent,/Same certificate/);
 exchangeGroup='X25519MLKEM768';await button.onclick();
 assert.equal(document.getElementById('diagram').className,'diagram');
 assert.match(document.getElementById('status').textContent,/did not meet the required algorithms/);
 exchangeGroup='MLKEM768';
 fail=true;await button.onclick();assert.match(document.getElementById('rotation-result').textContent,/unconfirmed/);
 assert.equal(document.getElementById('diagram').className,'diagram');assert.equal(button.disabled,false);
 console.log('Page: MLKEM768 evidence, rotation, hybrid rejection and failed connection states passed');
})().catch(e=>{console.error(e);process.exitCode=1});
