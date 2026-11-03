import fs from 'fs';

// One atomic shared-dictionary value keeps each certificate and key together.
// The cached bundle variable pins that pair for the duration of a handshake.
function bundle(r) { return ngx.shared.identities.get('current') || ''; }
function cert(r) { return JSON.parse(r.variables.identity_bundle || '{}').certificate || ''; }
function key(r) { return JSON.parse(r.variables.identity_bundle || '{}').private_key || ''; }
function health(r) {
    const value = JSON.parse(ngx.shared.identities.get('current') || '{}');
    r.return(value.expiration * 1000 > Date.now() + 1000 ? 200 : 503);
}
async function request(config, path, body) {
    const reply = await ngx.fetch('http://127.0.0.1:8081/v1/' + path, {
        method: 'POST', headers: {'X-Vault-Token': config.token, 'Content-Type': 'application/json'},
        body: JSON.stringify(body)
    });
    // Never log response bodies, certificates, keys or tokens on errors.
    if (!reply.ok) throw Error('Vault request failed: HTTP ' + reply.status);
    return (await reply.json());
}
async function rotate() {
    const shared = ngx.shared.identities;
    // Prevent overlapping periodic invocations during slow Vault requests.
    if (!shared.add('busy', '1', 5000)) return;
    try {
        const config = JSON.parse(fs.readFileSync('/identity/rotation.json', 'utf8'));
        const now = Date.now();
        if (Number(shared.get('token_renew_at') || 0) <= now) {
            const reply = await request(config, 'auth/token/renew-self', {});
            if (!reply.auth || !reply.auth.renewable || !(reply.auth.lease_duration > 0)) throw Error('Issuance token is not renewable');
            shared.set('token_renew_at', String(now + reply.auth.lease_duration * 500));
        }
        if (Number(shared.get('issue_at') || 0) > now) return;
        const reply = await request(config, config.issue_path, config.issue);
        const data = reply.data;
        if (!data || !data.certificate || !data.private_key || !data.serial_number ||
            !Array.isArray(data.ca_chain) || !data.ca_chain.length || !(data.expiration * 1000 > Date.now() + 2000)) {
            throw Error('Incomplete or nearly expired Vault identity');
        }
        const remaining = data.expiration * 1000 - Date.now();
        shared.set('current', JSON.stringify({certificate: [data.certificate].concat(data.ca_chain).join('\n'),
            private_key: data.private_key, expiration: data.expiration, serial_number: data.serial_number}));
        shared.set('issue_at', String(Date.now() + Math.min(config.ttl_ms / 2, remaining / 2)));
        ngx.log(ngx.INFO, 'Rotated workload identity; serial=' + data.serial_number);
    } catch (e) {
        ngx.log(ngx.ERR, 'Workload identity rotation failed; retaining previous identity until expiry. ' + e.message);
    } finally { shared.delete('busy'); }
}
export default {bundle, cert, key, health, rotate};
