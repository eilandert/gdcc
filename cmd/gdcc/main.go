// Command gdcc is a Go DCC (Distributed Checksum Clearinghouse) client. The
// core lives in package dcc, which the gozer backend links in-process behind one
// HTTP endpoint for rspamd; this command is the standalone CLI front-end.
//
// CLI usage (message on stdin, never touches disk):
//
//	gdcc check    < message.eml   # exit 0 = bulk (reject), 1 = not bulk
//	gdcc report   < message.eml   # report the message to the DCC servers
//	gdcc cksum    < message.eml   # print computed checksums (offline, debug)
//	gdcc register                 # save --client-id/--passwd to a DCC ids file
//
// Flags: --servers (comma list of host[:port]), --port, --timeout,
// --client-id, --passwd, --out (register: ids file), --threshold (reject at
// this body count; default "many"), --verbose, --version. Env fallbacks:
// GDCC_SERVERS, GDCC_CLIENT_ID, GDCC_CLIENT_PASSWD, DCC_IDS (ids file path).
//
// DCC has no client-side registration — the dccd operator issues your numeric
// client-id and password out of band — so `register` only persists credentials
// you already have (so later check/report authenticate automatically); it does
// not obtain them. It also prints them as GDCC_CLIENT_ID=/GDCC_CLIENT_PASSWD=
// env lines.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/eilandert/gdcc/dcc"
)

var version = "dev"

const repoURL = "https://github.com/eilandert/gdcc"

// maxStdin bounds the message read from stdin (DCC messages are small).
const maxStdin = 16 << 20 // 16 MiB

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("gdcc", flag.ContinueOnError)
	fs.SetOutput(stderr)
	// Every option is settable by flag and by env (flag > env > default).
	servers := fs.String("servers", os.Getenv("GDCC_SERVERS"), "comma list of DCC servers host[:port] (default public pool)")
	port := fs.Int("port", int(envUint("GDCC_PORT")), "default server port (0 = 6277)")
	timeout := fs.Duration("timeout", envDur("GDCC_TIMEOUT", 5*time.Second), "total per-server network budget")
	clientID := fs.Uint("client-id", uint(envUint("GDCC_CLIENT_ID")), "DCC client-id (1 = anonymous)")
	passwd := fs.String("passwd", os.Getenv("GDCC_CLIENT_PASSWD"), "DCC client password (authenticated ids)")
	out := fs.String("out", "", "register: ids file to write (default DCC_IDS, else /var/dcc/ids)")
	threshold := fs.Uint("threshold", uint(envUint("GDCC_THRESHOLD")), "reject when a body checksum count reaches this (0 = DCC \"many\")")
	verbose := fs.Bool("verbose", envBool("GDCC_VERBOSE"), "log per-operation detail (errors are logged regardless)")
	listen := fs.String("listen", envStr("GDCC_LISTEN", "127.0.0.1:8080"), "serve: HTTP listen address host:port — serves /check,/report,/metrics,/healthz (default loopback 127.0.0.1:8080; '' disables TCP)")
	unix := fs.String("unix", os.Getenv("GDCC_UNIX"), "serve: also serve the HTTP API on this Unix socket path (optional)")
	token := fs.String("token", os.Getenv("GDCC_TOKEN"), "serve: shared-secret token; required to bind a non-loopback address")
	maxConc := fs.Int("max-concurrent", int(envUintOr("GDCC_MAX_CONCURRENT", uint(runtime.NumCPU()))), "serve: max in-flight requests, default = CPU count (over the limit -> 503)")
	logStdout := fs.Bool("log-stdout", envBool("GDCC_LOG_STDOUT"), "serve: send info/access logs to stdout; errors stay on stderr")
	showVer := fs.Bool("version", false, "print version and exit")

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *showVer {
		fmt.Fprintln(stdout, "gdcc", version)
		return 0
	}
	rest := fs.Args()
	if len(rest) == 0 {
		fmt.Fprintln(stderr, "usage: gdcc [flags] check|report|cksum|register|serve")
		return 2
	}
	op := rest[0]
	// Allow flags after the subcommand too (gdcc serve --listen ...), not just
	// before it — Go's flag package otherwise stops at the first positional.
	if err := fs.Parse(rest[1:]); err != nil {
		return 2
	}

	// Resolve identity: flags/env first, then DCC ids file, then anonymous.
	id := dcc.ResolveIdentity(uint32(*clientID), *passwd) // #nosec G115 -- client-id is a 32-bit DCC field; truncation is intended

	c := &dcc.Client{
		Servers:  parseServers(*servers, *port),
		ClientID: id.ClientID,
		Password: id.Password,
		Timeout:  *timeout,
		Verbose:  *verbose,
		Log:      func(s string) { fmt.Fprintln(stderr, s) },
	}

	switch op {
	case "register":
		return runRegister(*out, uint32(*clientID), *passwd, stdout, stderr) // #nosec G115 -- client-id is a 32-bit DCC field
	case "cksum":
		raw, err := readStdin(stdin, stderr)
		if err != nil {
			return 2
		}
		for _, ck := range dcc.Checksums(raw) {
			fmt.Fprintf(stdout, "%-12s %s\n", ck.Label+":", ck.Sum)
		}
		return 0
	case "check":
		raw, err := readStdin(stdin, stderr)
		if err != nil {
			return 2
		}
		return doCheck(c, raw, uint32(*threshold), stdout, stderr) // #nosec G115 -- threshold is a small bounded count
	case "report":
		raw, err := readStdin(stdin, stderr)
		if err != nil {
			return 2
		}
		if err := c.Report(raw); err != nil {
			fmt.Fprintln(stderr, "report:", err)
			return 1
		}
		fmt.Fprintln(stdout, "reported")
		return 0
	case "serve":
		return runServe(c, serveConfig{listen: *listen, unix: *unix, token: *token, maxConc: *maxConc, logStdout: *logStdout, verbose: *verbose}, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown op %q\n", op)
		return 2
	}
}

func doCheck(c *dcc.Client, raw []byte, threshold uint32, stdout, stderr io.Writer) int {
	res, err := c.Check(raw)
	if err != nil {
		fmt.Fprintln(stderr, "check:", err)
		return 2
	}
	for _, ck := range res.Counts {
		fmt.Fprintf(stdout, "%-12s cur=%d prev=%d\n", ck.Label+":", ck.Cur, ck.Prev)
	}
	var v dcc.Verdict
	if threshold == 0 {
		v = res.Verdict()
	} else {
		v = res.VerdictThreshold(threshold)
	}
	if v.Bulk != nil {
		fmt.Fprintf(stdout, "verdict: %s (bulk=%d)\n", v.Action, *v.Bulk)
	} else {
		fmt.Fprintf(stdout, "verdict: %s\n", v.Action)
	}
	if v.Action == dcc.ActionReject {
		return 0 // bulk/spam → listed
	}
	return 1 // not bulk
}

func readStdin(stdin io.Reader, stderr io.Writer) ([]byte, error) {
	raw, err := readCapped(stdin, maxStdin)
	if err != nil {
		fmt.Fprintln(stderr, "read stdin:", err)
		return nil, err
	}
	return raw, nil
}

// errTooLarge is returned by readCapped when input exceeds the cap, so callers
// reject it instead of processing a silent truncated prefix.
var errTooLarge = errors.New("message exceeds the maximum size")

// readCapped reads up to max bytes and returns errTooLarge if there is more (it
// reads max+1 so an exactly-max message is accepted, an over-cap one rejected).
func readCapped(r io.Reader, max int) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, int64(max)+1))
	if err != nil {
		return nil, err
	}
	if len(b) > max {
		return nil, errTooLarge
	}
	return b, nil
}

// parseServers turns "h1,h2:1234" into Servers; empty → nil (use the pool).
func parseServers(spec string, defPort int) []dcc.Server {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil
	}
	var out []dcc.Server
	for _, item := range strings.Split(spec, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		host, port := item, defPort
		if h, p, err := net.SplitHostPort(item); err == nil {
			// host[:port] or [ipv6]:port
			host = h
			if n, err := strconv.Atoi(p); err == nil {
				port = n
			}
		} else if strings.HasPrefix(item, "[") && strings.HasSuffix(item, "]") {
			// bracketed IPv6 without a port: [::1]
			host = item[1 : len(item)-1]
		}
		// bare IPv6 (no brackets, no port) falls through as host=item, which
		// net.JoinHostPort brackets correctly at dial time.
		out = append(out, dcc.Server{Host: host, Port: port})
	}
	return out
}

func envUint(key string) uint {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseUint(v, 10, 32); err == nil {
			return uint(n)
		}
	}
	return 0
}

// envUintOr reads a uint env var, returning def when unset/invalid.
func envUintOr(key string, def uint) uint {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseUint(v, 10, 32); err == nil {
			return uint(n)
		}
	}
	return def
}

func envDur(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func envBool(key string) bool {
	switch strings.ToLower(os.Getenv(key)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func envStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
