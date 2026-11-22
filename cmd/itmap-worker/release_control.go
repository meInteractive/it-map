package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"itmap/internal/paidrecovery"
	"itmap/internal/workerengine"
)

const workerControlSocket = "/run/itmap-worker/release.sock"
const workerReleaseHold = "/opt/itmap/worker/release-hold"

var releaseToken = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,96}$`)

type releaseControl struct {
	gate     *workerengine.PauseGate
	instance string
	readHold func() (string, error)
	ledger   func(context.Context) (paidrecovery.ReleaseLedger, error)
}

type releaseRequest struct {
	Instance string `json:"instance"`
	Token    string `json:"token"`
}

func readWorkerReleaseHold() (string, error) {
	fd, err := syscall.Open(workerReleaseHold, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	file := os.NewFile(uintptr(fd), workerReleaseHold)
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0o022 != 0 {
		return "", errConfig
	}
	raw, err := io.ReadAll(io.LimitReader(file, 98))
	token := strings.TrimSuffix(string(raw), "\n")
	if err != nil || !releaseToken.MatchString(token) {
		return "", errConfig
	}
	return token, nil
}

func openReleaseControl(ledger func(context.Context) (paidrecovery.ReleaseLedger, error)) (*http.Server, *net.UnixListener, *workerengine.PauseGate, error) {
	for path := filepath.Dir(workerReleaseHold); path != "/"; path = filepath.Dir(path) {
		info, err := os.Lstat(path)
		if err != nil {
			return nil, nil, nil, err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != 0 || !info.IsDir() || info.Mode().Perm()&0o022 != 0 {
			return nil, nil, nil, errConfig
		}
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, nil, nil, err
	}
	gate := workerengine.NewPauseGate()
	token, err := readWorkerReleaseHold()
	if err != nil {
		return nil, nil, nil, err
	}
	if token != "" {
		if _, err = gate.Pause(token); err != nil {
			return nil, nil, nil, err
		}
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: workerControlSocket, Net: "unix"})
	if err != nil {
		return nil, nil, nil, err
	}
	if err = os.Chmod(workerControlSocket, 0o600); err != nil {
		_ = listener.Close()
		return nil, nil, nil, err
	}
	control := releaseControl{gate: gate, instance: hex.EncodeToString(nonce[:]), readHold: readWorkerReleaseHold, ledger: ledger}
	server := &http.Server{Handler: control, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 3 * time.Second, WriteTimeout: 3 * time.Second, MaxHeaderBytes: 4096}
	return server, listener, gate, nil
}

func (c releaseControl) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodGet && r.URL.Path == "/instance" {
		_ = json.NewEncoder(w).Encode(map[string]any{"instance": c.instance, "pid": os.Getpid(), "paused": c.gate.Paused()})
		return
	}
	if r.Method == http.MethodGet && r.URL.Path == "/ledger" && c.ledger != nil {
		evidence, err := c.ledger(r.Context())
		if err != nil {
			w.WriteHeader(http.StatusConflict)
			return
		}
		_ = json.NewEncoder(w).Encode(evidence)
		return
	}
	if r.Method != http.MethodPost || (r.URL.Path != "/pause" && r.URL.Path != "/resume") {
		http.NotFound(w, r)
		return
	}
	var request releaseRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 512))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || decoder.Decode(new(any)) != io.EOF ||
		request.Instance != c.instance || !releaseToken.MatchString(request.Token) {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	hold, err := c.readHold()
	if err != nil || (r.URL.Path == "/pause" && hold != request.Token) || (r.URL.Path == "/resume" && hold != "") {
		w.WriteHeader(http.StatusConflict)
		return
	}
	if r.URL.Path == "/resume" {
		if err := c.gate.Resume(request.Token); err != nil {
			w.WriteHeader(http.StatusConflict)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"state": "resumed"})
		return
	}
	state, err := c.gate.Pause(request.Token)
	if err != nil {
		w.WriteHeader(http.StatusConflict)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"state": string(state)})
}
