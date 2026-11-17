package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/guajun/hardware-reset-cli/cli/internal/transport"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

func simulator(t *testing.T, handle func([]string) string) (Backend, *[]string) {
	t.Helper()
	var mu sync.Mutex
	commands := []string{}
	b := Backend{Discover: func(context.Context) ([]transport.Candidate, error) {
		return []transport.Candidate{{Port: "TEST", Serial: "ABC", VID: "0483", PID: "5740"}}, nil
	}}
	b.Open = func(context.Context, string) (io.ReadWriteCloser, error) {
		host, device := net.Pipe()
		t.Cleanup(func() { host.Close(); device.Close() })
		go func() {
			defer device.Close()
			r := bufio.NewReader(device)
			for {
				line, e := r.ReadString('\n')
				if e != nil {
					return
				}
				f := strings.Fields(line)
				if len(f) == 0 {
					continue
				}
				mu.Lock()
				commands = append(commands, strings.Join(f[2:], " "))
				mu.Unlock()
				response := ""
				switch f[2] {
				case "HELLO":
					response = "OK HELLO HWRESET 1"
				case "INFO":
					response = "OK INFO 0.1.0 ABC 1 10000 PA0,PA1"
				default:
					response = handle(f)
				}
				if response != "" {
					if _, e = fmt.Fprintf(device, "V1 %s %s\n", f[1], response); e != nil {
						return
					}
				}
			}
		}()
		return host, nil
	}
	return b, &commands
}
func call(t *testing.T, args []string, b Backend) (int, Result, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := Run(context.Background(), args, &out, &errOut, "test", b)
	var result Result
	if e := json.Unmarshal(out.Bytes(), &result); e != nil {
		t.Fatalf("invalid output: %q %v", out.String(), e)
	}
	return code, result, errOut.String()
}
func TestLocalValidation(t *testing.T) {
	b := Backend{Discover: func(context.Context) ([]transport.Candidate, error) {
		t.Fatal("discovery before validation")
		return nil, nil
	}}
	for _, args := range [][]string{{"reset", "--json"}, {"reset", "--pin", "PB0", "--json"}, {"reset", "--pin", "PA0", "--duration-ms", "10000", "--json"}, {"info", "--port", "COM1", "--serial", "ABC", "--json"}, {"info", "--pin", "PA0", "--json"}, {"nonsense", "--json"}} {
		code, r, _ := call(t, args, b)
		if code != 2 || r.Error == nil || r.Error.Outcome != "not_started" {
			t.Fatalf("%v => %d %+v", args, code, r)
		}
	}
}
func TestResetJSONAndStatus(t *testing.T) {
	b, _ := simulator(t, func(f []string) string {
		switch f[2] {
		case "RESET":
			return "OK RESET " + f[3] + " RELEASED " + f[4]
		case "STATUS":
			return "OK STATUS " + f[3] + " RELEASED 0"
		}
		return "ERR UNKNOWN_COMMAND"
	})
	code, r, _ := call(t, []string{"reset", "--port", "TEST", "--pin", "pa0", "--json"}, b)
	if code != 0 || !r.OK || r.Device.Serial != "ABC" {
		t.Fatalf("%d %+v", code, r)
	}
	data := r.Data.(map[string]any)
	if data["completed"] != true || data["duration_ms"] != float64(100) {
		t.Fatal(data)
	}
	code, r, _ = call(t, []string{"status", "--port", "TEST", "--json"}, b)
	if code != 0 || len(r.Data.(map[string]any)["pins"].([]any)) != 2 {
		t.Fatalf("%d %+v", code, r)
	}
}
func TestTimeoutUnknown(t *testing.T) {
	b, _ := simulator(t, func([]string) string { return "" })
	code, r, _ := call(t, []string{"release", "--pin", "PA0", "--port", "TEST", "--timeout-ms", "20", "--json"}, b)
	if code != 6 || r.Error.Outcome != "unknown" {
		t.Fatalf("%d %+v", code, r)
	}
}
func TestRejectedNotStarted(t *testing.T) {
	b, _ := simulator(t, func([]string) string { return "ERR BUSY" })
	code, r, _ := call(t, []string{"reset", "--pin", "PA0", "--port", "TEST", "--json"}, b)
	if code != 8 || r.Error.Outcome != "not_started" || r.Error.Code != "BUSY" {
		t.Fatalf("%d %+v", code, r)
	}
}

func TestDeviceArgumentErrorHasDeviceExitCode(t *testing.T) {
	b, _ := simulator(t, func([]string) string { return "ERR INVALID_ARGUMENT" })
	code, r, _ := call(t, []string{"reset", "--pin", "PA0", "--port", "TEST", "--json"}, b)
	if code != 8 || r.Error.Code != "INVALID_ARGUMENT" || r.Error.Outcome != "not_started" {
		t.Fatalf("%d %+v", code, r)
	}
}
func TestIncompatibleVersionIsProtocolError(t *testing.T) {
	b, _ := simulator(t, func([]string) string { return "ERR UNSUPPORTED_VERSION" })
	code, r, _ := call(t, []string{"reset", "--pin", "PA0", "--port", "TEST", "--json"}, b)
	if code != 7 || r.Error.Code != "PROTOCOL_ERROR" {
		t.Fatalf("%d %+v", code, r)
	}
}
func TestCancelledOpenNotStarted(t *testing.T) {
	b := Backend{Open: func(ctx context.Context, _ string) (io.ReadWriteCloser, error) { return nil, context.Canceled }}
	code, r, _ := call(t, []string{"info", "--port", "TEST", "--json"}, b)
	if code != 130 || r.Error.Outcome != "not_started" {
		t.Fatalf("%d %+v", code, r)
	}
}
func TestMalformedSuccessUnknown(t *testing.T) {
	b, _ := simulator(t, func([]string) string { return "OK RESET PA1 RELEASED 100" })
	code, r, _ := call(t, []string{"reset", "--pin", "PA0", "--port", "TEST", "--json"}, b)
	if code != 7 || r.Error.Outcome != "unknown" {
		t.Fatalf("%d %+v", code, r)
	}
}
func TestListEmpty(t *testing.T) {
	b := Backend{Discover: func(context.Context) ([]transport.Candidate, error) { return nil, nil }}
	code, r, _ := call(t, []string{"list", "--json"}, b)
	if code != 0 || len(r.Data.(map[string]any)["devices"].([]any)) != 0 {
		t.Fatalf("%d %+v", code, r)
	}
}
func TestAmbiguousDevices(t *testing.T) {
	b, _ := simulator(t, func([]string) string { return "" })
	b.Discover = func(context.Context) ([]transport.Candidate, error) {
		return []transport.Candidate{{Port: "A"}, {Port: "B"}}, nil
	}
	code, r, _ := call(t, []string{"info", "--json"}, b)
	if code != 4 || r.Error.Code != "AMBIGUOUS_DEVICE" {
		t.Fatalf("%d %+v", code, r)
	}
}

func TestSelectionRetainsVerifiedSession(t *testing.T) {
	for _, args := range [][]string{{"status", "--pin", "PA0", "--json"}, {"status", "--pin", "PA0", "--serial", "ABC", "--json"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			b, _ := simulator(t, func(f []string) string { return "OK STATUS " + f[3] + " RELEASED 0" })
			originalOpen := b.Open
			opens := 0
			b.Open = func(ctx context.Context, name string) (io.ReadWriteCloser, error) {
				opens++
				if opens > 1 {
					return nil, fmt.Errorf("port busy after closing a verified session")
				}
				return originalOpen(ctx, name)
			}
			code, r, diagnostics := call(t, args, b)
			if code != 0 || !r.OK || opens != 1 {
				t.Fatalf("exit=%d opens=%d result=%+v diagnostics=%s", code, opens, r, diagnostics)
			}
		})
	}
}

type countedPort struct {
	io.ReadWriteCloser
	closed *int
}

func (p countedPort) Close() error { *p.closed++; return p.ReadWriteCloser.Close() }
func TestAmbiguousSelectionClosesEverySession(t *testing.T) {
	b, _ := simulator(t, func([]string) string { return "" })
	b.Discover = func(context.Context) ([]transport.Candidate, error) {
		return []transport.Candidate{{Port: "A"}, {Port: "B"}}, nil
	}
	originalOpen := b.Open
	closed := 0
	b.Open = func(ctx context.Context, name string) (io.ReadWriteCloser, error) {
		p, e := originalOpen(ctx, name)
		if e != nil {
			return nil, e
		}
		return countedPort{p, &closed}, nil
	}
	code, _, _ := call(t, []string{"info", "--json"}, b)
	if code != 4 || closed != 2 {
		t.Fatalf("exit=%d closed=%d", code, closed)
	}
}
func TestCancelledResetReleases(t *testing.T) {
	b, _ := simulator(t, func(f []string) string {
		if f[2] == "RELEASE" {
			return "OK RELEASE PA0 RELEASED"
		}
		return ""
	})
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(30*time.Millisecond, cancel)
	var out, errOut bytes.Buffer
	code := Run(ctx, []string{"reset", "--pin", "PA0", "--port", "TEST", "--json"}, &out, &errOut, "test", b)
	var r Result
	_ = json.Unmarshal(out.Bytes(), &r)
	if code != 130 || r.Error.Outcome != "unknown" {
		t.Fatalf("%d %s %s", code, out.String(), errOut.String())
	}
}

func TestHumanOutput(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want []string
	}{
		{[]string{"reset", "--port", "TEST", "--pin", "PA0"}, []string{"reset PA0 completed (100 ms)\n"}},
		{[]string{"release", "--port", "TEST", "--pin", "PA0"}, []string{"release PA0 released\n"}},
		{[]string{"assert", "--port", "TEST", "--pin", "PA0"}, []string{"assert PA0 acknowledged", "closing the port may release early"}},
		{[]string{"info", "--port", "TEST"}, []string{"Device: TEST (serial ABC)", "Firmware: 0.1.0", "Pins: PA0, PA1"}},
		{[]string{"status", "--port", "TEST"}, []string{"PIN", "STATE", "REMAINING_MS", "PA0", "released"}},
		{[]string{"list"}, []string{"PORT", "SERIAL", "0483:5740", "ABC"}},
		{[]string{"version"}, []string{"hardware-reset-cli test"}},
	} {
		t.Run(tc.args[0], func(t *testing.T) {
			b, _ := simulator(t, func(f []string) string {
				switch f[2] {
				case "RESET":
					return "OK RESET " + f[3] + " RELEASED " + f[4]
				case "ASSERT":
					return "OK ASSERT " + f[3] + " ASSERTED " + f[4]
				case "RELEASE":
					return "OK RELEASE " + f[3] + " RELEASED"
				case "STATUS":
					return "OK STATUS " + f[3] + " RELEASED 0"
				}
				return "ERR UNKNOWN_COMMAND"
			})
			var out, diagnostics bytes.Buffer
			code := Run(context.Background(), tc.args, &out, &diagnostics, "test", b)
			if code != 0 || diagnostics.Len() != 0 {
				t.Fatalf("exit=%d stderr=%s", code, &diagnostics)
			}
			if strings.HasPrefix(out.String(), "{") {
				t.Fatalf("human output contains JSON: %s", &out)
			}
			for _, want := range tc.want {
				if !strings.Contains(out.String(), want) {
					t.Fatalf("missing %q in %q", want, out.String())
				}
			}
		})
	}
}
