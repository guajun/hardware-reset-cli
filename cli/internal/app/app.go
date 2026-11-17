// Package app provides a testable CLI entry point with injectable serial I/O.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/guajun/hardware-reset-cli/cli/internal/protocol"
	"github.com/guajun/hardware-reset-cli/cli/internal/transport"
)

type Backend struct {
	Discover func(context.Context) ([]transport.Candidate, error)
	Open     func(context.Context, string) (io.ReadWriteCloser, error)
}

func DefaultBackend() Backend { return Backend{transport.Discover, transport.Open} }

type Device struct {
	Port   string `json:"port"`
	Serial string `json:"serial"`
}
type Failure struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	Outcome  string `json:"outcome"`
	Rejected bool   `json:"-"`
}
type Result struct {
	SchemaVersion int      `json:"schema_version"`
	OK            bool     `json:"ok"`
	Command       string   `json:"command"`
	Device        *Device  `json:"device"`
	Data          any      `json:"data"`
	Error         *Failure `json:"error"`
}
type Info struct {
	Firmware    string   `json:"firmware"`
	Protocol    int      `json:"protocol"`
	Pins        []string `json:"pins"`
	MaxDuration int      `json:"max_duration_ms"`
}
type Listed struct {
	Device
	VID      string `json:"vid"`
	PID      string `json:"pid"`
	Firmware string `json:"firmware"`
	Protocol int    `json:"protocol"`
}
type unavailable struct {
	Port   string `json:"port"`
	Reason string `json:"reason"`
}
type options struct {
	command, port, serial, pin string
	duration, hold, timeout    int
	json, verbose, help        bool
}
type session struct {
	device    Device
	info      Info
	client    *protocol.Client
	candidate transport.Candidate
}

const help = `hardware-reset-cli — USB GPIO reset controller

Usage: hardware-reset-cli <command> [options]
Commands: list, info, status, reset, assert, release, version
Options:
  --port NAME       Explicit serial port (mutually exclusive with --serial)
  --serial SERIAL   Select a stable device serial
  --pin PA0         PA0..PA7; required for reset/assert/release
  --duration-ms N   reset pulse, 1..10000 (default 100)
  --hold-ms N       assert lease, 1..10000 (default 1000)
  --timeout-ms N    per request timeout, 1..60000 (default 2000)
  --json            One JSON object on stdout
  --verbose         Diagnostics on stderr
  --help            Show help

reset waits until the device releases the line. assert only confirms assertion;
closing the port may release it immediately. Use reset for a timed pulse.
Write operations are never automatically retried; timeout means outcome unknown.
`

func parse(args []string) (options, error) {
	o := options{duration: 100, hold: 1000, timeout: 2000}
	// Keep JSON error reporting available even when another argument is invalid.
	for _, a := range args {
		if a == "--json" || a == "--json=true" {
			o.json = true
		}
	}
	if len(args) == 0 {
		o.help = true
		return o, nil
	}
	// Flags may precede or follow the command, but no positional arguments exist.
	reordered := []string{}
	commands := []string{}
	valueFlags := map[string]bool{"--port": true, "--serial": true, "--pin": true, "--duration-ms": true, "--hold-ms": true, "--timeout-ms": true}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			commands = append(commands, a)
			continue
		}
		reordered = append(reordered, a)
		if valueFlags[a] {
			if i+1 == len(args) {
				return o, fmt.Errorf("missing value for %s", a)
			}
			i++
			reordered = append(reordered, args[i])
		}
	}
	if len(commands) > 1 {
		return o, errors.New("only one command is allowed")
	}
	if len(commands) == 1 {
		o.command = commands[0]
	}
	f := flag.NewFlagSet("hardware-reset-cli", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	f.StringVar(&o.port, "port", "", "")
	f.StringVar(&o.serial, "serial", "", "")
	f.StringVar(&o.pin, "pin", "", "")
	f.IntVar(&o.duration, "duration-ms", 100, "")
	f.IntVar(&o.hold, "hold-ms", 1000, "")
	f.IntVar(&o.timeout, "timeout-ms", 2000, "")
	f.BoolVar(&o.json, "json", o.json, "")
	f.BoolVar(&o.verbose, "verbose", false, "")
	f.BoolVar(&o.help, "help", false, "")
	f.BoolVar(&o.help, "h", false, "")
	if err := f.Parse(reordered); err != nil {
		return o, err
	}
	if o.help {
		return o, nil
	}
	if !slices.Contains([]string{"list", "info", "status", "reset", "assert", "release", "version"}, o.command) {
		return o, errors.New("unknown or missing command")
	}
	if o.port != "" && o.serial != "" {
		return o, errors.New("--port and --serial are mutually exclusive")
	}
	if o.timeout < 1 || o.timeout > 60000 {
		return o, errors.New("--timeout-ms must be 1..60000")
	}
	o.pin = strings.ToUpper(o.pin)
	if o.pin != "" && !validPin(o.pin) {
		return o, errors.New("--pin must be PA0..PA7")
	}
	if slices.Contains([]string{"reset", "assert", "release"}, o.command) && o.pin == "" {
		return o, errors.New("--pin is required")
	}
	if o.duration < 1 || o.duration > 10000 || o.hold < 1 || o.hold > 10000 {
		return o, errors.New("duration and hold must be 1..10000 ms")
	}
	if o.command == "reset" && o.timeout < o.duration+500 {
		return o, errors.New("--timeout-ms must be at least --duration-ms + 500")
	}
	allowed := map[string]bool{"json": true, "verbose": true, "help": true, "h": true}
	if o.command != "version" {
		allowed["timeout-ms"] = true
	}
	if o.command != "version" && o.command != "list" {
		allowed["port"] = true
		allowed["serial"] = true
	}
	if slices.Contains([]string{"status", "reset", "assert", "release"}, o.command) {
		allowed["pin"] = true
	}
	if o.command == "reset" {
		allowed["duration-ms"] = true
	}
	if o.command == "assert" {
		allowed["hold-ms"] = true
	}
	var bad string
	f.Visit(func(v *flag.Flag) {
		if !allowed[v.Name] {
			bad = v.Name
		}
	})
	if bad != "" {
		return o, fmt.Errorf("--%s is not valid for %s", bad, o.command)
	}
	return o, nil
}
func validPin(s string) bool { return len(s) == 3 && s[:2] == "PA" && s[2] >= '0' && s[2] <= '7' }

func Run(ctx context.Context, args []string, out, errOut io.Writer, version string, b Backend) int {
	o, err := parse(args)
	r := Result{SchemaVersion: 1, Command: o.command}
	if err != nil {
		r.Error = &Failure{Code: "INVALID_ARGUMENT", Message: err.Error(), Outcome: "not_started"}
		return emit(o, r, out, errOut)
	}
	if o.help {
		fmt.Fprint(out, help)
		return 0
	}
	if o.command == "version" {
		r.OK = true
		r.Data = map[string]any{"cli_version": version, "supported_protocols": []int{1}}
		return emit(o, r, out, errOut)
	}
	if o.command == "list" {
		devices, failed, e := list(ctx, o, b, errOut)
		if e != nil {
			r.Error = fail(e, false)
		} else {
			r.OK = true
			r.Data = map[string]any{"devices": devices, "unavailable": failed}
		}
		return emit(o, r, out, errOut)
	}
	s, e := selectDevice(ctx, o, b, errOut)
	if e != nil {
		r.Error = fail(e, false)
		return emit(o, r, out, errOut)
	}
	defer s.client.Close()
	r.Device = &s.device
	data, e := execute(ctx, o, s)
	if e != nil {
		r.Error = fail(e, slices.Contains([]string{"reset", "assert", "release"}, o.command))
	} else {
		r.OK = true
		r.Data = data
	}
	return emit(o, r, out, errOut)
}
func timed(ctx context.Context, ms int) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, time.Duration(ms)*time.Millisecond)
}
func request(ctx context.Context, c *protocol.Client, ms int, cmd string, args ...string) ([]string, error) {
	x, cancel := timed(ctx, ms)
	defer cancel()
	return c.Request(x, cmd, args...)
}
func badResponse(message string) error {
	return &protocol.Error{Code: "PROTOCOL_ERROR", Message: message, Sent: true}
}
func token(s string) bool {
	if len(s) < 1 || len(s) > 32 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}
func connect(ctx context.Context, o options, b Backend, p transport.Candidate) (*session, error) {
	x, cancel := timed(ctx, 5000)
	port, e := b.Open(x, p.Port)
	cancel()
	if e != nil {
		failure := environmentError(e)
		failure.Message = fmt.Sprintf("cannot open port %q: %v; check the USB connection and port access, then run hardware-reset-cli list --json", p.Port, e)
		return nil, failure
	}
	c, e := protocol.New(port)
	if e != nil {
		port.Close()
		return nil, &protocol.Error{Code: "IO_ERROR", Message: e.Error()}
	}
	ok := false
	defer func() {
		if !ok {
			c.Close()
		}
	}()
	x, cancel = timed(ctx, o.timeout)
	e = c.Sync(x)
	cancel()
	if e != nil {
		return nil, e
	}
	hello, e := request(ctx, c, o.timeout, "HELLO", "1")
	if e != nil {
		return nil, e
	}
	if len(hello) != 2 || hello[0] != "HWRESET" || hello[1] != "1" {
		return nil, badResponse("incompatible product or protocol")
	}
	fields, e := request(ctx, c, o.timeout, "INFO")
	if e != nil {
		return nil, e
	}
	if len(fields) != 5 || !token(fields[0]) || !token(fields[1]) || fields[2] != "1" {
		return nil, badResponse("invalid INFO response")
	}
	maximum, e := protocol.Number(fields[3], 1, 10000)
	if e != nil {
		return nil, badResponse("invalid maximum duration")
	}
	pins := strings.Split(fields[4], ",")
	seen := map[string]bool{}
	for _, pin := range pins {
		if !validPin(pin) || seen[pin] {
			return nil, badResponse("invalid pin capability list")
		}
		seen[pin] = true
	}
	if p.Serial != "" && p.Serial != fields[1] {
		return nil, badResponse("USB and protocol serial numbers differ")
	}
	ok = true
	return &session{Device{p.Port, fields[1]}, Info{fields[0], 1, pins, int(maximum)}, c, p}, nil
}

func candidates(ctx context.Context, b Backend) ([]transport.Candidate, error) {
	x, cancel := timed(ctx, 5000)
	defer cancel()
	p, e := b.Discover(x)
	if e != nil {
		return nil, environmentError(e)
	}
	return p, nil
}
func list(ctx context.Context, o options, b Backend, errOut io.Writer) ([]Listed, []unavailable, error) {
	out := []Listed{}
	failed := []unavailable{}
	p, e := candidates(ctx, b)
	if e != nil {
		return out, failed, e
	}
	for _, candidate := range p {
		if e = ctx.Err(); e != nil {
			return out, failed, &protocol.Error{Code: "INTERRUPTED", Message: e.Error()}
		}
		s, e := connect(ctx, o, b, candidate)
		if e != nil {
			failed = append(failed, unavailable{candidate.Port, e.Error()})
			continue
		}
		out = append(out, Listed{s.device, candidate.VID, candidate.PID, s.info.Firmware, s.info.Protocol})
		s.client.Close()
	}
	if err := ctx.Err(); err != nil {
		return out, failed, environmentError(err)
	}
	return out, failed, nil
}
func selectDevice(ctx context.Context, o options, b Backend, errOut io.Writer) (*session, error) {
	if o.port != "" {
		return connect(ctx, o, b, transport.Candidate{Port: o.port})
	}
	p, e := candidates(ctx, b)
	if e != nil {
		return nil, e
	}
	// Keep the first verified session open while checking for ambiguity. Closing
	// and reopening it can race pending Windows USB reads and also drops DTR.
	var selected *session
	keep := false
	defer func() {
		if !keep && selected != nil {
			selected.client.Close()
		}
	}()
	for _, candidate := range p {
		if candidate.Serial != "" && o.serial != "" && candidate.Serial != o.serial {
			continue
		}
		s, e := connect(ctx, o, b, candidate)
		if e != nil {
			if o.verbose {
				fmt.Fprintf(errOut, "%s: %v\n", candidate.Port, e)
			}
			continue
		}
		if o.serial == "" || s.device.Serial == o.serial {
			if selected != nil {
				s.client.Close()
				return nil, &protocol.Error{Code: "AMBIGUOUS_DEVICE", Message: "multiple matching devices; specify --port or --serial"}
			}
			selected = s
		} else {
			s.client.Close()
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, &protocol.Error{Code: "INTERRUPTED", Message: err.Error()}
	}
	if selected == nil {
		message := "no verified hardware-reset device found"
		if o.serial != "" {
			message = fmt.Sprintf("no verified hardware-reset device with serial %q found", o.serial)
		}
		return nil, &protocol.Error{Code: "DEVICE_NOT_FOUND", Message: message + "; connect a USB board running the controller firmware, then run hardware-reset-cli list --json"}
	}
	keep = true
	return selected, nil
}
func execute(ctx context.Context, o options, s *session) (any, error) {
	if o.command == "info" {
		return s.info, nil
	}
	if o.pin != "" && !slices.Contains(s.info.Pins, o.pin) {
		return nil, &protocol.Error{Code: "INVALID_ARGUMENT", Message: "pin not supported by this firmware"}
	}
	if o.command == "reset" && o.duration > s.info.MaxDuration || o.command == "assert" && o.hold > s.info.MaxDuration {
		return nil, &protocol.Error{Code: "INVALID_ARGUMENT", Message: "duration exceeds device capability"}
	}
	if o.command == "status" {
		pins := s.info.Pins
		if o.pin != "" {
			pins = []string{o.pin}
		}
		out := []any{}
		for _, pin := range pins {
			f, e := request(ctx, s.client, o.timeout, "STATUS", pin)
			if e != nil {
				return nil, e
			}
			if len(f) != 3 || f[0] != pin || (f[1] != "RELEASED" && f[1] != "ASSERTED") {
				return nil, badResponse("invalid STATUS response")
			}
			remaining, e := protocol.Number(f[2], 0, 10000)
			if e != nil || f[1] == "RELEASED" && remaining != 0 {
				return nil, badResponse("invalid STATUS remaining time")
			}
			out = append(out, map[string]any{"pin": pin, "state": strings.ToLower(f[1]), "remaining_ms": remaining})
		}
		return map[string]any{"pins": out}, nil
	}
	args := []string{o.pin}
	if o.command == "reset" {
		args = append(args, strconv.Itoa(o.duration))
	}
	if o.command == "assert" {
		args = append(args, strconv.Itoa(o.hold))
	}
	f, e := request(ctx, s.client, o.timeout, strings.ToUpper(o.command), args...)
	if e != nil {
		if ctx.Err() != nil {
			var wireError *protocol.Error
			if errors.As(e, &wireError) && !wireError.Sent {
				return nil, e
			}
			outcome := "unknown"
			originalID := s.client.LastRequestID()
			x, cancel := timed(context.Background(), 500)
			released, re := s.client.Request(x, "RELEASE", o.pin)
			cancel()
			if re == nil && len(released) == 2 && released[0] == o.pin && released[1] == "RELEASED" && s.client.Cancelled(originalID) {
				outcome = "cancelled"
			}
			return nil, &cancelError{outcome}
		}
		return nil, e
	}
	expected := 2
	state := "RELEASED"
	if o.command != "release" {
		expected = 3
	}
	if o.command == "assert" {
		state = "ASSERTED"
	}
	if len(f) != expected || f[0] != o.pin || f[1] != state {
		return nil, badResponse("invalid write command response")
	}
	data := map[string]any{"pin": o.pin, "state": strings.ToLower(state)}
	if o.command == "reset" {
		if f[2] != strconv.Itoa(o.duration) {
			return nil, badResponse("RESET duration mismatch")
		}
		data["duration_ms"] = o.duration
		data["completed"] = true
	}
	if o.command == "assert" {
		if f[2] != strconv.Itoa(o.hold) {
			return nil, badResponse("ASSERT lease mismatch")
		}
		data["hold_ms"] = o.hold
		data["remaining_ms"] = o.hold
	}
	return data, nil
}

type cancelError struct{ outcome string }

func (e *cancelError) Error() string { return "interrupted" }
func fail(err error, write bool) *Failure {
	var cancelled *cancelError
	if errors.As(err, &cancelled) {
		return &Failure{Code: "INTERRUPTED", Message: "operation interrupted", Outcome: cancelled.outcome}
	}
	var e *protocol.Error
	if !errors.As(err, &e) {
		return &Failure{Code: "IO_ERROR", Message: err.Error(), Outcome: "not_started"}
	}
	outcome := "not_started"
	if write && e.Sent {
		outcome = "unknown"
		if slices.Contains([]string{"BAD_FRAME", "LINE_TOO_LONG", "FRAME_TIMEOUT", "UNSUPPORTED_VERSION", "HANDSHAKE_REQUIRED", "UNKNOWN_COMMAND", "INVALID_PIN", "INVALID_ARGUMENT", "BUSY"}, e.Code) {
			outcome = "not_started"
		}
		if e.Code == "CANCELLED" {
			outcome = "cancelled"
		}
	}
	if e.Code == "UNSUPPORTED_VERSION" {
		return &Failure{Code: "PROTOCOL_ERROR", Message: e.Message, Outcome: outcome}
	}
	return &Failure{Code: e.Code, Message: e.Message, Outcome: outcome, Rejected: e.Rejected}
}
func emit(o options, r Result, out, errOut io.Writer) int {
	code := 0
	if r.Error != nil {
		switch r.Error.Code {
		case "INVALID_ARGUMENT":
			code = 2
		case "DEVICE_NOT_FOUND":
			code = 3
		case "AMBIGUOUS_DEVICE":
			code = 4
		case "IO_ERROR":
			code = 5
		case "TIMEOUT":
			code = 6
		case "PROTOCOL_ERROR":
			code = 7
		case "DISCONNECTED", "TRANSPORT_ERROR":
			code = 9
		case "INTERRUPTED":
			code = 130
		default:
			code = 8
		}
		if r.Error.Rejected && r.Error.Code != "UNSUPPORTED_VERSION" {
			code = 8
		}
		fmt.Fprintf(errOut, "%s: %s (outcome: %s)\n", r.Error.Code, r.Error.Message, r.Error.Outcome)
	}
	if o.json {
		if e := json.NewEncoder(out).Encode(r); e != nil {
			fmt.Fprintln(errOut, e)
			return 5
		}
	} else if r.OK {
		if _, err := io.WriteString(out, humanResult(r)); err != nil {
			fmt.Fprintln(errOut, err)
			return 5
		}
	}
	return code
}

func humanResult(r Result) string {
	var out strings.Builder
	if r.Command == "info" {
		info := r.Data.(Info)
		fmt.Fprintf(&out, "Device: %s (serial %s)\nFirmware: %s\nProtocol: %d\nPins: %s\nMaximum duration: %d ms\n", r.Device.Port, r.Device.Serial, info.Firmware, info.Protocol, strings.Join(info.Pins, ", "), info.MaxDuration)
		return out.String()
	}
	data := r.Data.(map[string]any)
	switch r.Command {
	case "reset":
		fmt.Fprintf(&out, "reset %s completed (%d ms)\n", data["pin"], data["duration_ms"])
	case "release":
		fmt.Fprintf(&out, "release %s released\n", data["pin"])
	case "assert":
		fmt.Fprintf(&out, "assert %s acknowledged (lease %d ms; closing the port may release early)\n", data["pin"], data["hold_ms"])
	case "version":
		fmt.Fprintf(&out, "hardware-reset-cli %s (protocols: %v)\n", data["cli_version"], data["supported_protocols"])
	case "list":
		devices := data["devices"].([]Listed)
		if len(devices) == 0 {
			out.WriteString("No hardware reset devices found.\n")
		} else {
			table := tabwriter.NewWriter(&out, 0, 4, 2, ' ', 0)
			fmt.Fprintln(table, "PORT\tSERIAL\tVID:PID\tFIRMWARE\tPROTOCOL")
			for _, device := range devices {
				usb := device.VID + ":" + device.PID
				if usb == ":" {
					usb = "unknown"
				}
				fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%d\n", device.Port, device.Serial, usb, device.Firmware, device.Protocol)
			}
			table.Flush()
		}
		for _, candidate := range data["unavailable"].([]unavailable) {
			fmt.Fprintf(&out, "Unavailable: %s — %s\n", candidate.Port, candidate.Reason)
		}
	case "status":
		table := tabwriter.NewWriter(&out, 0, 4, 2, ' ', 0)
		fmt.Fprintln(table, "PIN\tSTATE\tREMAINING_MS")
		for _, entry := range data["pins"].([]any) {
			pin := entry.(map[string]any)
			fmt.Fprintf(table, "%s\t%s\t%d\n", pin["pin"], pin["state"], pin["remaining_ms"])
		}
		table.Flush()
	}
	return out.String()
}

func environmentError(err error) *protocol.Error {
	code := "IO_ERROR"
	if errors.Is(err, context.Canceled) {
		code = "INTERRUPTED"
	} else if errors.Is(err, context.DeadlineExceeded) {
		code = "TIMEOUT"
	}
	return &protocol.Error{Code: code, Message: err.Error()}
}
