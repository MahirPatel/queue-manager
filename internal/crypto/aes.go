// Package crypto implements the AES-256-CBC scheme third-party clients use
// to encrypt request payloads before posting them to a template's entry
// point. It mirrors the standard PHP openssl_encrypt pattern:
//
//	$encrypted = openssl_encrypt(json_encode($data), 'AES-256-CBC', $key, OPENSSL_RAW_DATA, $iv)
//	$encoded   = urlencode(base64_encode($encrypted))
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"errors"
	"net/url"
)

const (
	KeySize = 32 // AES-256
	IVSize  = 16 // AES block size
)

// Decrypt reverses the PHP encoding above: urldecode -> base64 decode ->
// AES-256-CBC decrypt -> PKCS7 unpad, returning the original JSON bytes.
func Decrypt(encoded string, key, iv []byte) ([]byte, error) {
	if len(key) != KeySize {
		return nil, errors.New("crypto: key must be 32 bytes for AES-256")
	}
	if len(iv) != IVSize {
		return nil, errors.New("crypto: iv must be 16 bytes")
	}

	urlDecoded, err := url.QueryUnescape(encoded)
	if err != nil {
		return nil, err
	}

	ciphertext, err := base64.StdEncoding.DecodeString(urlDecoded)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) == 0 || len(ciphertext)%aes.BlockSize != 0 {
		return nil, errors.New("crypto: ciphertext is not a multiple of the block size")
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	plaintext := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plaintext, ciphertext)

	return pkcs7Unpad(plaintext, aes.BlockSize)
}

// Encrypt mirrors the same PHP encoding, used for tests and for any
// outbound calls Queue itself needs to make in this scheme.
func Encrypt(plaintext []byte, key, iv []byte) (string, error) {
	if len(key) != KeySize {
		return "", errors.New("crypto: key must be 32 bytes for AES-256")
	}
	if len(iv) != IVSize {
		return "", errors.New("crypto: iv must be 16 bytes")
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	padded := pkcs7Pad(plaintext, aes.BlockSize)
	ciphertext := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ciphertext, padded)

	b64 := base64.StdEncoding.EncodeToString(ciphertext)
	return url.QueryEscape(b64), nil
}
