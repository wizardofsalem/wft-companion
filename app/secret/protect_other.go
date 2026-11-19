//go:build !windows

package secret

const plainPrefix = "wft-plain:"

func protect(b []byte) ([]byte, error) { return append([]byte(plainPrefix), b...), nil }

func unprotect(b []byte) ([]byte, error) {
	if len(b) < len(plainPrefix) || string(b[:len(plainPrefix)]) != plainPrefix {
		return nil, errUnreadable
	}
	return b[len(plainPrefix):], nil
}
