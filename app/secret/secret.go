package secret

import (
	"errors"
	"os"
	"path/filepath"
)

var errUnreadable = errors.New("the saved upload code can't be read on this computer; paste it again")

var ErrUnreadable = errUnreadable

const FileName = "secret.bin"

func Path(dir string) string { return filepath.Join(dir, FileName) }

func Load(dir string) (string, error) {
	b, err := os.ReadFile(Path(dir))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	plain, err := unprotect(b)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func Save(dir, value string) error {
	if value == "" {
		err := os.Remove(Path(dir))
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	b, err := protect([]byte(value))
	if err != nil {
		return err
	}
	return writeAtomic(Path(dir), b)
}

func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
