package main

import (
	"debug/pe"
	"errors"
	"flag"
	"fmt"
	"path/filepath"
)

func checkPE(file string) error {
	f, err := pe.Open(file)
	if err != nil {
		return fmt.Errorf("%s: not a Windows executable: %w", filepath.Base(file), err)
	}
	defer f.Close()
	if f.Machine != pe.IMAGE_FILE_MACHINE_AMD64 {
		return fmt.Errorf("%s: machine %#x, not amd64", filepath.Base(file), f.Machine)
	}
	oh, ok := f.OptionalHeader.(*pe.OptionalHeader64)
	if !ok {
		return fmt.Errorf("%s: no 64-bit optional header", filepath.Base(file))
	}
	if oh.Subsystem != pe.IMAGE_SUBSYSTEM_WINDOWS_GUI {
		return fmt.Errorf("%s: subsystem %d, not Windows GUI (%d): it would open a console window; build it with -ldflags -H=windowsgui",
			filepath.Base(file), oh.Subsystem, pe.IMAGE_SUBSYSTEM_WINDOWS_GUI)
	}
	return nil
}

func checkReleaseExes(dir string) error {
	exes, _ := filepath.Glob(filepath.Join(dir, "*.exe"))
	if len(exes) == 0 {
		return fmt.Errorf("%s has no .exe", dir)
	}
	for _, e := range exes {
		if err := checkPE(e); err != nil {
			return err
		}
	}
	return nil
}

func cmdPecheck(args []string) error {
	fl := flag.NewFlagSet("pecheck", flag.ExitOnError)
	dir := fl.String("dir", "", "a release folder: check every .exe in it (there must be one)")
	fl.Parse(args)
	if *dir != "" {
		return checkReleaseExes(*dir)
	}
	if fl.NArg() == 0 {
		return errors.New("usage: release pecheck -dir DIR | FILE.exe...")
	}
	for _, a := range fl.Args() {
		if err := checkPE(a); err != nil {
			return err
		}
	}
	return nil
}
