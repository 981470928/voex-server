// voex-release signs and verifies offline release manifests without online services.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"voex-server/internal/updater"
)

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func readKey(path string) []byte {
	raw, err := os.ReadFile(path)
	must(err)
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	must(err)
	return key
}
func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: voex-release keygen PRIVATE PUBLIC | sign PRIVATE MANIFEST SIGNATURE | verify PUBLIC MANIFEST SIGNATURE")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "frontend-install":
		if len(os.Args) != 5 {
			os.Exit(2)
		}
		must(updater.PrepareFrontend(os.Args[2], os.Args[3], os.Args[4]))
	case "frontend-switch":
		if len(os.Args) != 4 {
			os.Exit(2)
		}
		must(updater.SwitchFrontend(os.Args[2], os.Args[3]))
	case "frontend-current":
		if len(os.Args) != 3 {
			os.Exit(2)
		}
		version, err := updater.CurrentFrontend(os.Args[2])
		must(err)
		fmt.Println(version)
	case "keygen":
		if len(os.Args) != 4 {
			os.Exit(2)
		}
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		must(err)
		f, err := os.OpenFile(os.Args[2], os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		must(err)
		_, err = f.WriteString(base64.StdEncoding.EncodeToString(priv) + "\n")
		must(err)
		must(f.Close())
		must(os.WriteFile(os.Args[3], []byte(base64.StdEncoding.EncodeToString(pub)+"\n"), 0644))
	case "sign", "verify":
		if len(os.Args) != 5 {
			os.Exit(2)
		}
		key := readKey(os.Args[2])
		data, err := os.ReadFile(os.Args[3])
		must(err)
		if os.Args[1] == "sign" {
			if len(key) != ed25519.PrivateKeySize {
				must(fmt.Errorf("invalid private key"))
			}
			must(os.WriteFile(os.Args[4], []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(key, data))+"\n"), 0644))
		} else {
			signature := readKey(os.Args[4])
			if len(key) != ed25519.PublicKeySize || !ed25519.Verify(key, data, signature) {
				must(fmt.Errorf("invalid release signature"))
			}
			fmt.Println("Release signature verified")
		}
	default:
		os.Exit(2)
	}
}
