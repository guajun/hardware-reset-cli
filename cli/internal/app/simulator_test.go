package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/guajun/hardware-reset-cli/cli/internal/transport"
)

// HWRESET_SIM points to the executable built from the actual firmware protocol
// core. These tests deliberately do not replace firmware responses with mocks.
type simulatorPort struct {
	input   io.WriteCloser
	output  io.ReadCloser
	cmd     *exec.Cmd
	onReset func()
	once    sync.Once
}

func (p *simulatorPort) Read(b []byte) (int, error) { return p.output.Read(b) }
func (p *simulatorPort) Write(b []byte) (int, error) {
	n, err := p.input.Write(b)
	if err == nil && strings.Contains(string(b), " RESET ") && p.onReset != nil {
		p.onReset()
	}
	return n, err
}
func (p *simulatorPort) Close() error {
	p.once.Do(func() {
		_ = p.input.Close()
		done := make(chan struct{})
		go func() { _ = p.cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			_ = p.cmd.Process.Kill()
			<-done
		}
		_ = p.output.Close()
	})
	return nil
}

func simulatorBackend(t *testing.T, onReset func()) Backend {
	t.Helper()
	path := os.Getenv("HWRESET_SIM")
	if path == "" {
		t.Skip("set HWRESET_SIM to run tests against the C firmware simulator")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("HWRESET_SIM: %v", err)
	}
	return Backend{
		Discover: func(context.Context) ([]transport.Candidate, error) {
			return []transport.Candidate{{Port: "SIM", Serial: "0123456789ABCDEF01234567", VID: "0483", PID: "5740"}}, nil
		},
		Open: func(ctx context.Context, _ string) (io.ReadWriteCloser, error) {
			cmd := exec.Command(path)
			cmd.Stderr = os.Stderr
			input, err := cmd.StdinPipe()
			if err != nil {
				return nil, err
			}
			output, err := cmd.StdoutPipe()
			if err != nil {
				_ = input.Close()
				return nil, err
			}
			if err := cmd.Start(); err != nil {
				_ = input.Close()
				_ = output.Close()
				return nil, err
			}
			p := &simulatorPort{input: input, output: output, cmd: cmd, onReset: onReset}
			t.Cleanup(func() { _ = p.Close() })
			return p, nil
		},
	}
}

func TestFirmwareSimulatorCommands(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"list", []string{"list"}},
		{"info", []string{"info", "--serial", "0123456789ABCDEF01234567"}},
		{"status", []string{"status", "--port", "SIM"}},
		{"reset", []string{"reset", "--port", "SIM", "--pin", "PA0", "--duration-ms", "70"}},
		{"assert", []string{"assert", "--port", "SIM", "--pin", "PA7", "--hold-ms", "100"}},
		{"release", []string{"release", "--port", "SIM", "--pin", "PA7"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := simulatorBackend(t, nil)
			var out, diagnostics bytes.Buffer
			started := time.Now()
			code := Run(context.Background(), append(tc.args, "--json"), &out, &diagnostics, "0.1.0", b)
			if code != 0 {
				t.Fatalf("exit=%d stdout=%s stderr=%s", code, &out, &diagnostics)
			}
			var r Result
			decoder := json.NewDecoder(&out)
			if err := decoder.Decode(&r); err != nil {
				t.Fatal(err)
			}
			if err := decoder.Decode(new(any)); err != io.EOF {
				t.Fatalf("extra stdout content: %v", err)
			}
			if !r.OK || r.Error != nil || r.SchemaVersion != 1 {
				t.Fatalf("unexpected result: %+v", r)
			}
			data := r.Data.(map[string]any)
			switch tc.name {
			case "list":
				if len(data["devices"].([]any)) != 1 {
					t.Fatalf("devices: %v", data)
				}
			case "info":
				if data["firmware"] != "0.1.0" || len(data["pins"].([]any)) != 8 {
					t.Fatalf("info: %v", data)
				}
			case "status":
				if len(data["pins"].([]any)) != 8 {
					t.Fatalf("status: %v", data)
				}
			case "reset":
				if data["completed"] != true || data["state"] != "released" || data["duration_ms"] != float64(70) {
					t.Fatalf("reset: %v", data)
				}
				if time.Since(started) < 60*time.Millisecond {
					t.Fatal("RESET succeeded before C timer completed")
				}
			}
		})
	}
}

func TestFirmwareSimulatorCancelReset(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var once sync.Once
	b := simulatorBackend(t, func() {
		once.Do(func() {
			// Exercise cancellation while awaiting a final response. Cancellation
			// during the write itself correctly has an unknown outcome instead.
			go func() { time.Sleep(30 * time.Millisecond); cancel() }()
		})
	})
	var out, diagnostics bytes.Buffer
	code := Run(ctx, []string{"reset", "--port", "SIM", "--pin", "PA0", "--duration-ms", "1000", "--json"}, &out, &diagnostics, "0.1.0", b)
	if code != 130 {
		t.Fatalf("exit=%d stdout=%s stderr=%s", code, &out, &diagnostics)
	}
	var r Result
	if err := json.Unmarshal(out.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	if r.Error == nil || r.Error.Code != "INTERRUPTED" || r.Error.Outcome != "cancelled" {
		t.Fatalf("device did not confirm cancellation: %+v", r.Error)
	}
}
