package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/wizardofsalem/wft-companion/app/branding"
)

type Publisher struct {
	Company     string `json:"company"`
	Product     string `json:"product"`
	Description string `json:"description"`
	Exe         string `json:"exe"`
	Signer      string `json:"signer,omitempty"`
	SigningRoot string `json:"signingRoot,omitempty"`
	BaseURL     string `json:"baseURL"`
}

const placeholderMark = "(to be confirmed)"

func readPublisher(file string) (Publisher, error) {
	var p Publisher
	b, err := os.ReadFile(file)
	if err != nil {
		return p, err
	}
	if err := json.Unmarshal(b, &p); err != nil {
		return p, fmt.Errorf("%s: %w", file, err)
	}
	if p.Company == "" || p.Product == "" || p.Description == "" || p.Exe == "" || p.BaseURL == "" {
		return p, fmt.Errorf("%s: company, product, description, exe and baseURL are all required", file)
	}
	if !strings.HasPrefix(p.BaseURL, "https://") || strings.HasSuffix(p.BaseURL, "/") {
		return p, fmt.Errorf("%s: baseURL must be https:// with no trailing slash", file)
	}
	if strings.ContainsAny(p.Company+p.Signer+p.SigningRoot, `'"`) {
		return p, fmt.Errorf("%s: company, signer and signingRoot can't contain quotes", file)
	}
	return p, nil
}

func (p Publisher) Placeholder() bool { return strings.Contains(p.Company, placeholderMark) }

func (p Publisher) SignerName() string {
	if p.Signer != "" {
		return p.Signer
	}
	return p.Company
}

func (p Publisher) Display() string {
	return strings.TrimSpace(strings.ReplaceAll(p.Company, placeholderMark, ""))
}

func cmdWinres(args []string) error {
	fl := flag.NewFlagSet("winres", flag.ExitOnError)
	version := fl.String("version", "", "")
	config := fl.String("config", "release.json", "")
	out := fl.String("out", "app/rsrc_windows_amd64.syso", "")
	fl.Parse(args)
	pub, err := readPublisher(*config)
	if err != nil {
		return err
	}
	syso, err := buildSyso(pub, *version)
	if err != nil {
		return err
	}
	return os.WriteFile(*out, syso, 0o644)
}

func fileVersion(v string) (nums [4]uint16, pre bool, err error) {
	if !semver.MatchString(v) {
		return nums, false, fmt.Errorf("version %q is not MAJOR.MINOR.PATCH[-pre]", v)
	}
	core, _, pre := strings.Cut(v, "-")
	for i, part := range strings.Split(core, ".") {
		n, err := strconv.ParseUint(part, 10, 16)
		if err != nil {
			return nums, false, fmt.Errorf("version %q: %s is over 65535", v, part)
		}
		nums[i] = uint16(n)
	}
	return nums, pre, nil
}

const manifestXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0">
  <assemblyIdentity type="win32" name="WFTCompanion" version="%d.%d.%d.%d"/>
  <trustInfo xmlns="urn:schemas-microsoft-com:asm.v3">
    <security>
      <requestedPrivileges>
        <requestedExecutionLevel level="asInvoker" uiAccess="false"/>
      </requestedPrivileges>
    </security>
  </trustInfo>
  <compatibility xmlns="urn:schemas-microsoft-com:compatibility.v1">
    <application>
      <!-- Windows 10 and 11 -->
      <supportedOS Id="{8e0f7a12-bfb3-4fe8-b9a5-48fd50a15a9a}"/>
    </application>
  </compatibility>
</assembly>
`

func utf16z(s string) []byte {
	u := append(utf16.Encode([]rune(s)), 0)
	b := make([]byte, 2*len(u))
	for i, c := range u {
		binary.LittleEndian.PutUint16(b[2*i:], c)
	}
	return b
}

func pad4(b *bytes.Buffer) {
	for b.Len()%4 != 0 {
		b.WriteByte(0)
	}
}

func verNode(key string, text bool, value []byte, valueLen int, children ...[]byte) []byte {
	var b bytes.Buffer
	b.Write([]byte{0, 0})
	binary.Write(&b, binary.LittleEndian, uint16(valueLen))
	typ := uint16(0)
	if text {
		typ = 1
	}
	binary.Write(&b, binary.LittleEndian, typ)
	b.Write(utf16z(key))
	pad4(&b)
	b.Write(value)
	for _, c := range children {
		pad4(&b)
		b.Write(c)
	}
	out := b.Bytes()
	binary.LittleEndian.PutUint16(out, uint16(len(out)))
	return out
}

func versionInfo(pub Publisher, version string) ([]byte, error) {
	nums, pre, err := fileVersion(version)
	if err != nil {
		return nil, err
	}
	ms := uint32(nums[0])<<16 | uint32(nums[1])
	ls := uint32(nums[2])<<16 | uint32(nums[3])
	flags := uint32(0)
	if pre {
		flags = 0x2
	}
	var fixed bytes.Buffer
	for _, v := range []uint32{
		0xFEEF04BD, 0x00010000,
		ms, ls, ms, ls,
		0x3F, flags,
		0x00040004,
		0x1, 0,
		0, 0,
	} {
		binary.Write(&fixed, binary.LittleEndian, v)
	}
	strs := [][2]string{
		{"CompanyName", pub.Company},
		{"FileDescription", pub.Description},
		{"FileVersion", version},
		{"InternalName", strings.TrimSuffix(pub.Exe, ".exe")},
		{"LegalCopyright", "© " + pub.Company},
		{"OriginalFilename", pub.Exe},
		{"ProductName", pub.Product},
		{"ProductVersion", version},
	}
	var entries [][]byte
	for _, kv := range strs {
		v := utf16z(kv[1])
		entries = append(entries, verNode(kv[0], true, v, len(v)/2))
	}
	table := verNode("040904b0", true, nil, 0, entries...)
	stringInfo := verNode("StringFileInfo", true, nil, 0, table)
	translation := verNode("Translation", false, []byte{0x09, 0x04, 0xb0, 0x04}, 4)
	varInfo := verNode("VarFileInfo", true, nil, 0, translation)
	return verNode("VS_VERSION_INFO", false, fixed.Bytes(), fixed.Len(), stringInfo, varInfo), nil
}

type resource struct {
	typ  uint32
	id   uint32
	data []byte
}

const (
	rtIcon      = 3
	rtGroupIcon = 14
	rtVersion   = 16
	rtManifest  = 24
	langEnUS    = 0x0409
)

func icons() []resource {
	imgs := branding.Images()
	var group bytes.Buffer
	le := func(v any) { binary.Write(&group, binary.LittleEndian, v) }
	le([3]uint16{0, 1, uint16(len(imgs))})
	res := make([]resource, 0, len(imgs)+1)
	for i, im := range imgs {
		dim := byte(im.Size)
		if im.Size >= 256 {
			dim = 0
		}
		group.Write([]byte{dim, dim, 0, 0})
		le([2]uint16{1, 32})
		le(uint32(len(im.PNG)))
		le(uint16(i + 1))
		res = append(res, resource{rtIcon, uint32(i + 1), im.PNG})
	}
	return append(res, resource{rtGroupIcon, 1, group.Bytes()})
}

func buildSyso(pub Publisher, version string) ([]byte, error) {
	ver, err := versionInfo(pub, version)
	if err != nil {
		return nil, err
	}
	nums, _, _ := fileVersion(version)
	manifest := fmt.Sprintf(manifestXML, nums[0], nums[1], nums[2], nums[3])
	res := append(icons(), resource{rtVersion, 1, ver}, resource{rtManifest, 1, []byte(manifest)})

	const dirSize, entrySize, dataEntrySize = 16, 8, 16
	n := len(res)
	var types []uint32
	perType := map[uint32]int{}
	for _, r := range res {
		if perType[r.typ] == 0 {
			types = append(types, r.typ)
		}
		perType[r.typ]++
	}
	rootLen := dirSize + len(types)*entrySize
	idDir := map[uint32]int{}
	at := rootLen
	for _, t := range types {
		idDir[t] = at
		at += dirSize + perType[t]*entrySize
	}
	langDirs := at
	dataEntries := langDirs + n*(dirSize+entrySize)
	dataStart := dataEntries + n*dataEntrySize

	var sec bytes.Buffer
	le := func(v any) { binary.Write(&sec, binary.LittleEndian, v) }
	dir := func(ids int) { le([2]uint32{}); le([2]uint16{}); le(uint16(0)); le(uint16(ids)) }
	const subdir = 0x80000000

	dir(len(types))
	for _, t := range types {
		le(t)
		le(uint32(idDir[t]) | subdir)
	}
	i := 0
	for _, t := range types {
		dir(perType[t])
		for ; i < n && res[i].typ == t; i++ {
			le(res[i].id)
			le(uint32(langDirs+i*(dirSize+entrySize)) | subdir)
		}
	}
	for i := range res {
		dir(1)
		le(uint32(langEnUS))
		le(uint32(dataEntries + i*dataEntrySize))
	}
	offsets := make([]int, n)
	off := dataStart
	for i, r := range res {
		off = (off + 7) &^ 7
		offsets[i] = off
		off += len(r.data)
	}
	var relocs []uint32
	for i, r := range res {
		relocs = append(relocs, uint32(sec.Len()))
		le(uint32(offsets[i]))
		le(uint32(len(r.data)))
		le(uint32(0))
		le(uint32(0))
	}
	for i, r := range res {
		for sec.Len() < offsets[i] {
			sec.WriteByte(0)
		}
		sec.Write(r.data)
	}
	for sec.Len()%8 != 0 {
		sec.WriteByte(0)
	}
	if sec.Len() > 1<<30 {
		return nil, errors.New("resources too large")
	}

	const fileHdr, secHdr, relocSize = 20, 40, 10
	rawPtr := fileHdr + secHdr
	relocPtr := rawPtr + sec.Len()
	symPtr := relocPtr + len(relocs)*relocSize
	var obj bytes.Buffer
	w := func(v any) { binary.Write(&obj, binary.LittleEndian, v) }
	w(uint16(0x8664))
	w(uint16(1))
	w(uint32(0))
	w(uint32(symPtr))
	w(uint32(1))
	w(uint16(0))
	w(uint16(0))
	obj.WriteString(".rsrc\x00\x00\x00")
	w(uint32(0))
	w(uint32(0))
	w(uint32(sec.Len()))
	w(uint32(rawPtr))
	w(uint32(relocPtr))
	w(uint32(0))
	w(uint16(len(relocs)))
	w(uint16(0))
	w(uint32(0x40000040))
	obj.Write(sec.Bytes())
	for _, r := range relocs {
		w(r)
		w(uint32(0))
		w(uint16(3))
	}
	obj.WriteString(".rsrc\x00\x00\x00")
	w(uint32(0))
	w(uint16(1))
	w(uint16(0))
	w(uint8(3))
	w(uint8(0))
	w(uint32(4))
	return obj.Bytes(), nil
}
