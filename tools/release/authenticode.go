package main

import (
	"bytes"
	"crypto/x509"
	"encoding/asn1"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"math/big"
	"os"
)

type peSecurity struct {
	checksumAt int
	dirAt      int
	offset     uint32
	size       uint32
}

func readPESecurity(b []byte) (peSecurity, error) {
	var s peSecurity
	if len(b) < 0x40 || b[0] != 'M' || b[1] != 'Z' {
		return s, errors.New("not a PE file (no MZ header)")
	}
	pe := int(binary.LittleEndian.Uint32(b[0x3c:]))
	opt := pe + 4 + 20
	if pe < 0x40 || opt+2 > len(b) || !bytes.Equal(b[pe:pe+4], []byte("PE\x00\x00")) {
		return s, errors.New("not a PE file (no PE signature)")
	}
	dirs := 0
	switch binary.LittleEndian.Uint16(b[opt:]) {
	case 0x20b:
		dirs = opt + 112
	case 0x10b:
		dirs = opt + 96
	default:
		return s, errors.New("unknown optional header magic")
	}
	numDirs := int(binary.LittleEndian.Uint32(b[dirs-4:]))
	if numDirs < 5 || dirs+5*8 > len(b) {
		return s, errors.New("no security directory in the optional header")
	}
	s.checksumAt = opt + 64
	s.dirAt = dirs + 4*8
	s.offset = binary.LittleEndian.Uint32(b[s.dirAt:])
	s.size = binary.LittleEndian.Uint32(b[s.dirAt+4:])
	return s, nil
}

func unsignedMatches(signed, unsigned []byte) error {
	u, err := readPESecurity(unsigned)
	if err != nil {
		return fmt.Errorf("the unsigned build: %w", err)
	}
	if u.offset != 0 || u.size != 0 {
		return errors.New("the unsigned build already has a certificate table")
	}
	s, err := readPESecurity(signed)
	if err != nil {
		return fmt.Errorf("the signed exe: %w", err)
	}
	if s.size == 0 {
		return errors.New("the signed exe has no signature")
	}
	end := uint64(s.offset) + uint64(s.size)
	if end != uint64(len(signed)) {
		return fmt.Errorf("the signed exe's certificate table (%d+%d) doesn't end the file (%d bytes)", s.offset, s.size, len(signed))
	}
	n := len(unsigned)
	if int(s.offset) < n || int(s.offset)-n >= 8 {
		return fmt.Errorf("the signed exe's certificate table starts at %d; the unsigned build is %d bytes", s.offset, n)
	}
	for _, c := range signed[n:s.offset] {
		if c != 0 {
			return errors.New("the signed exe has data between the program and its signature")
		}
	}
	if s.checksumAt != u.checksumAt || s.dirAt != u.dirAt {
		return errors.New("the signed exe's headers are laid out differently from the unsigned build")
	}
	stripped := bytes.Clone(signed[:n])
	copy(stripped[s.checksumAt:s.checksumAt+4], unsigned[u.checksumAt:u.checksumAt+4])
	copy(stripped[s.dirAt:s.dirAt+8], unsigned[u.dirAt:u.dirAt+8])
	if !bytes.Equal(stripped, unsigned) {
		for i := range stripped {
			if stripped[i] != unsigned[i] {
				return fmt.Errorf("without its signature the signed exe differs from the unsigned build at byte %d", i)
			}
		}
	}
	return nil
}

type signatureInfo struct {
	Signer    string
	Root      string
	Timestamp bool
}

var (
	oidRFC3161Timestamp = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 311, 3, 3, 1}
	oidCounterSignature = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 6}
)

type pkcs7ContentInfo struct {
	ContentType asn1.ObjectIdentifier
	Content     asn1.RawValue `asn1:"explicit,tag:0"`
}

type pkcs7SignedData struct {
	Version          int
	DigestAlgorithms asn1.RawValue
	ContentInfo      asn1.RawValue
	Certificates     asn1.RawValue `asn1:"optional,tag:0"`
	CRLs             asn1.RawValue `asn1:"optional,tag:1"`
	SignerInfos      asn1.RawValue
}

type issuerAndSerial struct {
	Issuer asn1.RawValue
	Serial *big.Int
}

func readSignature(signed []byte) (signatureInfo, error) {
	var info signatureInfo
	s, err := readPESecurity(signed)
	if err != nil {
		return info, err
	}
	if s.size < 8 || uint64(s.offset)+uint64(s.size) > uint64(len(signed)) {
		return info, errors.New("no certificate table")
	}
	tbl := signed[s.offset : s.offset+s.size]
	length := binary.LittleEndian.Uint32(tbl)
	if binary.LittleEndian.Uint16(tbl[4:]) != 0x0200 || binary.LittleEndian.Uint16(tbl[6:]) != 0x0002 || length < 8 || length > s.size {
		return info, errors.New("the certificate table doesn't hold a PKCS#7 signature")
	}
	var ci pkcs7ContentInfo
	if _, err := asn1.Unmarshal(tbl[8:length], &ci); err != nil {
		return info, fmt.Errorf("the signature: %w", err)
	}
	var sd pkcs7SignedData
	if _, err := asn1.Unmarshal(ci.Content.Bytes, &sd); err != nil {
		return info, fmt.Errorf("the signature's SignedData: %w", err)
	}
	certs, err := x509.ParseCertificates(sd.Certificates.Bytes)
	if err != nil || len(certs) == 0 {
		return info, fmt.Errorf("the signature's certificates: %v", err)
	}
	var si asn1.RawValue
	if _, err := asn1.Unmarshal(sd.SignerInfos.Bytes, &si); err != nil {
		return info, fmt.Errorf("the signature's SignerInfo: %w", err)
	}
	var version int
	rest, err := asn1.Unmarshal(si.Bytes, &version)
	if err != nil {
		return info, fmt.Errorf("the SignerInfo: %w", err)
	}
	var sid issuerAndSerial
	if _, err := asn1.Unmarshal(rest, &sid); err != nil {
		return info, fmt.Errorf("the SignerInfo's certificate id: %w", err)
	}
	var leaf *x509.Certificate
	for _, c := range certs {
		if bytes.Equal(c.RawIssuer, sid.Issuer.FullBytes) && c.SerialNumber.Cmp(sid.Serial) == 0 {
			leaf = c
			break
		}
	}
	if leaf == nil {
		return info, errors.New("the signer's certificate isn't in the signature")
	}
	info.Signer = leaf.Subject.CommonName

	cur := leaf
	for range len(certs) + 1 {
		if bytes.Equal(cur.RawIssuer, cur.RawSubject) {
			break
		}
		var up *x509.Certificate
		for _, c := range certs {
			if c != cur && bytes.Equal(c.RawSubject, cur.RawIssuer) && cur.CheckSignatureFrom(c) == nil {
				up = c
				break
			}
		}
		if up == nil {
			break
		}
		cur = up
	}
	info.Root = cur.Issuer.CommonName

	for _, oid := range []asn1.ObjectIdentifier{oidRFC3161Timestamp, oidCounterSignature} {
		enc, _ := asn1.Marshal(oid)
		if bytes.Contains(si.FullBytes, enc) {
			info.Timestamp = true
		}
	}
	return info, nil
}

func checkAuthenticode(signed, unsigned []byte, signer, root string) (signatureInfo, error) {
	if err := unsignedMatches(signed, unsigned); err != nil {
		return signatureInfo{}, err
	}
	info, err := readSignature(signed)
	if err != nil {
		return info, err
	}
	if info.Signer != signer {
		return info, fmt.Errorf("signed by %q, not %q", info.Signer, signer)
	}
	if root != "" && info.Root != root {
		return info, fmt.Errorf("the signature chains to %q, not %q", info.Root, root)
	}
	if !info.Timestamp {
		return info, errors.New("the signature has no timestamp: it would stop verifying when the certificate expires")
	}
	return info, nil
}

func cmdAuthenticode(args []string) error {
	fl := flag.NewFlagSet("authenticode", flag.ExitOnError)
	signedFile := fl.String("signed", "", "the code-signed exe")
	unsignedFile := fl.String("unsigned", "", "our own unsigned build of the same version")
	signer := fl.String("signer", "", "the certificate name the signature must have (release.json signer)")
	root := fl.String("root", "", "the chain's root name (release.json signingRoot); empty: not checked")
	fl.Parse(args)
	if *signedFile == "" || *unsignedFile == "" || *signer == "" {
		return errors.New("-signed, -unsigned and -signer are required")
	}
	signed, err := os.ReadFile(*signedFile)
	if err != nil {
		return err
	}
	unsigned, err := os.ReadFile(*unsignedFile)
	if err != nil {
		return err
	}
	info, err := checkAuthenticode(signed, unsigned, *signer, *root)
	if err != nil {
		return fmt.Errorf("%s: %w", *signedFile, err)
	}
	fmt.Printf("authenticode: same program as the unsigned build; signer %q, root %q, timestamped\n", info.Signer, info.Root)
	return nil
}
