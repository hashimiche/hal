function report(r) {
    const v = r.variables;
    const observed = {
        sni: v.ssl_server_name || null,
        tls_version: v.ssl_protocol || null,
        cipher_suite: v.ssl_cipher || null,
        local_handshake_signature_scheme: v.ssl_sigalg || null,
        negotiated_group: v.ssl_curve || null
    };
    const configured = {
        mode: v.agility_mode,
        certificate_authentication: v.agility_algorithm,
        certificate_dns_name: v.agility_dns,
        certificate_sha256_fingerprint: v.agility_fingerprint,
        required_group: v.agility_group
    };
    const valid = observed.sni === configured.certificate_dns_name &&
        observed.tls_version === 'TLSv1.3' &&
        observed.negotiated_group === configured.required_group &&
        (configured.mode === 'pqc' || configured.mode === 'pure'
            ? observed.local_handshake_signature_scheme === 'mldsa65'
            : observed.local_handshake_signature_scheme === 'rsa_pss_rsae_sha256');
    r.headersOut['Content-Type'] = 'application/json';
    r.return(valid ? 200 : 409, JSON.stringify({
        configured,
        server_observed: observed,
        server_policy_match: valid,
        browser_trust: 'Inspect Chrome certificate details; JavaScript cannot read browser trust decisions.'
    }));
}
export default {report};
