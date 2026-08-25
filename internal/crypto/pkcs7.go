package crypto

import (
	"bytes"
	"errors"
)

func pkcs7Pad(data []byte, blockSize int) []byte {
	padding := blockSize - len(data)%blockSize
	padText := bytes.Repeat([]byte{byte(padding)}, padding)
	return append(data, padText...)
}

func pkcs7Unpad(data []byte, blockSize int) ([]byte, error) {
	length := len(data)
	if length == 0 || length%blockSize != 0 {
		return nil, errors.New("crypto: invalid padded data length")
	}
	padding := int(data[length-1])
	if padding == 0 || padding > blockSize || padding > length {
		return nil, errors.New("crypto: invalid padding")
	}
	for _, b := range data[length-padding:] {
		if int(b) != padding {
			return nil, errors.New("crypto: invalid padding bytes")
		}
	}
	return data[:length-padding], nil
}
