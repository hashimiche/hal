#!/usr/bin/env python3
"""Internal Vault PKI integration runner; invoke TestPQCIntegration via go test.
Uses Vault-issued positive fixtures and temporary stock NGINX containers.
"""
import argparse
from contextlib import ExitStack
import json
import os
from pathlib import Path
import re
import shutil
import tempfile
import socket
import ssl
import subprocess
import time
import urllib.request

def issue_and_verify(args, cleanup):
    os.umask(63)
    args.state.mkdir(parents=True, exist_ok=True, mode=448)
    prod = Path.home() / '.hal/vault-prod'
    addr = os.environ.get('VAULT_ADDR', 'https://127.0.0.1:8200')
    token = os.environ.get('VAULT_TOKEN') or json.loads((prod / 'init.json').read_text())['root_token']
    ctx = None
    if addr.startswith('https://'):
        ctx = ssl.create_default_context(cafile=os.environ.get('VAULT_CACERT', str(prod / 'certs/cert.pem')))

    def api(path, body=None, method=None):
        req = urllib.request.Request(addr + '/v1/' + path, data=None if body is None else json.dumps(body).encode(), headers={'X-Vault-Token': token, 'Content-Type': 'application/json'}, method=method)
        with urllib.request.urlopen(req, context=ctx, timeout=30) as r:
            raw = r.read()
            return json.loads(raw).get('data', {}) if raw else {}
    req = urllib.request.Request(addr + '/v1/sys/health')
    with urllib.request.urlopen(req, context=ctx, timeout=10) as r:
        version = json.load(r)['version']
    numeric = tuple((int(n) for n in version.split('+')[0].split('-')[0].split('.')))
    if numeric < (2, 2, 0) or '+ent' not in version:
        raise RuntimeError('Requires existing Vault Enterprise 2.2.0+; deployment will not be replaced')
    license = api('sys/license/status')
    if not license.get('autoloaded') and (not license.get('persisted_autoload')):
        raise RuntimeError('No active Enterprise license reported')
    mounts = api('sys/mounts')
    root, inter = ('hal-pqc-test-root-' + str(os.getpid()), 'hal-pqc-test-int-' + str(os.getpid()))
    host = 'backend.hal-pqc.svc.cluster.local'
    if any((m + '/' in mounts for m in (root, inter))):
        raise RuntimeError('Diagnostic mounts already exist; inspect them before explicitly removing or reusing them')
    # Register cleanup only after creation; never adopt or remove existing mounts.
    for mount in (root, inter):
        api('sys/mounts/' + mount, {'type': 'pki', 'description': 'HAL PQC interoperability proof, issue #110', 'config': {'max_lease_ttl': '8760h'}})
        cleanup.callback(api, 'sys/mounts/' + mount, method='DELETE')
    common = {'key_type': 'ml-dsa', 'parameter_set': 'ml-dsa-65'}
    root_data = api(root + '/root/generate/internal', dict(common, common_name='HAL PQC Proof Root', ttl='8760h'))
    csr = api(inter + '/intermediate/generate/internal', dict(common, common_name='HAL PQC Proof Intermediate'))['csr']
    signed = api(root + '/root/sign-intermediate', {'csr': csr, 'common_name': 'HAL PQC Proof Intermediate', 'ttl': '4380h', 'max_path_length': 0})
    api(inter + '/intermediate/set-signed', {'certificate': signed['certificate']})
    (args.state / 'root.pem').write_text(root_data['certificate'] + '\n')
    (args.state / 'intermediate.pem').write_text(signed['certificate'] + '\n')
    for name, client in [('frontend', True), ('backend', False), ('wrong-eku', False), ('no-eku', False)]:
        role = dict(common, allow_any_name=True, enforce_hostnames=True, max_ttl='24h', client_flag=client, server_flag=not client and name != 'no-eku', key_usage=['DigitalSignature'])
        api(inter + '/roles/' + name, role)
        leaf = api(inter + '/issue/' + name, {'common_name': name, 'alt_names': host if name == 'backend' else name + '.hal-pqc.svc.cluster.local', 'ttl': '1h', 'private_key_format': 'pkcs8'})
        (args.state / (name + '.pem')).write_text(leaf['certificate'] + '\n')
        (args.state / (name + '.key')).write_text(leaf['private_key'] + '\n')
    expired = api(inter + '/issue/frontend', {'common_name': 'expired', 'ttl': '1s', 'private_key_format': 'pkcs8'})
    (args.state / 'expired.pem').write_text(expired['certificate'] + '\n')
    (args.state / 'expired.key').write_text(expired['private_key'] + '\n')
    time.sleep(2)

    def run(*cmd):
        result = subprocess.run([args.openssl, *cmd], capture_output=True, text=True, timeout=20)
        if result.returncode:
            raise RuntimeError(' '.join(cmd) + '\n' + result.stderr)
        return result.stdout + result.stderr
    evidence = ['Vault ' + version, run('version', '-a'), run('list', '-providers')]
    for name in ('root', 'intermediate', 'frontend', 'backend', 'wrong-eku'):
        evidence.append(run('x509', '-in', str(args.state / (name + '.pem')), '-noout', '-text'))
    for name, purpose in [('frontend', 'sslclient'), ('backend', 'sslserver')]:
        cmd = ['verify', '-CAfile', str(args.state / 'root.pem'), '-untrusted', str(args.state / 'intermediate.pem'), '-purpose', purpose]
        if name == 'backend':
            cmd += ['-verify_hostname', host]
        evidence.append(run(*cmd, str(args.state / (name + '.pem'))))
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', 0))
        port = sock.getsockname()[1]
    server_cmd = [args.openssl, 's_server', '-accept', '127.0.0.1:' + str(port), '-cert', str(args.state / 'backend.pem'), '-key', str(args.state / 'backend.key'), '-cert_chain', str(args.state / 'intermediate.pem'), '-CAfile', str(args.state / 'root.pem'), '-Verify', '2', '-verify_return_error', '-purpose', 'sslclient', '-tls1_3', '-groups', 'MLKEM768', '-sigalgs', 'mldsa65', '-www']
    with (args.state / 'server.log').open('w') as log:
        server = subprocess.Popen(server_cmd, stdout=log, stderr=log)
        try:
            time.sleep(0.5)
            base = [args.openssl, 's_client', '-connect', '127.0.0.1:' + str(port), '-servername', host, '-verify_hostname', host, '-CAfile', str(args.state / 'root.pem'), '-verify_return_error', '-tls1_3', '-groups', 'MLKEM768', '-sigalgs', 'mldsa65', '-ign_eof']
            identity = ['-cert', str(args.state / 'frontend.pem'), '-key', str(args.state / 'frontend.key'), '-cert_chain', str(args.state / 'intermediate.pem')]
            for name, cmd, expected in [('positive', base + identity, True), ('missing-client', base, False), ('wrong-eku', base + ['-cert', str(args.state / 'wrong-eku.pem'), '-key', str(args.state / 'wrong-eku.key'), '-cert_chain', str(args.state / 'intermediate.pem')], False), ('wrong-hostname', base + identity + ['-verify_hostname', 'wrong.invalid'], False), ('classical-only', base + identity + ['-groups', 'X25519'], False), ('hybrid-only', base + identity + ['-groups', 'X25519MLKEM768'], False)]:
                result = subprocess.run(cmd, input='GET / HTTP/1.0\r\n\r\n', capture_output=True, text=True, timeout=15)
                text = result.stdout + result.stderr
                (args.state / (name + '.log')).write_text(text)
                success = result.returncode == 0 and 'HTTP/1.0 200' in text
                if success != expected:
                    raise RuntimeError(name + ' unexpected result; see ' + str(args.state / (name + '.log')))
                if success and (not all((v in text for v in ['ML-DSA-65', 'TLSv1.3']))
                                or not re.search(r'(?:Negotiated TLS1\.3 group|Server Temp Key):\s*MLKEM768(?:\s|$)', text)):
                    raise RuntimeError('Positive handshake lacks required observed algorithm evidence')
                evidence.append(name + ': PASS (HTTP success=' + str(success) + ')')
                print(evidence[-1])
        finally:
            server.terminate()
            server.wait(timeout=5)
    (args.state / 'evidence.txt').write_text('\n\n'.join(evidence))
    print('Diagnostic evidence:', args.state)

def test_nginx(args, assets):
    name = 'hal-pqc-nginx-test-' + str(os.getpid())

    def run(*cmd, **kwargs):
        return subprocess.run(cmd, check=True, capture_output=True, text=True, **kwargs).stdout
    version = subprocess.run([args.engine, 'run', '--rm', '--network', 'none', '--entrypoint', 'nginx', args.image, '-V'], check=True, capture_output=True, text=True)
    print('\n'.join(version.stderr.splitlines()[:3]), flush=True)
    print('njs ' + run(args.engine, 'run', '--rm', '--network', 'none', '--entrypoint', 'njs', args.image, '-v').strip(), flush=True)
    with tempfile.TemporaryDirectory(prefix='hal-pqc-nginx-') as temp:
        tmp = Path(temp)
        for directory in ('identity', 'trust', 'app'):
            (tmp / directory).mkdir()
        (tmp / 'identity/tls.crt').write_text((args.state / 'backend.pem').read_text() + (args.state / 'intermediate.pem').read_text())
        shutil.copy(args.state / 'backend.key', tmp / 'identity/tls.key')
        shutil.copy(args.state / 'root.pem', tmp / 'trust/root.pem')
        shutil.copy(assets / 'backend.conf', tmp / 'nginx.conf')
        shutil.copy(assets / 'evidence.js', tmp / 'app/evidence.js')
        # Local issuance is exclusively an untrusted negative fixture.
        run(args.openssl, 'req', '-x509', '-newkey', 'ML-DSA-65', '-nodes', '-keyout', str(tmp / 'rogue-root.key'), '-out', str(tmp / 'rogue-root.pem'), '-subj', '/CN=NGINX Negative Root', '-days', '1', '-addext', 'basicConstraints=critical,CA:TRUE')
        run(args.openssl, 'req', '-new', '-newkey', 'ML-DSA-65', '-nodes', '-keyout', str(tmp / 'rogue.key'), '-out', str(tmp / 'rogue.csr'), '-subj', '/CN=NGINX Negative Client')
        (tmp / 'client.ext').write_text('extendedKeyUsage=clientAuth\nkeyUsage=digitalSignature\n')
        run(args.openssl, 'x509', '-req', '-in', str(tmp / 'rogue.csr'), '-CA', str(tmp / 'rogue-root.pem'), '-CAkey', str(tmp / 'rogue-root.key'), '-set_serial', '2', '-days', '1', '-extfile', str(tmp / 'client.ext'), '-out', str(tmp / 'rogue.pem'))
        try:
            run(args.engine, 'run', '-d', '--name', name, '-p', '127.0.0.1::8443', '-p', '127.0.0.1::8444', '-v', str(tmp / 'identity') + ':/identity:ro', '-v', str(tmp / 'trust') + ':/trust:ro', '-v', str(tmp / 'app') + ':/app:ro', '-v', str(tmp / 'nginx.conf') + ':/etc/nginx/nginx.conf:ro', '--entrypoint', 'nginx', args.image, '-g', 'daemon off;')
            time.sleep(0.5)
            port = run(args.engine, 'port', name, '8443/tcp').strip().split(':')[-1]
            base = [args.openssl, 's_client', '-connect', '127.0.0.1:' + port, '-servername', 'backend.hal-pqc.svc.cluster.local', '-verify_hostname', 'backend.hal-pqc.svc.cluster.local', '-CAfile', str(args.state / 'root.pem'), '-verify_return_error', '-tls1_3', '-groups', 'MLKEM768', '-sigalgs', 'mldsa65', '-ign_eof']

            def identity(n):
                return ['-cert', str(args.state / (n + '.pem')), '-key', str(args.state / (n + '.key')), '-cert_chain', str(args.state / 'intermediate.pem')]
            tests = [('positive', base + identity('frontend'), True), ('missing-client', base, False), ('wrong-eku', base + identity('wrong-eku'), False), ('wrong-hostname', base + identity('frontend') + ['-verify_hostname', 'wrong.invalid'], False), ('untrusted-client', base + ['-cert', str(tmp / 'rogue.pem'), '-key', str(tmp / 'rogue.key'), '-cert_chain', str(tmp / 'rogue-root.pem')], False), ('untrusted-backend', base + identity('frontend') + ['-CAfile', str(tmp / 'rogue-root.pem')], False), ('classical-only', base + identity('frontend') + ['-groups', 'X25519'], False), ('hybrid-only', base + identity('frontend') + ['-groups', 'X25519MLKEM768'], False), ('missing-client-eku', base + identity('no-eku'), False), ('expired-client', base + identity('expired'), False)]
            for label, cmd, expected in tests:
                before = subprocess.run([args.engine, 'logs', name], capture_output=True, text=True).stderr.count('authenticated request processed')
                result = subprocess.run(cmd, input='GET / HTTP/1.0\r\nHost: backend\r\n\r\n', capture_output=True, text=True, timeout=15)
                text = result.stdout + result.stderr
                (args.state / ('nginx-' + label + '.log')).write_text(text)
                success = 'HTTP/1.1 200 OK' in result.stdout
                if success != expected:
                    print(subprocess.run([args.engine, 'logs', name], capture_output=True, text=True).stderr)
                    raise RuntimeError(label + ' unexpected result; ' + str(args.state / ('nginx-' + label + '.log')))
                if expected:
                    payload = result.stdout.split('\n\n', 1)[1]
                    data, _ = json.JSONDecoder().raw_decode(payload)
                    assert data['message'] == 'I trust your certificate'
                    assert data['connection']['peer_certificate']['public_key_algorithm'] == 'ML-DSA-65'
                    assert data['connection']['peer_certificate']['chain_verified']
                    assert data['connection']['peer_certificate']['trust_anchor'] is None
                    assert data['connection']['tls_session']['protocol'] == 'TLSv1.3'
                    assert data['connection']['tls_session']['local_handshake_signature_scheme'] == 'mldsa65'
                    assert data['connection']['tls_session']['peer_handshake_signature_scheme'] == 'mldsa65'
                    assert data['connection']['tls_session']['key_exchange_group'] == 'MLKEM768'
                    (args.state / 'nginx-connection.json').write_text(json.dumps(data, indent=2) + '\n')
                else:
                    logs = subprocess.run([args.engine, 'logs', name], capture_output=True, text=True).stderr
                    assert before == logs.count('authenticated request processed'), label + ' reached authenticated handler'
                print(label + ': PASS')
            # Test the actual frontend proxy; share only the test network namespace.
            front = tmp / 'frontend'
            for directory in ('identity', 'trust', 'browser', 'app'):
                (front / directory).mkdir(parents=True, exist_ok=True)
            (front / 'identity/tls.crt').write_text((args.state / 'frontend.pem').read_text() + (args.state / 'intermediate.pem').read_text())
            shutil.copy(args.state / 'frontend.key', front / 'identity/tls.key')
            shutil.copy(args.state / 'root.pem', front / 'trust/root.pem')
            shutil.copy(assets / 'evidence.js', front / 'app/evidence.js')
            run(args.openssl, 'req', '-x509', '-newkey', 'ec', '-pkeyopt', 'ec_paramgen_curve:P-256', '-nodes', '-keyout', str(front / 'browser/tls.key'), '-out', str(front / 'browser/tls.crt'), '-subj', '/CN=localhost', '-days', '1', '-addext', 'subjectAltName=DNS:localhost')
            frontport = run(args.engine, 'port', name, '8444/tcp').strip().split(':')[-1]
            template = (assets / 'frontend.conf').read_text().replace('__CLUSTER_DOMAIN__', 'cluster.local').replace('listen 8443 ssl', 'listen 8444 ssl').replace('proxy_pass https://backend.hal-pqc.svc.cluster.local:8443/', 'proxy_pass https://127.0.0.1:8443/')
            for label in ('frontend-positive', 'frontend-wrong-hostname', 'frontend-untrusted-backend'):
                before = subprocess.run([args.engine, 'logs', name], capture_output=True, text=True).stderr.count('authenticated request processed')
                config = template.replace('proxy_ssl_name backend.hal-pqc.svc.cluster.local;', 'proxy_ssl_name wrong.invalid;') if label == 'frontend-wrong-hostname' else template
                shutil.copy(tmp / 'rogue-root.pem' if label == 'frontend-untrusted-backend' else args.state / 'root.pem', front / 'trust/root.pem')
                (front / 'app/nginx.conf').write_text(config)
                frontend = name + '-frontend'
                try:
                    run(args.engine, 'run', '-d', '--name', frontend, '--network', 'container:' + name, '-v', str(front / 'identity') + ':/identity:ro', '-v', str(front / 'trust') + ':/trust:ro', '-v', str(front / 'browser') + ':/browser:ro', '-v', str(front / 'app') + ':/app:ro', '--entrypoint', 'nginx', args.image, '-c', '/app/nginx.conf', '-g', 'daemon off;')
                    time.sleep(0.4)
                    reply = run('curl', '--silent', '--show-error', '--cacert', str(front / 'browser/tls.crt'), '-X', 'POST', 'https://localhost:' + frontport + '/test', '--write-out', '\n%{http_code}')
                    body, status = reply.rsplit('\n', 1)
                    expected = '200' if label == 'frontend-positive' else '502'
                    if status != expected:
                        print(subprocess.run([args.engine, 'logs', frontend], capture_output=True, text=True).stderr)
                        raise RuntimeError(label + ' unexpected HTTP ' + status + ': ' + body)
                    data = json.loads(body)
                    if label == 'frontend-positive':
                        assert data['frontend_verification']['chain_and_hostname_verified']
                        assert data['frontend_observed_backend'] is None
                        assert data['backend_response']['connection']['tls_session']['key_exchange_group'] == 'MLKEM768'
                        assert data['browser_connection']['key_exchange_group'] in ('X25519', 'prime256v1')
                        (args.state / 'nginx-frontend-connection.json').write_text(json.dumps(data, indent=2) + '\n')
                    if label != 'frontend-positive':
                        after = subprocess.run([args.engine, 'logs', name], capture_output=True, text=True).stderr.count('authenticated request processed')
                        assert before == after, label + ' reached authenticated backend handler'
                    print(label + ': PASS')
                finally:
                    subprocess.run([args.engine, 'rm', '-f', frontend], capture_output=True)
        finally:
            subprocess.run([args.engine, 'rm', '-f', name], capture_output=True)

def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--state', type=Path, required=True)
    p.add_argument('--image', required=True)
    p.add_argument('--engine', default='podman')
    p.add_argument('--openssl', default='openssl')
    args = p.parse_args()
    source = Path(__file__).resolve().parents[3]
    if args.state.resolve().is_relative_to(source):
        p.error('integration artifacts must be outside the source tree')
    with ExitStack() as cleanup:
        issue_and_verify(args, cleanup)
        test_nginx(args, Path(__file__).resolve().parents[1] / 'pki-pqc')
if __name__ == '__main__':
    main()
