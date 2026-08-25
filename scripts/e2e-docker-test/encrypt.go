// Command encrypt is a standalone test helper for scripts/e2e-docker-test.sh
// — it does NOT import internal/crypto (host tooling can't reach the
// module's internal packages from outside it), it just reimplements the
// same AES-256-CBC + PKCS7 + base64 + urlencode scheme directly against
// stdlib so the e2e test can produce a real ingest payload without needing
// the app itself, or any dependency beyond the Go toolchain already
// required to build this project.
//
//	go run ./scripts/e2e-docker-test/encrypt.go <key> <iv> <plaintext>
package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
)

func pkcs7Pad(data []byte, blockSize int) []byte {
	padding := blockSize - len(data)%blockSize
	return append(data, bytes.Repeat([]byte{byte(padding)}, padding)...)
}

func main() {
	if len(os.Args) != 4 {
		fmt.Fprintln(os.Stderr, "usage: encrypt <32-byte-key> <16-byte-iv> <plaintext>")
		os.Exit(1)
	}
	key, iv, plaintext := []byte(os.Args[1]), []byte(os.Args[2]), []byte(os.Args[3])

	block, err := aes.NewCipher(key)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	padded := pkcs7Pad(plaintext, aes.BlockSize)
	ciphertext := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ciphertext, padded)

	fmt.Print(url.QueryEscape(base64.StdEncoding.EncodeToString(ciphertext)))
}
