package logx

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/wizardofsalem/wft-companion/app/diag"
)

const (
	DefaultMaxSize = 2 << 20
	DefaultKeep    = 5
)

type Rotator struct {
	Dir     string
	Name    string
	MaxSize int64
	Keep    int
	Homes   []string

	mu   sync.Mutex
	f    *os.File
	size int64
}

func (r *Rotator) path(i int) string {
	if i == 0 {
		return filepath.Join(r.Dir, r.Name+".log")
	}
	return filepath.Join(r.Dir, fmt.Sprintf("%s.%d.log", r.Name, i))
}

func (r *Rotator) Paths() []string {
	keep := r.keep()
	out := make([]string, 0, keep)
	for i := keep - 1; i >= 0; i-- {
		out = append(out, r.path(i))
	}
	return out
}

func (r *Rotator) keep() int {
	if r.Keep < 1 {
		return DefaultKeep
	}
	return r.Keep
}

func (r *Rotator) maxSize() int64 {
	if r.MaxSize <= 0 {
		return DefaultMaxSize
	}
	return r.MaxSize
}

func (r *Rotator) open() error {
	if err := os.MkdirAll(r.Dir, 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(r.path(0), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	r.f, r.size = f, info.Size()
	return nil
}

func (r *Rotator) rotate() error {
	if r.f != nil {
		r.f.Close()
		r.f = nil
	}
	keep := r.keep()
	os.Remove(r.path(keep - 1))
	for i := keep - 2; i >= 0; i-- {
		os.Rename(r.path(i), r.path(i+1))
	}
	return r.open()
}

func (r *Rotator) Write(p []byte) (int, error) {
	clean := []byte(diag.RedactPaths(diag.Redact(string(p)), r.Homes))
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		if err := r.open(); err != nil {
			return 0, err
		}
	}
	if r.size > 0 && r.size+int64(len(clean)) > r.maxSize() {
		if err := r.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := r.f.Write(clean)
	r.size += int64(n)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func (r *Rotator) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		return nil
	}
	err := r.f.Close()
	r.f = nil
	return err
}

func New(w io.Writer, level slog.Level) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level}))
}
