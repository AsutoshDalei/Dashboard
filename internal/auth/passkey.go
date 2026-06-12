package auth

import (
	"crypto/subtle"
)

func ConstantTimePasskeyEqual(a, b string) bool {
	n := len(a)
	if len(b) > n {
		n = len(b)
	}
	if n == 0 {
		return len(a) == 0 && len(b) == 0
	}
	bufA := make([]byte, n)
	bufB := make([]byte, n)
	copy(bufA, a)
	copy(bufB, b)
	return subtle.ConstantTimeCompare(bufA, bufB) == 1
}