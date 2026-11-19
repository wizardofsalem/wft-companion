package main

import (
	"archive/zip"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"
)

func readZipFiles(file string) (map[string][]byte, error) {
	zr, err := zip.OpenReader(file)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	files := map[string][]byte{}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
		files[f.Name] = b
	}
	return files, nil
}

func withoutSource(toc []byte) []byte {
	var out bytes.Buffer
	for _, l := range bytes.SplitAfter(toc, []byte("\n")) {
		if bytes.HasPrefix(bytes.TrimSpace(l), []byte("## X-WFT-Source:")) {
			continue
		}
		out.Write(l)
	}
	return out.Bytes()
}

func compareAddonZips(release, curseforge map[string][]byte) error {
	var problems []string
	names := map[string]bool{}
	for n := range release {
		names[n] = true
	}
	for n := range curseforge {
		names[n] = true
	}
	sorted := make([]string, 0, len(names))
	for n := range names {
		sorted = append(sorted, n)
	}
	sort.Strings(sorted)
	for _, n := range sorted {
		r, inR := release[n]
		c, inC := curseforge[n]
		switch {
		case !inR:
			problems = append(problems, n+": only in the CurseForge zip")
		case !inC:
			problems = append(problems, n+": only in the release zip")
		case strings.HasSuffix(n, ".toc"):
			if !bytes.Equal(withoutSource(r), withoutSource(c)) {
				problems = append(problems, n+": differs (beyond ## X-WFT-Source)")
			}
		case !bytes.Equal(r, c):
			problems = append(problems, n+": differs")
		}
	}
	if len(problems) > 0 {
		return errors.New("the release and CurseForge addons differ:\n  " + strings.Join(problems, "\n  "))
	}
	return nil
}

func cmdAddonCompare(args []string) error {
	fl := flag.NewFlagSet("addoncompare", flag.ExitOnError)
	rel := fl.String("release", "", "the app release's addon zip")
	cf := fl.String("curseforge", "", "the CurseForge zip of the same version")
	fl.Parse(args)
	if *rel == "" || *cf == "" {
		return errors.New("-release and -curseforge are required")
	}
	r, err := readZipFiles(*rel)
	if err != nil {
		return fmt.Errorf("%s: %w", *rel, err)
	}
	c, err := readZipFiles(*cf)
	if err != nil {
		return fmt.Errorf("%s: %w", *cf, err)
	}
	if err := compareAddonZips(r, c); err != nil {
		return err
	}
	fmt.Printf("addoncompare: %d files, the same addon as on CurseForge\n", len(r))
	return nil
}
