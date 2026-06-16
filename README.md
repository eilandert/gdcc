# gdcc

> A from-scratch, dependency-free **Go [DCC](https://www.dcc-servers.net/dcc/)
> (Distributed Checksum Clearinghouse) client** — `check` / `report` — for
> streaming pipelines, with zero on-disk message handling.

gdcc computes DCC's message checksums **byte-for-byte identically to the reference
`dccproc`** and speaks the client-server UDP protocol (anonymous or keyed), so the
public DCC servers accept its queries. The checksums are the part that has to be
exact; they are verified against real `dccproc` 2.3.169 in CI. gdcc is a clean
reimplementation of the DCC *protocol*, not DCC itself.

Use it two ways:

- **As a Go library** — `import "github.com/eilandert/gdcc/dcc"` and call
  `Client.Check/Report` in-process. This is how the
  [gozer](https://github.com/eilandert/gozer) backend uses it: linked directly —
  no subprocess, no `/var/dcc`, no set-uid `dccproc`.
- **As a CLI** — `gdcc check|report|cksum` (message on stdin, never touches disk),
  plus a `gdcc serve` HTTP sidecar.

## Quick start

```go
// library
import "github.com/eilandert/gdcc/dcc"

c := &dcc.Client{}                 // zero value: anonymous, public server pool
res, err := c.Check(msg)           // per-checksum counts from the server
v := res.Verdict()                 // {Action: reject|accept|unknown, Bulk *int}
_ = c.Report(msg)                  // report the message to the servers

sums := dcc.Checksums(msg)         // offline: the computed checksums (debug)
```

`Client` mirrors the gazor/gyzor shape: `Servers`, `ClientID`, `Password`,
`Timeout`, `Verbose`, `Log`. An empty `Password` (or `ClientID` 1) is the
anonymous client.

```sh
# CLI — exit 0 = bulk (reject), 1 = not bulk, 2 = error
gdcc check < message.eml
```

## The DRP family

Three pure-Go network clients, one orchestrator binary, one Docker deployment —
each wire-compatible with the original perl/python/C tool:

| Repo | Role |
|------|------|
| [gdcc](https://github.com/eilandert/gdcc) | DCC client — library + CLI |
| [gazor](https://github.com/eilandert/gazor) | Razor 2 client — library + CLI |
| [gyzor](https://github.com/eilandert/gyzor) | Pyzor client — library + CLI |
| [gozer](https://github.com/eilandert/gozer) | backend binary — links all three in-process behind one HTTP endpoint |
| [rspamd-dcc-razor-pyzor](https://github.com/eilandert/rspamd-dcc-razor-pyzor) | Docker deployment — gozer image + rspamd plugin + dovecot sieve |

The three clients share the same `Client` shape, CLI/env conventions and `serve`
API. Background: [why we rewrote them in Go](https://github.com/eilandert/rspamd-dcc-razor-pyzor#the-go-rewrite-gazor-gyzor-gdcc-gozer).

**Why Go?** The classic DCC client is C and forks `dccproc` per message, dragging
in glibc, a set-uid helper, the `dcc` user and `/var/dcc`. gdcc is one static
binary: no fork, the message stays in RAM, and every checksum is parity-tested
against real `dccproc`. Dropping it into gozer lets that image become
`FROM scratch` / distroless-static with zero subprocesses.

## CLI

```sh
gdcc check   < message.eml    # exit 0 = bulk (reject), 1 = not bulk, 2 = error
gdcc report  < message.eml    # report the message to the DCC servers
gdcc cksum   < message.eml    # print the computed checksums (offline, debug)
gdcc register                 # save --client-id/--passwd to a DCC ids file
gdcc serve                    # HTTP sidecar: /check /report /metrics /healthz
```

Every option is settable by flag **or** environment variable (flag > env >
DCC ids file > default):

| flag | env | meaning |
|------|-----|---------|
| `--servers` | `GDCC_SERVERS` | comma list of `host[:port]` (IPv4/IPv6/hostname); replaces the DNS pool |
| `--port` | `GDCC_PORT` | default server port (6277) |
| `--timeout` | `GDCC_TIMEOUT` | per-server network budget |
| `--client-id` / `--passwd` | `GDCC_CLIENT_ID` / `GDCC_CLIENT_PASSWD` | authenticated identity (1 = anonymous) |
| `--out` | — | `register`: ids file to write (default `DCC_IDS`, else `/var/dcc/ids`) |
| `--threshold` | `GDCC_THRESHOLD` | reject at this body count (0 = DCC "many") |
| `--verbose` | `GDCC_VERBOSE` | per-operation logging (errors are logged either way) |
| `--listen` / `--unix` / `--token` | `GDCC_LISTEN` / `GDCC_UNIX` / `GDCC_TOKEN` | `serve` HTTP listen address `host:port` (default loopback `127.0.0.1:8080`), optional Unix socket, shared secret (**required to bind a non-loopback address**) |
| `--max-concurrent` | `GDCC_MAX_CONCURRENT` | `serve` max in-flight requests (default 8; over the limit → `503`) |
| `--log-stdout` | `GDCC_LOG_STDOUT` | `serve` send info/access logs to stdout; **errors/warnings stay on stderr**. `/report` access logged always, `/check` under `--verbose`. |

With no identity given, gdcc falls back to the DCC ids file (`DCC_IDS` env or
`/var/dcc/ids`) and finally to the anonymous client.

### Credentials

DCC has **no client-side registration** — the dccd operator issues your numeric
client-id and password out of band. Supply them three ways (precedence order):

1. **Flags / env** — `--client-id`+`--passwd` (`GDCC_CLIENT_ID`/`GDCC_CLIENT_PASSWD`).
2. **`gdcc register`** — persists the supplied id+password to a DCC ids file
   (`--out`, else `DCC_IDS`, else `/var/dcc/ids`; dir `0700`, file `0600`, atomic,
   idempotent per id) so later `check`/`report` authenticate automatically. Both
   `--client-id` and `--passwd` are required — it **saves** credentials, it does
   not **obtain** them. It reports the file it wrote and prints the identity as
   bare `GDCC_CLIENT_ID=`/`GDCC_CLIENT_PASSWD=` lines:

   ```sh
   gdcc --client-id 32 --passwd s3cret register
   # register: saved client-id 32 to /var/dcc/ids
   # register: environment variables for this identity (use instead of the file):
   # GDCC_CLIENT_ID=32
   # GDCC_CLIENT_PASSWD=s3cret
   gdcc --out ./ids --client-id 32 --passwd s3cret register | grep '^GDCC_' > gdcc.env
   ```
3. **The DCC ids file** itself (`id passwd` lines), drop-in with the standard
   `/var/dcc/ids`.

### serve mode

`gdcc serve` runs a plain **HTTP/1.1** server. **Safe by default:** it binds
loopback (`127.0.0.1:8080`) and bounds in-flight requests (`--max-concurrent`,
default 8 → `503` over the limit). Exposing it on another address requires a
`--token` — it refuses a non-loopback bind without one (`/report` writes public
DCC counts). Set `--listen host:port` / `GDCC_LISTEN` (and/or a Unix socket via
`--unix`):

- `POST /check` → `{"action":"reject|accept|unknown","bulk":N,"counts":[...]}` (the `action`/`bulk` shape matches the gozer DCC sub-result)
- `POST /report` → `{"reported":true}`
- `GET /metrics` → Prometheus exposition (request/verdict counters, latency histogram)
- `GET /healthz`

POST the raw RFC-822 message as the body (`--data-binary` keeps the bytes intact —
the checksums are computed over them):

```sh
gdcc serve --listen :8080 --token s3cret &

# query — JSON verdict (drop the header if no --token was set)
curl -s --data-binary @message.eml \
  -H 'X-GDCC-Token: s3cret' http://127.0.0.1:8080/check
# {"action":"accept","bulk":null,"counts":[…]}

# report to the DCC servers — Bearer works too (DCC has no revoke)
curl -s --data-binary @spam.eml -H 'Authorization: Bearer s3cret' http://127.0.0.1:8080/report
curl -s http://127.0.0.1:8080/metrics      # no auth
```

An optional `--token` requires a `Bearer` or `X-GDCC-Token` header on `/check` and
`/report`. Default bind is loopback `127.0.0.1:8080` (gozer is `8077`,
`gyzor serve` `8078`, `gazor serve` `8079`). Messages over 16 MiB are rejected
(`413`), not silently truncated. For an **authenticated** client (`--passwd`),
server answers whose DCC signature does not verify against the password are
ignored, so a spoofed UDP reply cannot inject counts.

## Correctness

The checksums are the make-or-break: a wrong checksum means the server sees a
different fingerprint and DCC silently scores nothing. The `dcc` package is a
faithful port of DCC's `ck.c`, `ckbody.c`, `ckfuz1.c` and `ckfuz2.c` (including
the URL/HTML state machines, punycode, the language word dictionaries and the 8859
charset tables), gated by a **parity test** that compares gdcc to real `dccproc`
2.3.169 over a corpus — plain, HTML, URL (with third-level-domain trimming),
all-caps, greeting-line, and long prose — byte-for-byte across all five message
checksums (From, Message-ID, Body, Fuz1, Fuz2). The body scanner is fuzzed against
panics and for determinism.

## Scope

Check and report only. DCC has no network "un-report" — a count cannot be
decremented — so there is no revoke (matching gozer, which returns `dcc: null`
for `/revoke`). The MIME container layer (multipart / base64 / quoted-printable /
charset-from-headers / Received-IP) is not yet ported; those feed the same proven
checksum cores.

## Build / test

```sh
go build ./cmd/gdcc
go test ./...                                   # includes the dccproc parity gate
go test -fuzz=FuzzChecksums ./dcc               # checksum fuzzing
```

The parity golden vectors (`dcc/testdata/expected.tsv`) are committed;
`dcc/testdata/gen_expected.sh` regenerates them from real `dccproc` (run from a
container that ships the dcc binaries).

## See also

- The rest of the family is in the table above.
- [The Go rewrite: gazor, gyzor, gdcc, gozer](https://github.com/eilandert/rspamd-dcc-razor-pyzor#the-go-rewrite-gazor-gyzor-gdcc-gozer) — why the perl/python/C clients were rewritten in Go
- Blog article: <https://deb.myguard.nl/2026/06/rspamd-dcc-razor-pyzor-docker-backend/>
- Docker Hub: <https://hub.docker.com/r/eilandert/rspamd-dcc-razor-pyzor>

## License

**MIT** — see [LICENSE](LICENSE). DCC itself is distributed under an ISC-like
licence; this is an independent clean-room reimplementation of the DCC protocol
(not derived from the DCC source), so it is not bound by DCC's copyleft terms.
"gdcc" describes a Go DCC-protocol client; it is not the DCC software.
