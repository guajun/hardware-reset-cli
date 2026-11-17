package transport

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/gofrs/flock"
	"go.bug.st/serial"
)

type Candidate struct{ Port, Serial, VID, PID string }
type port struct {
	serial.Port
	lock *flock.Flock
}

func (p *port) Close() error {
	_ = p.SetDTR(false)
	err := p.Port.Close()
	_ = p.lock.Unlock()
	return err
}

func Open(ctx context.Context, name string) (io.ReadWriteCloser, error) {
	type result struct {
		p io.ReadWriteCloser
		e error
	}
	ch := make(chan result)
	go func() {
		p, e := open(name)
		select {
		case ch <- result{p, e}:
		case <-ctx.Done():
			if p != nil {
				p.Close()
			}
		}
	}()
	select {
	case r := <-ch:
		return r.p, r.e
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func open(name string) (io.ReadWriteCloser, error) {
	key := name
	if runtime.GOOS == "windows" {
		key = strings.ToUpper(name)
	} else if real, e := filepath.EvalSymlinks(name); e == nil {
		key = real
	}
	sum := sha256.Sum256([]byte(key))
	dir := filepath.Join(os.TempDir(), "hardware-reset-cli-locks")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	lock := flock.New(filepath.Join(dir, fmt.Sprintf("%x.lock", sum)))
	ok, err := lock.TryLock()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("port %s is in use by another hardware-reset-cli process", name)
	}
	p, err := serial.Open(name, &serial.Mode{BaudRate: 115200, DataBits: 8, Parity: serial.NoParity, StopBits: serial.OneStopBit, InitialStatusBits: &serial.ModemOutputBits{DTR: true, RTS: false}})
	if err != nil {
		lock.Unlock()
		return nil, err
	}
	if err = p.SetReadTimeout(25 * time.Millisecond); err != nil {
		p.Close()
		lock.Unlock()
		return nil, err
	}
	return &port{p, lock}, nil
}
