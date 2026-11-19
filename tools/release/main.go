package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: release check|keygen|zip|sums|sign|verify|notes|provenance|manifest|winres|pubkey|publisher|channel|prepublish|pecheck|authenticode|addoncompare [flags]")
		os.Exit(2)
	}
	cmds := map[string]func([]string) error{
		"check": cmdCheck, "keygen": cmdKeygen, "zip": cmdZip, "sums": cmdSums, "sign": cmdSign,
		"verify": cmdVerify, "notes": cmdNotes, "provenance": cmdProvenance, "manifest": cmdManifest,
		"winres": cmdWinres, "pubkey": cmdPubkey, "publisher": cmdPublisher, "channel": cmdChannel,
		"prepublish": cmdPrepublish, "pecheck": cmdPecheck, "authenticode": cmdAuthenticode,
		"addoncompare": cmdAddonCompare,
	}
	run, ok := cmds[os.Args[1]]
	if !ok {
		fmt.Fprintln(os.Stderr, "release: unknown command", os.Args[1])
		os.Exit(2)
	}
	if err := run(os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "release "+os.Args[1]+":", err)
		os.Exit(1)
	}
}

var semver = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$`)

func cmdCheck(args []string) error {
	fl := flag.NewFlagSet("check", flag.ExitOnError)
	version := fl.String("version", "", "the version being released (from VERSION)")
	changelog := fl.String("changelog", "CHANGELOG.md", "")
	addonDir := fl.String("addon", "addon/WoWForeverGuildChronicle", "")
	config := fl.String("config", "release.json", "the publisher settings")
	channel := fl.String("channel", "", "stable, beta or probe (default: probe for probe versions, else beta)")
	signed := fl.Bool("signed", false, "the exe will be code-signed")
	updatePub := fl.String("update-pub", "", "the update key compiled in as current")
	nextPub := fl.String("next-pub", "", "the offline next key compiled in (required from 1.0.0)")
	devPub := fl.String("dev-pub", "", "the dev update key, refused from 1.0.0")
	fl.Parse(args)
	if err := check(*version, *changelog, *addonDir); err != nil {
		return err
	}
	if err := checkAddon(*version, *addonDir); err != nil {
		return err
	}
	if err := checkKeys(*version, *updatePub, *nextPub, *devPub); err != nil {
		return err
	}
	if *channel == "" {
		*channel = DefaultChannel(*version)
	}
	if err := channelAllowed(*channel, *version); err != nil {
		return err
	}
	pub, err := readPublisher(*config)
	if err != nil {
		return err
	}
	if pub.Placeholder() && (*channel == "stable" || *signed) {
		return fmt.Errorf("%s: the company name %q is still a placeholder; signed and stable builds need the confirmed legal name", *config, pub.Company)
	}
	return nil
}

func check(version, changelog, addonDir string) error {
	if !semver.MatchString(version) {
		return fmt.Errorf("version %q is not MAJOR.MINOR.PATCH[-pre]", version)
	}
	if strings.HasSuffix(version, "-dev") {
		return errors.New("a -dev version is never released")
	}
	if _, err := changelogEntry(changelog, version); err != nil {
		return err
	}
	tocs, _ := filepath.Glob(filepath.Join(addonDir, "*.toc"))
	if len(tocs) == 0 {
		return fmt.Errorf("no .toc in %s", addonDir)
	}
	for _, toc := range tocs {
		v, err := tocVersion(toc)
		if err != nil {
			return err
		}
		if v != version {
			return fmt.Errorf("%s says ## Version: %s, VERSION says %s", toc, v, version)
		}
	}
	return nil
}

func tocVersion(file string) (string, error) {
	f, err := os.Open(file)
	if err != nil {
		return "", err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if v, ok := strings.CutPrefix(sc.Text(), "## Version:"); ok {
			return strings.TrimSpace(v), nil
		}
	}
	return "", fmt.Errorf("%s has no ## Version: line", file)
}

func changelogEntry(file, version string) (string, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	var out []string
	in := false
	for line := range strings.SplitSeq(string(b), "\n") {
		if strings.HasPrefix(line, "## [") {
			if in {
				break
			}
			in = strings.HasPrefix(line, "## ["+version+"]")
			continue
		}
		if in {
			out = append(out, line)
		}
	}
	body := strings.TrimSpace(strings.Join(out, "\n"))
	if !in {
		return "", fmt.Errorf("%s has no entry \"## [%s]\"", file, version)
	}
	if body == "" {
		return "", fmt.Errorf("%s: the entry for %s is empty", file, version)
	}
	return body, nil
}

func cmdNotes(args []string) error {
	fl := flag.NewFlagSet("notes", flag.ExitOnError)
	version := fl.String("version", "", "")
	changelog := fl.String("changelog", "CHANGELOG.md", "")
	fl.Parse(args)
	body, err := changelogEntry(*changelog, *version)
	if err != nil {
		return err
	}
	fmt.Printf("## [%s]\n\n%s\n", *version, body)
	return nil
}

func insideRepo(p string) bool {
	dir := filepath.Dir(p)
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--is-inside-work-tree").Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

func cmdKeygen(args []string) error {
	fl := flag.NewFlagSet("keygen", flag.ExitOnError)
	out := fl.String("out", "", "private key file (PEM), outside the repository")
	pubOut := fl.String("pub", "", "public key file (PEM)")
	encrypt := fl.Bool("passphrase-stdin", false, "encrypt the private key with a passphrase read from standard input")
	fl.Parse(args)
	if *out == "" || *pubOut == "" {
		return errors.New("-out and -pub are required")
	}
	if insideRepo(*out) {
		return fmt.Errorf("%s is inside a git repository: private keys never go there", *out)
	}
	if _, err := os.Stat(*pubOut); err == nil {
		return fmt.Errorf("%s exists: a public key is never replaced (it is compiled into released apps)", *pubOut)
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	privDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return err
	}
	pubDER, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return err
	}
	blk := &pem.Block{Type: "PRIVATE KEY", Bytes: privDER}
	if *encrypt {
		pass, err := readPassphrase(os.Stdin)
		if err != nil {
			return err
		}
		if blk, err = sealKey(privDER, pass); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(*out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if err := pem.Encode(f, blk); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.WriteFile(*pubOut, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER}), 0o644)
}

func readPrivate(file string, pass io.Reader) (ed25519.PrivateKey, error) {
	if insideRepo(file) {
		return nil, fmt.Errorf("%s is inside a git repository: refusing to use it", file)
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	blk, _ := pem.Decode(b)
	der := []byte(nil)
	switch {
	case blk == nil:
		return nil, fmt.Errorf("%s: not a PEM private key", file)
	case blk.Type == "PRIVATE KEY":
		der = blk.Bytes
	case blk.Type == encryptedKeyType:
		if pass == nil {
			return nil, fmt.Errorf("%s is encrypted: pass -passphrase-stdin", file)
		}
		p, err := readPassphrase(pass)
		if err != nil {
			return nil, err
		}
		if der, err = openKey(blk, p); err != nil {
			return nil, fmt.Errorf("%s: %w", file, err)
		}
	default:
		return nil, fmt.Errorf("%s: not a PEM private key", file)
	}
	k, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, err
	}
	priv, ok := k.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("%s: not an Ed25519 key", file)
	}
	return priv, nil
}

func readPublic(file string) (ed25519.PublicKey, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	blk, _ := pem.Decode(b)
	if blk == nil || blk.Type != "PUBLIC KEY" {
		return nil, fmt.Errorf("%s: not a PEM public key", file)
	}
	k, err := x509.ParsePKIXPublicKey(blk.Bytes)
	if err != nil {
		return nil, err
	}
	pub, ok := k.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("%s: not an Ed25519 key", file)
	}
	return pub, nil
}

func signFile(priv ed25519.PrivateKey, file string) error {
	b, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, b))
	return os.WriteFile(file+".sig", []byte(sig+"\n"), 0o644)
}

func verifyFile(pub ed25519.PublicKey, file string) error {
	b, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	s, err := os.ReadFile(file + ".sig")
	if err != nil {
		return err
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(s)))
	if err != nil {
		return fmt.Errorf("%s.sig: %w", file, err)
	}
	if !ed25519.Verify(pub, b, sig) {
		return fmt.Errorf("%s: signature does not verify", file)
	}
	return nil
}

func cmdPubkey(args []string) error {
	fl := flag.NewFlagSet("pubkey", flag.ExitOnError)
	pubFile := fl.String("pub", "", "public key file")
	fl.Parse(args)
	pub, err := readPublic(*pubFile)
	if err != nil {
		return err
	}
	fmt.Println(base64.StdEncoding.EncodeToString(pub))
	return nil
}

func cmdChannel(args []string) error {
	fl := flag.NewFlagSet("channel", flag.ExitOnError)
	version := fl.String("version", "", "")
	fl.Parse(args)
	fmt.Println(DefaultChannel(*version))
	return nil
}

func cmdPublisher(args []string) error {
	fl := flag.NewFlagSet("publisher", flag.ExitOnError)
	config := fl.String("config", "release.json", "")
	display := fl.Bool("display", false, "without the placeholder marker (the Apps entry's publisher)")
	root := fl.Bool("root", false, "print the signing chain's root name instead")
	signer := fl.Bool("signer", false, "print the code-signing certificate's name (what a signed build pins) instead")
	base := fl.Bool("base", false, "print the release host (baseURL) instead")
	fl.Parse(args)
	pub, err := readPublisher(*config)
	if err != nil {
		return err
	}
	if *base {
		fmt.Println(pub.BaseURL)
		return nil
	}
	if *root {
		fmt.Println(pub.SigningRoot)
		return nil
	}
	if *signer {
		fmt.Println(pub.SignerName())
		return nil
	}
	if *display {
		fmt.Println(pub.Display())
		return nil
	}
	fmt.Println(pub.Company)
	return nil
}

func cmdSign(args []string) error {
	fl := flag.NewFlagSet("sign", flag.ExitOnError)
	key := fl.String("key", "", "private key file")
	passStdin := fl.Bool("passphrase-stdin", false, "the key is encrypted: read its passphrase from standard input")
	fl.Parse(args)
	var pass io.Reader
	if *passStdin {
		pass = os.Stdin
	}
	priv, err := readPrivate(*key, pass)
	if err != nil {
		return err
	}
	for _, f := range fl.Args() {
		if err := signFile(priv, f); err != nil {
			return err
		}
	}
	return nil
}

func cmdVerify(args []string) error {
	fl := flag.NewFlagSet("verify", flag.ExitOnError)
	pubFile := fl.String("pub", "", "public key file")
	fl.Parse(args)
	pub, err := readPublic(*pubFile)
	if err != nil {
		return err
	}
	for _, f := range fl.Args() {
		if err := verifyFile(pub, f); err != nil {
			return err
		}
	}
	return nil
}

func writeZip(src, prefix string, epoch int64, out string) error {
	var files []string
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			files = append(files, p)
		} else if !d.IsDir() {
			return fmt.Errorf("%s: only regular files can be zipped", p)
		}
		return nil
	})
	if err != nil {
		return err
	}
	sort.Strings(files)
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(f)
	mod := time.Unix(epoch, 0).UTC()
	for _, p := range files {
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		hdr := &zip.FileHeader{Name: path.Join(prefix, filepath.ToSlash(rel)), Method: zip.Deflate, Modified: mod}
		hdr.SetMode(0o644)
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if _, err := w.Write(b); err != nil {
			return err
		}
	}
	if err := zw.Close(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func cmdZip(args []string) error {
	fl := flag.NewFlagSet("zip", flag.ExitOnError)
	src := fl.String("src", "", "folder to zip")
	prefix := fl.String("prefix", "", "folder name inside the zip (default: the folder's own name)")
	epoch := fl.Int64("epoch", 0, "SOURCE_DATE_EPOCH: every entry's time")
	out := fl.String("out", "", "")
	fl.Parse(args)
	if *src == "" || *out == "" || *epoch == 0 {
		return errors.New("-src, -out and -epoch are required")
	}
	if *prefix == "" {
		*prefix = filepath.Base(filepath.Clean(*src))
	}
	return writeZip(*src, *prefix, *epoch, *out)
}

func fileSHA256(file string) (string, int64, error) {
	f, err := os.Open(file)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func releaseFiles(dir string) ([]string, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range ents {
		n := e.Name()
		if e.IsDir() || strings.HasPrefix(n, "SHA256SUMS") || strings.HasSuffix(n, ".sig") ||
			n == "provenance.json" || n == "CHANGELOG.md" {
			continue
		}
		names = append(names, n)
	}
	sort.Strings(names)
	return names, nil
}

func writeSums(dir, out string) error {
	names, err := releaseFiles(dir)
	if err != nil {
		return err
	}
	if len(names) == 0 {
		return fmt.Errorf("no files in %s", dir)
	}
	var buf bytes.Buffer
	for _, n := range names {
		h, _, err := fileSHA256(filepath.Join(dir, n))
		if err != nil {
			return err
		}
		fmt.Fprintf(&buf, "%s  %s\n", h, n)
	}
	return os.WriteFile(out, buf.Bytes(), 0o644)
}

func cmdSums(args []string) error {
	fl := flag.NewFlagSet("sums", flag.ExitOnError)
	dir := fl.String("dir", "", "")
	out := fl.String("out", "", "")
	fl.Parse(args)
	return writeSums(*dir, *out)
}

type Provenance struct {
	Version         string            `json:"version"`
	Commit          string            `json:"commit"`
	Dirty           bool              `json:"dirty"`
	SourceDateEpoch int64             `json:"sourceDateEpoch"`
	GoVersion       string            `json:"goVersion"`
	Image           string            `json:"image"`
	Reproduced      bool              `json:"reproduced"`
	CodeSigned      bool              `json:"codeSigned"`
	UpdateKey       string            `json:"updateKey"`
	Unsigned        map[string]string `json:"unsignedSha256"`
	Public          *PublicSource     `json:"public,omitempty"`
	Signer          string            `json:"codeSigner,omitempty"`
}

type PublicSource struct {
	Repo   string `json:"repo"`
	Tag    string `json:"tag,omitempty"`
	Commit string `json:"commit"`
	CIRun  string `json:"ciRun,omitempty"`
}

func cmdProvenance(args []string) error {
	fl := flag.NewFlagSet("provenance", flag.ExitOnError)
	var p Provenance
	dir := fl.String("dir", "", "the release folder")
	unsignedDir := fl.String("unsigned", "", "folder holding the unsigned build")
	fl.StringVar(&p.Version, "version", "", "")
	fl.StringVar(&p.Commit, "commit", "", "")
	fl.BoolVar(&p.Dirty, "dirty", false, "")
	fl.Int64Var(&p.SourceDateEpoch, "epoch", 0, "")
	fl.StringVar(&p.GoVersion, "go", "", "")
	fl.StringVar(&p.Image, "image", "", "")
	fl.BoolVar(&p.Reproduced, "reproduced", false, "")
	fl.BoolVar(&p.CodeSigned, "code-signed", false, "")
	fl.StringVar(&p.UpdateKey, "update-key", "", "")
	fl.StringVar(&p.Signer, "signer", "", "the code-signing certificate's name (code-signed builds)")
	var pub PublicSource
	fl.StringVar(&pub.Repo, "public-repo", "", "the public repo the build came from")
	fl.StringVar(&pub.Tag, "public-tag", "", "")
	fl.StringVar(&pub.Commit, "public-commit", "", "")
	fl.StringVar(&pub.CIRun, "ci-run", "", "")
	fl.Parse(args)
	if pub.Repo != "" {
		if pub.Commit == "" {
			return errors.New("-public-repo needs -public-commit")
		}
		p.Public = &pub
	}
	if !p.CodeSigned {
		p.Signer = ""
	}
	names, err := releaseFiles(*unsignedDir)
	if err != nil {
		return err
	}
	p.Unsigned = map[string]string{}
	for _, n := range names {
		if p.Unsigned[n], _, err = fileSHA256(filepath.Join(*unsignedDir, n)); err != nil {
			return err
		}
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(*dir, "provenance.json"), append(b, '\n'), 0o644)
}

type Manifest struct {
	Schema      int                     `json:"schema"`
	Channel     string                  `json:"channel"`
	Sequence    int64                   `json:"sequence"`
	Expires     time.Time               `json:"expires"`
	Version     string                  `json:"version"`
	Released    time.Time               `json:"released"`
	MinSafe     string                  `json:"minSafe,omitempty"`
	Notes       string                  `json:"notes"`
	Files       map[string]ManifestFile `json:"files"`
	RevokedKeys []string                `json:"revokedKeys"`
}

type ManifestFile struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

func platformFile(name string) string {
	switch {
	case strings.HasSuffix(name, "-windows-amd64.exe"):
		return "windows-amd64"
	case strings.HasSuffix(name, "-macos.app.zip"):
		return "darwin-universal"
	}
	return ""
}

func isProbe(version string) bool {
	_, pre, ok := strings.Cut(version, "-")
	return ok && strings.HasPrefix(pre, "probe")
}

func DefaultChannel(version string) string {
	if isProbe(version) {
		return "probe"
	}
	return "beta"
}

func channelAllowed(channel, version string) error {
	switch channel {
	case "stable", "beta", "probe":
	default:
		return fmt.Errorf("unknown channel %q", channel)
	}
	switch {
	case isProbe(version) && channel != "probe":
		return fmt.Errorf("%s is a probe build: it only ever gets the probe channel, never %s", version, channel)
	case !isProbe(version) && channel == "probe":
		return fmt.Errorf("only probe versions (x.y.z-probe.N) go to the probe channel, not %s", version)
	}
	return nil
}

func buildManifest(dir, channel, version, baseURL, minSafe, notes string, prov Provenance, now time.Time) (Manifest, error) {
	if err := channelAllowed(channel, version); err != nil {
		return Manifest{}, err
	}
	if prov.Version != version {
		return Manifest{}, fmt.Errorf("provenance is for %s, not %s", prov.Version, version)
	}
	if channel == "stable" {
		switch {
		case !prov.CodeSigned:
			return Manifest{}, errors.New("stable needs a code-signed build; unsigned builds go to beta only")
		case prov.Dirty:
			return Manifest{}, errors.New("stable needs a build from a clean commit")
		case !prov.Reproduced:
			return Manifest{}, errors.New("stable needs a reproduced build")
		case strings.Contains(version, "-"):
			return Manifest{}, errors.New("pre-release versions go to beta only")
		}
	}
	names, err := releaseFiles(dir)
	if err != nil {
		return Manifest{}, err
	}
	m := Manifest{
		Schema: 1, Channel: channel, Sequence: now.Unix(), Expires: now.Add(45 * 24 * time.Hour).UTC().Truncate(time.Second),
		Version: version, Released: now.UTC().Truncate(time.Second), MinSafe: minSafe, Notes: notes,
		Files: map[string]ManifestFile{}, RevokedKeys: []string{},
	}
	for _, n := range names {
		key := platformFile(n)
		if key == "" {
			continue
		}
		h, size, err := fileSHA256(filepath.Join(dir, n))
		if err != nil {
			return Manifest{}, err
		}
		m.Files[key] = ManifestFile{URL: strings.TrimRight(baseURL, "/") + "/" + version + "/" + n, SHA256: h, Size: size}
	}
	if len(m.Files) == 0 {
		return Manifest{}, fmt.Errorf("no platform files in %s", dir)
	}
	return m, nil
}

func cmdManifest(args []string) error {
	fl := flag.NewFlagSet("manifest", flag.ExitOnError)
	dir := fl.String("dir", "", "the release folder")
	channel := fl.String("channel", "", "stable, beta or probe (default: probe for probe versions, else beta)")
	version := fl.String("version", "", "")
	baseURL := fl.String("base-url", "https://wowforevertracker.com/downloads/companion", "the release host (release.sh passes release.json's baseURL)")
	minSafe := fl.String("min-safe", "", "")
	changelog := fl.String("changelog", "CHANGELOG.md", "")
	out := fl.String("out", "", "manifest file (default: CHANNEL.json beside the release folder)")
	fl.Parse(args)
	b, err := os.ReadFile(filepath.Join(*dir, "provenance.json"))
	if err != nil {
		return err
	}
	var prov Provenance
	if err := json.Unmarshal(b, &prov); err != nil {
		return err
	}
	notes, err := changelogEntry(*changelog, *version)
	if err != nil {
		return err
	}
	if *channel == "" {
		*channel = DefaultChannel(*version)
	}
	m, err := buildManifest(*dir, *channel, *version, *baseURL, *minSafe, notes, prov, time.Now())
	if err != nil {
		return err
	}
	j, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if *out == "" {
		*out = filepath.Join(filepath.Dir(filepath.Clean(*dir)), *channel+".json")
	}
	return os.WriteFile(*out, append(j, '\n'), 0o644)
}
