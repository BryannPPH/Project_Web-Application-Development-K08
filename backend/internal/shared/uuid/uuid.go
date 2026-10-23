// Package uuid menyediakan validasi dan pembuatan UUID tanpa dependency baru.
package uuid

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
)

// Valid memeriksa format kanonik UUID (8-4-4-4-12 heksadesimal).
func Valid(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		switch index {
		case 8, 13, 18, 23:
			if character != '-' {
				return false
			}
		default:
			if !isHex(character) {
				return false
			}
		}
	}
	return true
}

// New membuat UUID versi 4 dari crypto/rand.
func New() (string, error) {
	return newFrom(rand.Reader)
}

func newFrom(random io.Reader) (string, error) {
	var value [16]byte
	if _, err := io.ReadFull(random, value[:]); err != nil {
		return "", fmt.Errorf("generate uuid: %w", err)
	}
	value[6] = (value[6] & 0x0f) | 0x40 // versi 4
	value[8] = (value[8] & 0x3f) | 0x80 // varian RFC 4122

	encoded := hex.EncodeToString(value[:])
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" +
		encoded[16:20] + "-" + encoded[20:32], nil
}

func isHex(character byte) bool {
	return (character >= '0' && character <= '9') ||
		(character >= 'a' && character <= 'f') ||
		(character >= 'A' && character <= 'F')
}
