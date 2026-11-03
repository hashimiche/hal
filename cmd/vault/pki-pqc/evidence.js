/* NGINX owns TLS and certificate verification. This code only formats evidence
 * and checks explicit EKU policy on the verified connection's peer certificate.
 * No identity is read from HTTP headers or certificate files. */
function field(buffer, offset) {
    let start = offset, tag = buffer[offset++], size = buffer[offset++];
    if (size & 128) {
        let count = size & 127;
        if (!count || count > 4) throw Error('Unsupported DER length');
        size = 0;
        while (count--) size = size * 256 + buffer[offset++];
    }
    if (offset + size > buffer.length || offset <= start || !Number.isFinite(size)) throw Error('Invalid DER bounds');
    return {tag, bytes: buffer.subarray(offset, offset + size), end: offset + size};
}
function parts(buffer) {
    let result = [], offset = 0;
    while (offset < buffer.length) { let value = field(buffer, offset); result.push(value); offset = value.end; }
    return result;
}
function oid(buffer) {
    let values = [], value = 0;
    for (let index = 0; index < buffer.length; index++) {
        let byte = buffer[index];
        value = value * 128 + (byte & 127);
        if (!(byte & 128)) { values.push(value); value = 0; }
    }
    if (!values.length || value) throw Error('Invalid OID');
    let first = values.shift(), arc = first < 40 ? 0 : first < 80 ? 1 : 2;
    return [arc, first - arc * 40].concat(values).join('.');
}
function certificate(pem) {
    let encoded = pem.replace(/-----[^-]+-----/g, '').replace(/\s/g, '');
    let outer = parts(field(Buffer.from(encoded, 'base64'), 0).bytes);
    let tbs = parts(outer[0].bytes), base = tbs[0].tag === 160 ? 1 : 0;
    let key = oid(parts(parts(tbs[base + 5].bytes)[0].bytes)[0].bytes);
    let signature = oid(parts(outer[1].bytes)[0].bytes);
    let names = [], usages = [];
    for (let i = 0; i < tbs.length; i++) {
        let entry = tbs[i];
        if (entry.tag !== 163) continue;
        let extensions = parts(field(entry.bytes, 0).bytes);
        for (let j = 0; j < extensions.length; j++) {
            let extension = extensions[j];
            let values = parts(extension.bytes), id = oid(values[0].bytes);
            if (id !== '2.5.29.17' && id !== '2.5.29.37') continue;
            let decoded = parts(field(values[values.length - 1].bytes, 0).bytes);
            if (id === '2.5.29.17') names = decoded.filter(x => x.tag === 130 || x.tag === 134).map(x => x.bytes.toString());
            else usages = decoded.map(x => oid(x.bytes));
        }
    }
    let algorithms = {'2.16.840.1.101.3.4.3.18': 'ML-DSA-65'};
    return {public_key_algorithm: algorithms[key] || key, certificate_signature_algorithm: algorithms[signature] || signature,
        subject_alternative_names: names, extended_key_usage: usages.map(x => x === '1.3.6.1.5.5.7.3.2' ? 'TLS Web Client Authentication' : x === '1.3.6.1.5.5.7.3.1' ? 'TLS Web Server Authentication' : x)};
}
function backend(r) {
    let v = r.variables;
    if (v.ssl_client_verify !== 'SUCCESS') { r.return(403); return; }
    try {
        let peer = certificate(v.ssl_client_raw_cert);
        if (peer.public_key_algorithm !== 'ML-DSA-65' || !peer.extended_key_usage.includes('TLS Web Client Authentication')
            || v.ssl_protocol !== 'TLSv1.3' || v.ssl_client_sigalg !== 'mldsa65' || v.ssl_curve !== 'MLKEM768') {
            r.return(403, '{"error":"Required ML-DSA, explicit clientAuth EKU or ML-KEM-768 policy not met"}'); return;
        }
        peer.subject = v.ssl_client_s_dn; peer.issuer = v.ssl_client_i_dn; peer.serial_number = v.ssl_client_serial;
        peer.valid_from = v.ssl_client_v_start; peer.valid_until = v.ssl_client_v_end;
        peer.chain_verified = true; peer.trust_anchor = null;
        r.warn('authenticated request processed');
        r.headersOut['Content-Type'] = 'application/json';
        r.return(200, JSON.stringify({message: 'I trust your certificate', authenticated: true, connection: {peer_certificate: peer,
            tls_session: {protocol: v.ssl_protocol, cipher_suite: v.ssl_cipher, peer_handshake_signature_scheme: v.ssl_client_sigalg,
                local_handshake_signature_scheme: v.ssl_sigalg, key_exchange_group: v.ssl_curve}}}));
    } catch (e) { r.error('Verified-peer metadata error: ' + e.message); r.return(500, '{"error":"Verified-peer metadata could not be decoded"}'); }
}
async function test(r) {
    let reply = await r.subrequest('/_backend', {method: 'GET'});
    r.headersOut['Content-Type'] = 'application/json';
    if (reply.status !== 200) { r.return(502, JSON.stringify({error: 'NGINX upstream TLS verification or backend policy failed; inspect frontend/backend logs', upstream_status: reply.status})); return; }
    try {
        r.return(200, JSON.stringify({frontend_observed_backend: null,
            frontend_verification: {source: 'NGINX verified HTTPS proxy; upstream peer metadata is not exposed by HTTP proxy bindings',
                backend_dns: r.variables.pqc_backend_dns, chain_and_hostname_verified: true},
            browser_connection: {protocol: r.variables.ssl_protocol, cipher_suite: r.variables.ssl_cipher, key_exchange_group: r.variables.ssl_curve},
            backend_response: JSON.parse(reply.responseText)}));
    } catch (e) { r.return(502, '{"error":"Invalid authenticated backend response"}'); }
}
export default {backend, test};
