# Hardware reset CLI

Go 1.23 或更新版本，支持 Windows、Linux、macOS，无需 cgo。

```sh
go build -o ../bin/hardware-reset-cli ./cmd/hardware-reset-cli
go test ./...
go vet ./...
```

Windows 输出文件可命名为 `../bin/hardware-reset-cli.exe`。

```sh
hardware-reset-cli list --json
hardware-reset-cli reset --port COM5 --pin PA0 --duration-ms 100 --json
hardware-reset-cli status --serial YOUR_SERIAL --json
hardware-reset-cli --help
```

`reset` 等待设备完成脉冲并释放；`assert` 仅确认已拉低，随后关闭端口可能立即释放，不适合要求完整保持时长的调用。写命令不自动重试，超时必须作为结果未知处理。

Windows/Linux 按 USB `0483:5740` 找候选，再通过 HELLO/INFO 验证产品及序列号；macOS 使用 `/dev/cu.usbmodem*` 候选及同样的协议验证，列表中的 VID/PID 为空。可用 `--port` 显式指定端口，仍会验证产品及协议。Linux 需当前用户具有设备节点访问权限。

串口使用系统 API 加合作文件锁，避免多个本程序进程共享端口。文件锁依赖内核锁，进程退出后自动释放；临时目录中的锁文件可保留。设备枚举及打开预算为 5 秒，每条协议请求默认 2 秒，可通过 `--timeout-ms` 调整。串口接收缓冲及响应行限制为 256 字节。

目录：`cmd/hardware-reset-cli/` 为入口，`internal/transport/` 实现串口与发现，`internal/protocol/` 实现 V1 编解码与编号匹配，`internal/app/` 实现命令、验证、结果及退出码。详细接口见 [CLI 约定](../docs/cli.md) 和 [USB 协议](../docs/usb-protocol.md)。

构建版本可通过 `go build -ldflags "-X main.version=0.1.0" ...` 注入。依赖锁定于 `go.mod` / `go.sum`：go.bug.st/serial、gofrs/flock 及其间接依赖，均保留上游许可。
