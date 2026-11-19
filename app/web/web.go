package web

import (
	"bytes"
	"crypto/rand"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/wizardofsalem/wft-companion/app/branding"
	"github.com/wizardofsalem/wft-companion/app/service"
)

//go:embed index.html
var static embed.FS

type Backend interface {
	Status() service.Status
	Health() service.Health
	Acknowledge(types, off []string) error
	CheckUpdates() error
	SetGoldShared(on bool) error
	InstallUpdate() error
	Configure(service.Settings) error
	SyncNow()
	Detect() []string
	SaveLogs() (string, error)
	EnsureAddon(force bool) (string, error)
	Uninstall(removeAddon bool) error
	StartSignIn(region string) error
	CancelSignIn()
	SignOut() error
}

type Server struct {
	Backend Backend
	Quit    func()

	token string
	host  string
	srv   *http.Server
}

func (s *Server) Start() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	return s.Serve(ln)
}

func (s *Server) Serve(ln net.Listener) (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	s.token = hex.EncodeToString(b)
	s.host = ln.Addr().String()
	s.srv = &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second,
		ErrorLog: slog.NewLogLogger(slog.Default().Handler(), slog.LevelError)}
	slog.Info("web: status page listening", "addr", s.host)
	go s.srv.Serve(ln)
	return s.URL(), nil
}

func (s *Server) URL() string {
	return "http://" + s.host + "/" + s.token + "/"
}

func (s *Server) Close() error {
	if s.srv == nil {
		return nil
	}
	return s.srv.Close()
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	prefix := "/" + s.token + "/"
	mux.HandleFunc("GET "+prefix+"{$}", s.index)
	mux.HandleFunc("GET "+prefix+"api/ping", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"app": "WFT Companion"})
	})
	mux.HandleFunc("GET "+prefix+"api/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, s.Backend.Status())
	})
	mux.HandleFunc("GET "+prefix+"api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, s.Backend.Health())
	})
	mux.HandleFunc("POST "+prefix+"api/consent/ack", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Types []string `json:"types"`
			Off   []string `json:"off"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&in); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Something went wrong. Try again."})
			return
		}
		if err := s.Backend.Acknowledge(in.Types, in.Off); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST "+prefix+"api/privacy", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			GoldShared bool `json:"goldShared"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&in); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Something went wrong. Try again."})
			return
		}
		if err := s.Backend.SetGoldShared(in.GoldShared); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST "+prefix+"api/update/check", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Backend.CheckUpdates(); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST "+prefix+"api/update/install", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Backend.InstallUpdate(); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST "+prefix+"api/settings", s.settings)
	mux.HandleFunc("POST "+prefix+"api/sync", func(w http.ResponseWriter, r *http.Request) {
		s.Backend.SyncNow()
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST "+prefix+"api/detect", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string][]string{"detected": s.Backend.Detect()})
	})
	mux.HandleFunc("POST "+prefix+"api/savelogs", func(w http.ResponseWriter, r *http.Request) {
		p, err := s.Backend.SaveLogs()
		if err != nil {
			slog.Warn("save logs failed", "err", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Couldn't save the logs."})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"path": p, "name": filepath.Base(p), "folder": filepath.Dir(p)})
	})
	mux.HandleFunc("POST "+prefix+"api/addon", func(w http.ResponseWriter, r *http.Request) {
		msg, err := s.Backend.EnsureAddon(true)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"message": msg})
	})
	mux.HandleFunc("POST "+prefix+"api/uninstall", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			RemoveAddon bool `json:"removeAddon"`
		}
		json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&in)
		if err := s.Backend.Uninstall(in.RemoveAddon); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		s.quit()
	})
	mux.HandleFunc("POST "+prefix+"api/signin", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Region string `json:"region"`
		}
		_ = json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&in)
		err := s.Backend.StartSignIn(in.Region)
		switch {
		case errors.Is(err, service.ErrRegionNeeded):
			writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error(), "regionNeeded": true})
		case err != nil:
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		default:
			writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		}
	})
	mux.HandleFunc("POST "+prefix+"api/signin/cancel", func(w http.ResponseWriter, r *http.Request) {
		s.Backend.CancelSignIn()
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST "+prefix+"api/signout", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Backend.SignOut(); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST "+prefix+"api/quit", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		s.quit()
	})
	return s.guard(mux)
}

func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slog.Debug("web: request", "method", r.Method, "path", strings.Replace(r.URL.Path, s.token, "[token]", 1))
		if r.Host != s.host {
			slog.Warn("web: refused a foreign Host", "host", r.Host)
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+s.host {
				slog.Warn("web: refused a foreign Origin", "origin", origin)
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
				http.Error(w, "expected JSON", http.StatusUnsupportedMediaType)
				return
			}
		}
		h := w.Header()
		h.Set("Cache-Control", "no-store")
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'; img-src 'self' data:; frame-ancestors 'none'; form-action 'none'")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) quit() {
	slog.Info("web: stop requested")
	if s.Quit != nil {
		go s.Quit()
	}
}

var faviconLink = `<link rel="icon" type="image/png" href="data:image/png;base64,` +
	base64.StdEncoding.EncodeToString(branding.PNG(32)) + `">`

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	b, err := static.ReadFile("index.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	b = bytes.Replace(b, []byte(`<link rel="icon" href="data:,">`), []byte(faviconLink), 1)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(b)
}

func (s *Server) settings(w http.ResponseWriter, r *http.Request) {
	var in service.Settings
	dec := json.NewDecoder(io.LimitReader(r.Body, 64<<10))
	if err := dec.Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Something went wrong. Try again."})
		return
	}
	if err := s.Backend.Configure(in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, s.Backend.Status())
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func RequestQuit(url string) bool {
	c := &http.Client{Timeout: 2 * time.Second}
	resp, err := c.Post(strings.TrimRight(url, "/")+"/api/quit", "application/json", strings.NewReader("{}"))
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func Ping(url string) bool {
	c := &http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get(strings.TrimRight(url, "/") + "/api/ping")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var body map[string]string
	return resp.StatusCode == http.StatusOK && json.NewDecoder(resp.Body).Decode(&body) == nil && body["app"] == "WFT Companion"
}
