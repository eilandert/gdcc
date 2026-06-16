package main

import (
	"fmt"
	"io"
	"os"

	"github.com/eilandert/gdcc/dcc"
)

// dccAnonID is the DCC anonymous client-id (DCC_ID_ANON); ids at or below it
// need no credential, so register rejects them.
const dccAnonID = 1

// runRegister persists a DCC client-id + password to an ids file so later
// check/report runs authenticate automatically, then prints them as
// GDCC_CLIENT_ID=/GDCC_CLIENT_PASSWD= env lines.
//
// DCC has no client-side registration: the dccd operator issues the id and
// password out of band, so both --client-id and --passwd are required — this
// only saves credentials you already have, it does not obtain them. Path
// precedence: --out, else DCC_IDS, else /var/dcc/ids.
func runRegister(out string, clientID uint32, passwd string, stdout, stderr io.Writer) int {
	if clientID <= dccAnonID {
		fmt.Fprintln(stderr, "register: --client-id/GDCC_CLIENT_ID must be your DCC id (> 1; the anonymous id 1 needs no registration)")
		return 2
	}
	if passwd == "" {
		fmt.Fprintln(stderr, "register: --passwd/GDCC_CLIENT_PASSWD is required (DCC ids are issued by the server operator)")
		return 2
	}

	path := registerPath(out)
	if err := dcc.WriteIdentityFile(path, dcc.Identity{ClientID: clientID, Password: passwd}); err != nil {
		fmt.Fprintln(stderr, "register:", err)
		return 1
	}

	fmt.Fprintf(stdout, "register: saved client-id %d to %s\n", clientID, path)
	// Bare KEY=value lines (no prefix) so `grep '^GDCC_'` extracts them — use
	// them via the env (container/systemd EnvironmentFile) instead of the file.
	fmt.Fprintln(stdout, "register: environment variables for this identity (use instead of the file):")
	fmt.Fprintf(stdout, "GDCC_CLIENT_ID=%d\n", clientID)
	fmt.Fprintf(stdout, "GDCC_CLIENT_PASSWD=%s\n", passwd)
	return 0
}

// registerPath resolves the ids file to write: --out, else DCC_IDS, else the
// conventional default.
func registerPath(out string) string {
	if out != "" {
		return out
	}
	if p := os.Getenv("DCC_IDS"); p != "" {
		return p
	}
	return dcc.DefaultIDsPath
}
