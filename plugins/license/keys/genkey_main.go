//go:build ignore

package main

import (
	"fmt"
	"os"

	"github.com/GoCodeAlone/workflow/pkg/license"
)

func main() {
	pub, priv, err := license.GenerateKeyPair()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	pubPEM := license.MarshalPublicKeyPEM(pub)
	privPEM := license.MarshalPrivateKeyPEM(priv)
	fmt.Printf("=== PUBLIC KEY (embedded in binary) ===\n%s\n", pubPEM)
	fmt.Printf("=== PRIVATE KEY (DO NOT COMMIT — store securely) ===\n%s\n", privPEM)
	if err := os.WriteFile("license.pub", pubPEM, 0644); err != nil {
		fmt.Fprintln(os.Stderr, "write license.pub:", err)
		os.Exit(1)
	}
	fmt.Println("Written public key to license.pub")
}
