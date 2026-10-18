// Command adminsecret creates the author credentials: a bcrypt hash of your
// password (ADMIN_PASSWORD_HASH) and a random signing key (JWT_SECRET).
//
//	make admin-secrets
//
// The password is read without echo and never printed or stored.
package main

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

const minPasswordLen = 10

// One shared reader: a new bufio.Reader per prompt would swallow input
// buffered for the next prompt.
var stdin = bufio.NewReader(os.Stdin)

func main() {
	pw := prompt("Choose the author password: ")
	if len(pw) < minPasswordLen {
		fail(fmt.Sprintf("use at least %d characters", minPasswordLen))
	}
	if prompt("Type it again: ") != pw {
		fail("the two passwords don't match")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(pw), 12)
	if err != nil {
		fail(err.Error())
	}
	key := make([]byte, 48)
	rand.Read(key)
	secret := base64.RawURLEncoding.EncodeToString(key)

	fmt.Print(`
1) Add these two lines to .env (the single quotes matter: the hash contains '$'):

ADMIN_PASSWORD_HASH='` + string(hash) + `'
JWT_SECRET='` + secret + `'

2) In Vercel -> Settings -> Environment Variables, add the same two names with
   the values WITHOUT the quotes, then redeploy.

Changing JWT_SECRET later signs out every existing author session.
`)
}

// prompt reads a line with terminal echo turned off (when stdin is a terminal).
func prompt(label string) string {
	fmt.Fprint(os.Stderr, label)
	if stty("-echo") == nil {
		defer func() {
			stty("echo")
			fmt.Fprintln(os.Stderr)
		}()
	}
	line, err := stdin.ReadString('\n')
	if err != nil && line == "" {
		fail("no input")
	}
	return strings.TrimRight(line, "\r\n")
}

func stty(arg string) error {
	cmd := exec.Command("stty", arg)
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "adminsecret:", msg)
	os.Exit(1)
}
