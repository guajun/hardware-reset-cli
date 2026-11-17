package app

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"testing"

	"github.com/guajun/hardware-reset-cli/cli/internal/transport"
)

func TestDeviceCommandsWithoutBoard(t *testing.T) {
	for _, command := range []string{"info", "status", "reset", "assert", "release"} {
		t.Run(command, func(t *testing.T) {
			b := Backend{Discover: func(context.Context) ([]transport.Candidate, error) { return []transport.Candidate{}, nil }, Open: func(context.Context, string) (io.ReadWriteCloser, error) {
				t.Fatal("opened a port without any discovered board")
				return nil, nil
			}}
			args := []string{command, "--json"}
			if command == "reset" || command == "assert" || command == "release" {
				args = append(args, "--pin", "PA0")
			}
			code, r, diagnostics := call(t, args, b)
			if code != 3 || r.OK || r.Device != nil || r.Error == nil || r.Error.Code != "DEVICE_NOT_FOUND" || r.Error.Outcome != "not_started" {
				t.Fatalf("exit=%d result=%+v", code, r)
			}
			if !strings.Contains(diagnostics, "connect a USB board") || !strings.Contains(diagnostics, "list --json") {
				t.Fatalf("unhelpful diagnostic: %s", diagnostics)
			}
		})
	}
}

func TestMissingSerialWhenOtherBoardConnected(t *testing.T) {
	for _, knownUSBSerial := range []string{"ABC", ""} {
		t.Run("usb_serial="+knownUSBSerial, func(t *testing.T) {
			b, _ := simulator(t, func([]string) string {
				t.Error("requested GPIO operation on a nonmatching board")
				return "ERR UNKNOWN_COMMAND"
			})
			b.Discover = func(context.Context) ([]transport.Candidate, error) {
				return []transport.Candidate{{Port: "TEST", Serial: knownUSBSerial}}, nil
			}
			if knownUSBSerial != "" {
				b.Open = func(context.Context, string) (io.ReadWriteCloser, error) {
					t.Fatal("opened a USB serial that was already known not to match")
					return nil, nil
				}
			}
			code, r, diagnostics := call(t, []string{"reset", "--serial", "MISSING", "--pin", "PA0", "--json"}, b)
			if code != 3 || r.Error == nil || r.Error.Code != "DEVICE_NOT_FOUND" || r.Error.Outcome != "not_started" || !strings.Contains(diagnostics, "MISSING") {
				t.Fatalf("exit=%d result=%+v diagnostic=%s", code, r, diagnostics)
			}
		})
	}
}

func TestExplicitUnavailablePort(t *testing.T) {
	for _, command := range []string{"info", "reset"} {
		b := Backend{Discover: func(context.Context) ([]transport.Candidate, error) {
			t.Fatal("explicit port should not enumerate")
			return nil, nil
		}, Open: func(_ context.Context, name string) (io.ReadWriteCloser, error) {
			if name != "MISSING_PORT" {
				t.Fatalf("unexpected port %q", name)
			}
			return nil, os.ErrNotExist
		}}
		args := []string{command, "--port", "MISSING_PORT", "--json"}
		if command == "reset" {
			args = append(args, "--pin", "PA0")
		}
		code, r, diagnostics := call(t, args, b)
		if code != 5 || r.Error == nil || r.Error.Code != "IO_ERROR" || r.Error.Outcome != "not_started" || !strings.Contains(diagnostics, "MISSING_PORT") || !strings.Contains(diagnostics, "list --json") {
			t.Fatalf("exit=%d result=%+v diagnostic=%s", code, r, diagnostics)
		}
	}
}

func TestUnplugAfterResetSent(t *testing.T) {
	resetReceived := make(chan struct{})
	b := Backend{Open: func(context.Context, string) (io.ReadWriteCloser, error) {
		host, device := net.Pipe()
		t.Cleanup(func() { host.Close(); device.Close() })
		go func() {
			defer device.Close()
			reader := bufio.NewReader(device)
			for {
				line, err := reader.ReadString('\n')
				if err != nil {
					return
				}
				f := strings.Fields(line)
				if len(f) == 0 {
					continue
				}
				var response string
				switch f[2] {
				case "HELLO":
					response = "OK HELLO HWRESET 1"
				case "INFO":
					response = "OK INFO 0.1.0 ABC 1 10000 PA0"
				case "RESET":
					close(resetReceived)
					return
				default:
					return
				}
				if _, err = fmt.Fprintf(device, "V1 %s %s\n", f[1], response); err != nil {
					return
				}
			}
		}()
		return host, nil
	}}
	code, r, diagnostics := call(t, []string{"reset", "--port", "TEST", "--pin", "PA0", "--json"}, b)
	select {
	case <-resetReceived:
	default:
		t.Fatal("test did not reach reset transmission")
	}
	if code != 9 || r.Error == nil || r.Error.Code != "DISCONNECTED" || r.Error.Outcome != "unknown" || r.Device == nil || r.Device.Serial != "ABC" {
		t.Fatalf("exit=%d result=%+v diagnostic=%s", code, r, diagnostics)
	}
	if !strings.Contains(diagnostics, "reconnect the USB board") {
		t.Fatalf("unhelpful disconnect diagnostic: %s", diagnostics)
	}
}

func TestNoBoardTextOutputKeepsErrorOffStdout(t *testing.T) {
	b := Backend{Discover: func(context.Context) ([]transport.Candidate, error) { return nil, nil }}
	var out, diagnostics bytes.Buffer
	code := Run(context.Background(), []string{"info"}, &out, &diagnostics, "test", b)
	if code != 3 || out.Len() != 0 || !strings.Contains(diagnostics.String(), "DEVICE_NOT_FOUND") {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, out.String(), diagnostics.String())
	}
}
