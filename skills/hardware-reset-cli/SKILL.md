---
name: hardware-reset-cli
description: Discover a USB hardware reset controller and use hardware-reset-cli to inspect GPIO state or reset an attached development board from scripts and AI harnesses.
---

# Hardware reset CLI

Use `hardware-reset-cli` for a Blue Pill running the matching USB CDC reset-controller firmware. The CLI controls the external board's active-low RESET through an open-drain GPIO; it does not flash firmware, use J-Link, or prove the target OS/application has booted.

## Install and discover

Check `hardware-reset-cli version --json` first. If missing, use the platform's one-line binary installer from the [project README](https://github.com/guajun/hardware-reset-cli#快速使用). Installing this skill does not install the executable or controller firmware. Go is only needed for a source build:

```sh
go install github.com/guajun/hardware-reset-cli/cli/cmd/hardware-reset-cli@latest
```

The independently installable skill is published at `skills/hardware-reset-cli/SKILL.md`:

```sh
gh skill install guajun/hardware-reset-cli hardware-reset-cli --agent codex --scope user
```

Use `--agent claude-code` or `--agent github-copilot` for other hosts.

Discover with `hardware-reset-cli list --json`. `version` works offline; `list` opens candidate serial ports to verify HELLO/INFO, so it is not a passive USB inventory. All device commands, including `info` and `status`, open and close their serial port. Opening/closing changes DTR and may release a previously asserted line. Do not run discovery or diagnostics concurrently with an intentional hold owned by another serial client.

Windows/Linux filter the development VID/PID `0483:5740`; macOS probes `/dev/cu.usbmodem*` and leaves VID/PID empty. All candidates must pass product/version checks. A USB port existing alone does not prove matching firmware is present.

## Select the controller and GPIO

Use the controller serial returned by `list` for stable selection, or an explicit current `--port`. These flags are mutually exclusive. Multiple verified devices require a choice; never silently choose the first or reuse a sample COM port, controller serial, or J-Link serial from documentation.

Determine the GPIO connected to the external board's RESET from the user's wiring or project configuration. Firmware v1 exposes PA0–PA7; USB/SWD/LED pins cannot be controlled. An available GPIO is not evidence that it is connected to the intended target. Resolve genuinely missing wiring information before a reset.

The following PowerShell variables represent the resolved controller and pin; adapt quoting to the current shell:

```powershell
hardware-reset-cli info --serial $ControllerSerial --json
hardware-reset-cli status --serial $ControllerSerial --pin $Pin --json
hardware-reset-cli reset --serial $ControllerSerial --pin $Pin --duration-ms 100 --json
```

Use existing authorization when it covers the chosen board and reset. If authorization or target selection is missing, present the intended operation and resolve only what is missing. Reset/assert/release execute immediately; there is no dry-run or `--yes` flag. Do not issue a reset merely to diagnose an absent device.

## Timing and results

- Prefer `reset` for a timed pulse. It waits for device-side release before reporting `completed: true`. Durations are 1–10000 ms; default 100 ms. Select timing appropriate to the actual target.
- Request timeout defaults to 2000 ms and must be at least duration + 500 ms. For a 3000 ms pulse, explicitly use `--duration-ms 3000 --timeout-ms 3500` or longer.
- `assert --hold-ms ...` only acknowledges assertion; the CLI then closes its port and DTR falling can release immediately. It is not a substitute for a guaranteed duration. Persistent protocol clients can hold the port open until lease expiry or `release`.
- `status` reports the controller's commanded state, not measured pin voltage. `released` means the open-drain output is released; the target's pull-up establishes the voltage.
- `reset` success confirms GPIO timing, not target boot, application health, or debugger reconnection. Check those separately when the task requires them.

Use JSON stdout and the process exit code together. Top-level schema fields are `schema_version`, `ok`, `command`, `device`, `data`, `error`. Diagnostics go to stderr. Branch on `error.code` and `error.outcome`, not message wording.

## Missing device and failures

- No verified controller discovered: `list --json` succeeds with `devices: []`; device commands fail with exit 3 / `DEVICE_NOT_FOUND`, outcome `not_started`. This alone does not prove a physical board is unplugged: it may have different firmware, a driver issue, or be inaccessible.
- Explicit unavailable port, permissions, or contention: exit 5 / `IO_ERROR`. Check `list --json`, the USB data connection, matching firmware, OS permissions, and another process owning the port. Do not use administrator/root by default.
- Multiple devices: exit 4 / `AMBIGUOUS_DEVICE`; select a controller explicitly.
- Invalid local arguments: exit 2. Malformed/incompatible protocol: exit 7. Device rejection such as BUSY: exit 8.
- Deadline: exit 6 / `TIMEOUT`. Disconnect/transport failure: exit 9. Once a write may have been sent, outcome is `unknown`; never automatically repeat reset or assertion. A later released status cannot prove whether an earlier pulse happened.
- User interruption: exit 130. The CLI attempts RELEASE within 500 ms; `cancelled` requires confirmation of the original cancellation. Otherwise outcome remains unknown (or `not_started` if definitely unsent).

USB reset/disconnect and detected DTR falling release outputs. Device-side lease expiry depends on the MCU continuing to run; it is not an independent hardware watchdog. For implementation details or raw serial work, consult the [CLI contract](https://github.com/guajun/hardware-reset-cli/blob/main/docs/cli.md) and [USB protocol](https://github.com/guajun/hardware-reset-cli/blob/main/docs/usb-protocol.md).
