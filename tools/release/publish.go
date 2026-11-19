package main

import (
	"bufio"
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func major(version string) int {
	n, _ := strconv.Atoi(strings.SplitN(version, ".", 2)[0])
	return n
}

func checkKeys(version, current, next, dev string) error {
	if major(version) < 1 {
		return nil
	}
	read := func(f string) (ed25519.PublicKey, error) {
		if f == "" {
			return nil, nil
		}
		return readPublic(f)
	}
	cur, err := read(current)
	if err != nil {
		return err
	}
	nxt, err := read(next)
	if err != nil {
		return err
	}
	dv, err := read(dev)
	if err != nil {
		return err
	}
	switch {
	case cur == nil:
		return fmt.Errorf("%s needs the production update key (-update-pub)", version)
	case nxt == nil:
		return fmt.Errorf("%s needs the offline next key compiled in (UPDATE_NEXT_PUB)", version)
	case dv != nil && (cur.Equal(dv) || nxt.Equal(dv)):
		return fmt.Errorf("%s: the dev update key is never compiled into a 1.0+ release", version)
	case cur.Equal(nxt):
		return errors.New("the current and next update keys are the same key")
	}
	return nil
}

func tocFiles(toc string) ([]string, error) {
	f, err := os.Open(toc)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		out = append(out, strings.ReplaceAll(l, `\`, "/"))
	}
	return out, sc.Err()
}

func checkAddon(version, dir string) error {
	tocs, _ := filepath.Glob(filepath.Join(dir, "*.toc"))
	listed := map[string]bool{}
	for _, toc := range tocs {
		files, err := tocFiles(toc)
		if err != nil {
			return err
		}
		for _, f := range files {
			if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(f))); err != nil {
				return fmt.Errorf("%s loads %s, which is not in the addon", filepath.Base(toc), f)
			}
			listed[f] = true
		}
	}
	probe := isProbe(version)
	return filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		name := strings.ToLower(d.Name())
		if d.IsDir() {
			if !probe && (name == "tests" || name == "test" || name == "fixtures" || name == "testdata") {
				return fmt.Errorf("%s: test material is never in a release's addon", rel)
			}
			return nil
		}
		if !probe && strings.Contains(name, "probe") {
			return fmt.Errorf("%s: probe code is never in a release's addon (only x.y.z-probe.N builds)", rel)
		}
		if path.Ext(name) == ".lua" && !listed[rel] {
			return fmt.Errorf("%s is in the addon but no .toc loads it", rel)
		}
		return nil
	})
}

func cmdPrepublish(args []string) error {
	fl := flag.NewFlagSet("prepublish", flag.ExitOnError)
	dir := fl.String("dir", "", "the release folder (dist/release/VERSION)")
	manifest := fl.String("manifest", "", "the channel manifest to publish (its .sig beside it)")
	pubFile := fl.String("pub", "", "the key the manifest must be signed with")
	sumsPub := fl.String("sums-pub", "", "the key SHA256SUMS must be signed with (default: -pub)")
	version := fl.String("version", "", "")
	channel := fl.String("channel", "", "")
	baseURL := fl.String("base-url", "", "release.json's baseURL")
	fl.Parse(args)
	if *sumsPub == "" {
		*sumsPub = *pubFile
	}
	return prepublish(*dir, *manifest, *pubFile, *sumsPub, *version, *channel, *baseURL, time.Now())
}

func prepublish(dir, manifest, pubFile, sumsPub, version, channel, baseURL string, now time.Time) error {
	if dir == "" || manifest == "" || pubFile == "" || version == "" || channel == "" || baseURL == "" {
		return errors.New("-dir, -manifest, -pub, -version, -channel and -base-url are required")
	}
	pub, err := readPublic(pubFile)
	if err != nil {
		return err
	}
	if err := verifyFile(pub, manifest); err != nil {
		return err
	}
	sp, err := readPublic(sumsPub)
	if err != nil {
		return err
	}
	sums := filepath.Join(dir, "SHA256SUMS")
	if err := verifyFile(sp, sums); err != nil {
		return err
	}
	b, err := os.ReadFile(manifest)
	if err != nil {
		return err
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return fmt.Errorf("%s: %w", manifest, err)
	}
	pb, err := os.ReadFile(filepath.Join(dir, "provenance.json"))
	if err != nil {
		return err
	}
	var prov Provenance
	if err := json.Unmarshal(pb, &prov); err != nil {
		return err
	}
	switch {
	case m.Schema != 1:
		return fmt.Errorf("manifest schema %d", m.Schema)
	case m.Version != version || prov.Version != version:
		return fmt.Errorf("manifest is %s, provenance %s, publishing %s", m.Version, prov.Version, version)
	case m.Channel != channel:
		return fmt.Errorf("manifest is for %s, publishing to %s", m.Channel, channel)
	case prov.Dirty:
		return errors.New("a dirty (working-tree) build is never published")
	case !prov.Reproduced:
		return errors.New("an unreproduced build is never published")
	case channel == "stable" && !prov.CodeSigned:
		return errors.New("stable needs a code-signed build")
	case now.Add(7 * 24 * time.Hour).After(m.Expires):
		return fmt.Errorf("the manifest expires %s: rebuild it rather than publish one that is about to expire", m.Expires.Format(time.RFC3339))
	}
	if err := channelAllowed(channel, version); err != nil {
		return err
	}
	if err := checkSums(dir, sums); err != nil {
		return err
	}
	if err := checkReleaseExes(dir); err != nil {
		return err
	}
	prefix := strings.TrimRight(baseURL, "/") + "/" + version + "/"
	if len(m.Files) == 0 {
		return errors.New("the manifest lists no files")
	}
	for plat, f := range m.Files {
		name, ok := strings.CutPrefix(f.URL, prefix)
		if !ok || name == "" || strings.Contains(name, "/") {
			return fmt.Errorf("%s: %s is not a file under %s", plat, f.URL, prefix)
		}
		h, size, err := fileSHA256(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		if h != f.SHA256 || size != f.Size {
			return fmt.Errorf("%s: %s does not match the manifest's hash or size", plat, name)
		}
	}
	return nil
}

func checkSums(dir, sums string) error {
	b, err := os.ReadFile(sums)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for line := range bytes.SplitSeq(bytes.TrimSpace(b), []byte("\n")) {
		h, name, ok := strings.Cut(string(line), "  ")
		if !ok || strings.ContainsAny(name, `/\`) {
			return fmt.Errorf("%s: bad line %q", sums, line)
		}
		got, _, err := fileSHA256(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		if got != h {
			return fmt.Errorf("%s: hash mismatch", name)
		}
		seen[name] = true
	}
	names, err := releaseFiles(dir)
	if err != nil {
		return err
	}
	for _, n := range names {
		if !seen[n] {
			return fmt.Errorf("%s is not in SHA256SUMS", n)
		}
	}
	return nil
}
