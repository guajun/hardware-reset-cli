package protocol

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func TestFrames(t *testing.T) {
	for _, s := range []string{"V1 1 OK HELLO HWRESET 1\n", "V1 7 ERR BUSY\r\n", "V1 0 ERR BAD_FRAME\n"} {
		if r := parse([]byte(s)); r.err != nil {
			t.Fatalf("valid %q: %v", s, r.err)
		}
	}
	for _, s := range []string{"V1 01 ERR BUSY\n", "V1 -1 ERR BUSY\n", "V1 4294967296 ERR BUSY\n", "V2 1 ERR BUSY\n", "V1 1  ERR BUSY\n", "V1 1 ERR BUSY extra\n", "V1 1 OK HELLO\x00\n"} {
		if r := parse([]byte(s)); r.err == nil {
			t.Fatalf("accepted %q", s)
		}
	}
}
func TestFragmentedResponse(t *testing.T) {
	host, device := net.Pipe()
	defer device.Close()
	c, e := New(host)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	go func() {
		line, _ := bufio.NewReader(device).ReadString('\n')
		f := strings.Fields(line)
		for _, b := range []byte(fmt.Sprintf("V1 %s OK HELLO HWRESET 1\r\n", f[1])) {
			_, _ = device.Write([]byte{b})
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	f, e := c.Request(ctx, "HELLO", "1")
	if e != nil || strings.Join(f, " ") != "HWRESET 1" {
		t.Fatalf("%v %v", f, e)
	}
}
func TestDeadlineDoesNotRetry(t *testing.T) {
	host, device := net.Pipe()
	defer device.Close()
	c, _ := New(host)
	defer c.Close()
	reads := make(chan string, 2)
	go func() {
		r := bufio.NewReader(device)
		for {
			line, e := r.ReadString('\n')
			if e != nil {
				return
			}
			reads <- line
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, e := c.Request(ctx, "RESET", "PA0", "100")
	pe, ok := e.(*Error)
	if !ok || pe.Code != "TIMEOUT" || !pe.Sent {
		t.Fatal(e)
	}
	<-reads
	select {
	case duplicate := <-reads:
		t.Fatalf("retried: %s", duplicate)
	default:
	}
}

type shortPort struct{}

func (shortPort) Read([]byte) (int, error)    { return 0, io.EOF }
func (shortPort) Write(p []byte) (int, error) { return len(p) - 1, nil }
func (shortPort) Close() error                { return nil }
func TestPartialWriteUnknown(t *testing.T) {
	c, _ := New(shortPort{})
	defer c.Close()
	_, e := c.Request(context.Background(), "RESET", "PA0", "100")
	p, ok := e.(*Error)
	if !ok || p.Code != "TRANSPORT_ERROR" || !p.Sent {
		t.Fatal(e)
	}
}
func TestOversizeResponse(t *testing.T) {
	host, device := net.Pipe()
	defer device.Close()
	c, _ := New(host)
	defer c.Close()
	go func() {
		_, _ = bufio.NewReader(device).ReadString('\n')
		_, _ = device.Write([]byte(strings.Repeat("A", 257)))
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, e := c.Request(ctx, "INFO")
	p, ok := e.(*Error)
	if !ok || p.Code != "PROTOCOL_ERROR" {
		t.Fatal(e)
	}
}
