// Package protocol implements the bounded V1 hardware-reset wire protocol.
package protocol

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Error struct {
	Code     string
	Message  string
	Sent     bool
	Rejected bool
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

type response struct {
	id     uint32
	fields []string
	err    error
}
type Client struct {
	port       io.ReadWriteCloser
	incoming   chan response
	done       chan struct{}
	readerDone chan struct{}
	next       uint32
	used       map[uint32]bool
	received   map[uint32]response
	lastID     uint32
	closeOnce  sync.Once
	closeErr   error
}

func New(port io.ReadWriteCloser) (*Client, error) {
	var seed [4]byte
	if _, err := rand.Read(seed[:]); err != nil {
		return nil, err
	}
	id := binary.LittleEndian.Uint32(seed[:]) & 0x7fffffff
	if id == 0 {
		id = 1
	}
	c := &Client{port: port, incoming: make(chan response, 8), done: make(chan struct{}), readerDone: make(chan struct{}), next: id, used: make(map[uint32]bool), received: make(map[uint32]response)}
	go c.readLoop()
	return c, nil
}
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		close(c.done)
		c.closeErr = c.port.Close()
		// Allow the pending read to leave the driver after handle closure. Keep
		// shutdown bounded even if a platform driver fails to unblock its read.
		select {
		case <-c.readerDone:
		case <-time.After(100 * time.Millisecond):
		}
	})
	return c.closeErr
}
func (c *Client) deliver(r response) bool {
	select {
	case c.incoming <- r:
		return true
	case <-c.done:
		return false
	}
}
func (c *Client) readLoop() {
	defer close(c.readerDone)
	var line []byte
	buf := make([]byte, 64)
	for {
		n, err := c.port.Read(buf)
		for _, b := range buf[:n] {
			line = append(line, b)
			if len(line) > 256 {
				c.deliver(response{err: &Error{Code: "PROTOCOL_ERROR", Message: "response exceeds 256 bytes"}})
				return
			}
			if b == '\n' {
				r := parse(line)
				line = nil
				if !c.deliver(r) {
					return
				}
				if r.err != nil {
					return
				}
			}
		}
		if err != nil {
			c.deliver(response{err: &Error{Code: "DISCONNECTED", Message: "device connection lost: " + err.Error() + "; reconnect the USB board and run hardware-reset-cli list --json"}})
			return
		}
		select {
		case <-c.done:
			return
		default:
		}
		if n == 0 {
			time.Sleep(time.Millisecond)
		}
	}
}
func parse(line []byte) response {
	bad := func() response {
		return response{err: &Error{Code: "PROTOCOL_ERROR", Message: "malformed device response"}}
	}
	s := strings.TrimSuffix(string(line), "\n")
	s = strings.TrimSuffix(s, "\r")
	if s == "" {
		return bad()
	}
	for _, b := range []byte(s) {
		if b < 32 || b > 126 {
			return bad()
		}
	}
	f := strings.Split(s, " ")
	for _, v := range f {
		if v == "" {
			return bad()
		}
	}
	if len(f) < 4 || f[0] != "V1" {
		return bad()
	}
	id, err := Number(f[1], 0, 4294967295)
	if err != nil {
		return bad()
	}
	if f[2] == "ERR" {
		if len(f) != 4 {
			return bad()
		}
	} else if f[2] != "OK" {
		return bad()
	}
	return response{id: uint32(id), fields: f[2:]}
}
func Number(s string, min, max uint64) (uint64, error) {
	if s == "" || (len(s) > 1 && s[0] == '0') {
		return 0, errors.New("noncanonical integer")
	}
	for _, b := range []byte(s) {
		if b < '0' || b > '9' {
			return 0, errors.New("invalid integer")
		}
	}
	n, e := strconv.ParseUint(s, 10, 64)
	if e != nil || n < min || n > max {
		return 0, errors.New("integer out of range")
	}
	return n, nil
}
func (c *Client) write(ctx context.Context, b []byte) error {
	done := make(chan error, 1)
	go func() {
		n, e := c.port.Write(b)
		if e == nil && n != len(b) {
			e = io.ErrShortWrite
		}
		done <- e
	}()
	select {
	case err := <-done:
		if err != nil {
			return &Error{Code: "TRANSPORT_ERROR", Message: err.Error(), Sent: true}
		}
		return nil
	case <-ctx.Done():
		c.Close()
		return contextError(ctx, true)
	}
}
func contextError(ctx context.Context, sent bool) *Error {
	code := "TIMEOUT"
	if errors.Is(ctx.Err(), context.Canceled) {
		code = "INTERRUPTED"
	}
	return &Error{Code: code, Message: ctx.Err().Error(), Sent: sent}
}
func (c *Client) Sync(ctx context.Context) error { return c.write(ctx, []byte("\n")) }

// Request is intentionally sequential. It never retransmits a request.
func (c *Client) Request(ctx context.Context, command string, args ...string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, contextError(ctx, false)
	}
	if c.next == 0 {
		return nil, &Error{Code: "PROTOCOL_ERROR", Message: "request ID space exhausted; reopen the device"}
	}
	id := c.next
	c.lastID = id
	c.next++
	c.used[id] = true
	line := fmt.Sprintf("V1 %d %s", id, command)
	if len(args) > 0 {
		line += " " + strings.Join(args, " ")
	}
	line += "\n"
	if len(line) > 256 {
		return nil, &Error{Code: "PROTOCOL_ERROR", Message: "request exceeds frame limit"}
	}
	if err := c.write(ctx, []byte(line)); err != nil {
		return nil, err
	}
	for {
		select {
		case <-ctx.Done():
			return nil, contextError(ctx, true)
		case r := <-c.incoming:
			if r.err != nil {
				var e *Error
				if errors.As(r.err, &e) {
					e.Sent = true
				}
				return nil, r.err
			}
			if c.used[r.id] {
				c.received[r.id] = r
			}
			if r.id != id {
				if r.id != 0 && c.used[r.id] {
					continue
				}
				return nil, &Error{Code: "PROTOCOL_ERROR", Message: "unexpected response ID", Sent: true}
			}
			if r.fields[0] == "ERR" {
				return nil, &Error{Code: r.fields[1], Message: "device rejected request", Sent: true, Rejected: true}
			}
			if r.fields[1] != command {
				return nil, &Error{Code: "PROTOCOL_ERROR", Message: "response command mismatch", Sent: true}
			}
			return r.fields[2:], nil
		}
	}
}

// LastRequestID and Cancelled are called by the same sequential request owner.
// A later RELEASE response alone cannot prove the earlier RESET was cancelled.
func (c *Client) LastRequestID() uint32 { return c.lastID }
func (c *Client) Cancelled(id uint32) bool {
	r, ok := c.received[id]
	return ok && len(r.fields) == 2 && r.fields[0] == "ERR" && r.fields[1] == "CANCELLED"
}
