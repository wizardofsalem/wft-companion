package main

import (
	"bufio"
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const (
	encryptedKeyType = "WFT ENCRYPTED PRIVATE KEY"
	keyAAD           = "wft-update-key-v1"
	kdfIterations    = 600_000
	minPassphrase    = 12
)

func deriveKey(passphrase string, salt []byte, iter int) ([]byte, error) {
	return pbkdf2.Key(sha256.New, passphrase, salt, iter, 32)
}

func sealKey(der []byte, passphrase string) (*pem.Block, error) {
	if len(passphrase) < minPassphrase {
		return nil, fmt.Errorf("the passphrase needs at least %d characters", minPassphrase)
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	k, err := deriveKey(passphrase, salt, kdfIterations)
	if err != nil {
		return nil, err
	}
	gcm, err := newGCM(k)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return &pem.Block{
		Type: encryptedKeyType,
		Headers: map[string]string{
			"KDF": "pbkdf2-sha256", "Iterations": strconv.Itoa(kdfIterations),
			"Salt": hex.EncodeToString(salt), "Cipher": "aes-256-gcm", "Nonce": hex.EncodeToString(nonce),
		},
		Bytes: gcm.Seal(nil, nonce, der, []byte(keyAAD)),
	}, nil
}

func openKey(blk *pem.Block, passphrase string) ([]byte, error) {
	h := blk.Headers
	if h["KDF"] != "pbkdf2-sha256" || h["Cipher"] != "aes-256-gcm" {
		return nil, errors.New("unknown key encryption")
	}
	iter, err := strconv.Atoi(h["Iterations"])
	if err != nil || iter < 100_000 {
		return nil, errors.New("bad iteration count")
	}
	salt, err1 := hex.DecodeString(h["Salt"])
	nonce, err2 := hex.DecodeString(h["Nonce"])
	if err1 != nil || err2 != nil {
		return nil, errors.New("bad salt or nonce")
	}
	k, err := deriveKey(passphrase, salt, iter)
	if err != nil {
		return nil, err
	}
	gcm, err := newGCM(k)
	if err != nil {
		return nil, err
	}
	if len(nonce) != gcm.NonceSize() {
		return nil, errors.New("bad nonce")
	}
	der, err := gcm.Open(nil, nonce, blk.Bytes, []byte(keyAAD))
	if err != nil {
		return nil, errors.New("wrong passphrase (or the key file is damaged)")
	}
	return der, nil
}

func newGCM(k []byte) (cipher.AEAD, error) {
	b, err := aes.NewCipher(k)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(b)
}

func readPassphrase(r io.Reader) (string, error) {
	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	p := strings.TrimRight(line, "\r\n")
	if p == "" {
		return "", errors.New("no passphrase on standard input")
	}
	return p, nil
}
