// Local crypto agility benchmark. Built by benchmark_crypto_agility.py against
// the host OpenSSL 3.5+ library; no Vault credentials or CA private keys used.
#define _POSIX_C_SOURCE 200809L
#include <arpa/inet.h>
#include <errno.h>
#include <openssl/err.h>
#include <openssl/pem.h>
#include <openssl/ssl.h>
#include <openssl/x509.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <strings.h>
#include <sys/socket.h>
#include <sys/time.h>
#include <time.h>
#include <unistd.h>

#define CIPHER "TLS_AES_256_GCM_SHA384"
#define BODY "HAL crypto agility benchmark v1"

typedef struct {
    const char *name;
    const char *dns;
    const char *group;
    const char *root_file;
    const char *leaf_file;
    int port;
    SSL_CTX *ctx;
    unsigned char expected_sha256[EVP_MAX_MD_SIZE];
    unsigned int expected_len;
} bench_mode_t;

static bench_mode_t modes[] = {
    {"traditional", "traditional.agility.localhost", "X25519", "traditional-root.pem", "traditional.pem", 8094, NULL, {0}, 0},
    {"hybrid", "hybrid.agility.localhost", "X25519MLKEM768", "traditional-root.pem", "hybrid.pem", 8095, NULL, {0}, 0},
    {"pqc", "pqc.agility.localhost", "X25519MLKEM768", "pqc-root.pem", "pqc.pem", 8096, NULL, {0}, 0},
    {"pure", "pure.agility.localhost", "MLKEM768", "pqc-root.pem", "pure.pem", 8097, NULL, {0}, 0}
};

static void die(const char *mode, const char *message) {
    fprintf(stderr, "%s: %s\n", mode, message);
    ERR_print_errors_fp(stderr);
    exit(1);
}

static double milliseconds(void) {
    struct timespec ts;
    if (clock_gettime(CLOCK_MONOTONIC, &ts) != 0) die("clock", strerror(errno));
    return (double)ts.tv_sec * 1000.0 + (double)ts.tv_nsec / 1000000.0;
}

static void certificate_sha256(X509 *cert, unsigned char *digest, unsigned int *length, const char *mode) {
    if (!X509_digest(cert, EVP_sha256(), digest, length) || *length != 32) {
        die(mode, "cannot calculate certificate SHA-256");
    }
}

static void init_mode(bench_mode_t *mode, const char *state_dir) {
    char root[1024], leaf[1024];
    if (snprintf(root, sizeof(root), "%s/%s", state_dir, mode->root_file) >= (int)sizeof(root) ||
        snprintf(leaf, sizeof(leaf), "%s/%s", state_dir, mode->leaf_file) >= (int)sizeof(leaf)) {
        die(mode->name, "state path too long");
    }
    FILE *file = fopen(leaf, "r");
    if (!file) die(mode->name, "cannot open expected leaf certificate");
    X509 *expected = PEM_read_X509(file, NULL, NULL, NULL);
    fclose(file);
    if (!expected) die(mode->name, "cannot parse expected leaf certificate");
    certificate_sha256(expected, mode->expected_sha256, &mode->expected_len, mode->name);
    X509_free(expected);

    mode->ctx = SSL_CTX_new(TLS_client_method());
    if (!mode->ctx || !SSL_CTX_set_min_proto_version(mode->ctx, TLS1_3_VERSION) ||
        !SSL_CTX_set_max_proto_version(mode->ctx, TLS1_3_VERSION) ||
        !SSL_CTX_set_ciphersuites(mode->ctx, CIPHER) ||
        !SSL_CTX_set1_groups_list(mode->ctx, mode->group) ||
        !SSL_CTX_load_verify_locations(mode->ctx, root, NULL)) {
        die(mode->name, "cannot configure TLS 1.3, cipher, group, or trust root");
    }
    SSL_CTX_set_verify(mode->ctx, SSL_VERIFY_PEER, NULL);
    SSL_CTX_set_session_cache_mode(mode->ctx, SSL_SESS_CACHE_OFF);
    SSL_CTX_set_options(mode->ctx, SSL_OP_NO_TICKET);
}

static void sample(bench_mode_t *mode, int sequence, int output) {
    int fd = socket(AF_INET, SOCK_STREAM, 0);
    if (fd < 0) die(mode->name, "socket creation failed");
    struct timeval timeout = {.tv_sec = 5, .tv_usec = 0};
    if (setsockopt(fd, SOL_SOCKET, SO_RCVTIMEO, &timeout, sizeof(timeout)) != 0 ||
        setsockopt(fd, SOL_SOCKET, SO_SNDTIMEO, &timeout, sizeof(timeout)) != 0) {
        die(mode->name, "socket timeout setup failed");
    }
    struct sockaddr_in address = {.sin_family = AF_INET, .sin_port = htons((unsigned short)mode->port)};
    if (inet_pton(AF_INET, "127.0.0.1", &address.sin_addr) != 1) die(mode->name, "invalid loopback address");
    double start = milliseconds();
    if (connect(fd, (struct sockaddr *)&address, sizeof(address)) != 0) die(mode->name, "TCP connect failed");
    double connected = milliseconds();

    SSL *ssl = SSL_new(mode->ctx);
    if (!ssl || !SSL_set_tlsext_host_name(ssl, mode->dns) || !SSL_set1_host(ssl, mode->dns) ||
        !SSL_set_fd(ssl, fd)) {
        die(mode->name, "TLS client setup failed");
    }
    double handshake_start = milliseconds();
    if (SSL_connect(ssl) != 1) {
        die(mode->name, "verified TLS handshake failed");
    }
    double handshaken = milliseconds();
    if (SSL_get_verify_result(ssl) != X509_V_OK || SSL_version(ssl) != TLS1_3_VERSION ||
        strcmp(SSL_get_cipher_name(ssl), CIPHER) != 0 || SSL_session_reused(ssl)) {
        die(mode->name, "TLS verification, version, cipher, or full-handshake requirement failed");
    }
    const char *group = SSL_get0_group_name(ssl);
    if (!group || strcasecmp(group, mode->group) != 0) die(mode->name, "negotiated group differs from required group");

    X509 *cert = SSL_get1_peer_certificate(ssl);
    if (!cert) die(mode->name, "server sent no certificate");
    unsigned char digest[EVP_MAX_MD_SIZE];
    unsigned int digest_len = 0;
    certificate_sha256(cert, digest, &digest_len, mode->name);
    if (digest_len != mode->expected_len || memcmp(digest, mode->expected_sha256, digest_len) != 0) {
        die(mode->name, "server selected an unexpected leaf certificate");
    }
    char subject[256];
    if (X509_NAME_get_text_by_NID(X509_get_subject_name(cert), NID_commonName, subject, sizeof(subject)) < 0 ||
        strcmp(subject, mode->dns) != 0) die(mode->name, "certificate subject differs from mode DNS name");
    EVP_PKEY *key = X509_get_pubkey(cert);
    if (!key) die(mode->name, "certificate public key unavailable");
    const char *algorithm = EVP_PKEY_get0_type_name(key);
    if (!algorithm) die(mode->name, "certificate algorithm unavailable");
    char fingerprint[65];
    for (unsigned int j = 0; j < digest_len; j++) snprintf(fingerprint + j * 2, 3, "%02x", digest[j]);

    const STACK_OF(X509) *chain = SSL_get_peer_cert_chain(ssl);
    if (!chain || sk_X509_num(chain) != 2) die(mode->name, "server did not send leaf and intermediate certificates");
    int chain_bytes = 0;
    for (int j = 0; j < sk_X509_num(chain); j++) chain_bytes += i2d_X509(sk_X509_value(chain, j), NULL);

    char request[256];
    int request_len = snprintf(request, sizeof(request),
        "GET /benchmark HTTP/1.1\r\nHost: %s\r\nConnection: close\r\nAccept: text/plain\r\n\r\n", mode->dns);
    if (request_len <= 0 || request_len >= (int)sizeof(request)) die(mode->name, "HTTP request too long");
    int written = 0;
    while (written < request_len) {
        int n = SSL_write(ssl, request + written, request_len - written);
        if (n <= 0) die(mode->name, "HTTP request write failed");
        written += n;
    }
    double requested = milliseconds();
    char response[16384];
    int n = SSL_read(ssl, response, sizeof(response) - 1);
    if (n <= 0) die(mode->name, "HTTP first byte read failed");
    double first_byte = milliseconds();
    int used = n;
    while (used < (int)sizeof(response) - 1) {
        n = SSL_read(ssl, response + used, sizeof(response) - 1 - used);
        if (n <= 0) break;
        used += n;
    }
    response[used] = '\0';
    char *body = strstr(response, "\r\n\r\n");
    if (strncmp(response, "HTTP/1.1 200 ", 13) != 0 || !body || strcmp(body + 4, BODY) != 0) {
        die(mode->name, "HTTP response differs from the common benchmark body");
    }
    if (output) {
        printf("%d,%s,%s,%d,TLSv1.3,%s,%s,%s,%s,%s,false,%d,%.6f,%.6f,%.6f,%.6f\n",
            sequence, mode->name, mode->dns, mode->port, CIPHER, subject, fingerprint,
            algorithm, group, chain_bytes, connected - start, handshaken - handshake_start,
            first_byte - requested, first_byte - start);
    }
    EVP_PKEY_free(key);
    X509_free(cert);
    SSL_free(ssl);
    close(fd);
}

int main(int argc, char **argv) {
    if (argc != 4) {
        fprintf(stderr, "usage: crypto_agility_bench <runs>=30+ <warmups> <state-dir>\n");
        return 2;
    }
    char *end = NULL;
    long runs = strtol(argv[1], &end, 10);
    if (!end || *end || runs < 30 || runs > 10000) die("arguments", "runs must be 30..10000");
    long warmups = strtol(argv[2], &end, 10);
    if (!end || *end || warmups < 0 || warmups > 1000) die("arguments", "warmups must be 0..1000");
    for (size_t m = 0; m < sizeof(modes) / sizeof(modes[0]); m++) init_mode(&modes[m], argv[3]);
    for (long i = 0; i < warmups; i++) {
        for (size_t m = 0; m < sizeof(modes) / sizeof(modes[0]); m++) sample(&modes[m], 0, 0);
    }
    puts("sequence,mode,dns,port,tls_version,cipher_suite,certificate_subject,certificate_sha256,certificate_algorithm,negotiated_group,session_reused,chain_der_bytes,tcp_connect_ms,tls_handshake_ms,request_ttfb_ms,total_ttfb_ms");
    for (long i = 1; i <= runs; i++) {
        for (size_t m = 0; m < sizeof(modes) / sizeof(modes[0]); m++) sample(&modes[m], (int)i, 1);
    }
    for (size_t m = 0; m < sizeof(modes) / sizeof(modes[0]); m++) SSL_CTX_free(modes[m].ctx);
    return 0;
}
