# CLI（待实现）

Go 命令行程序的接口见 [CLI 输入输出方案](../docs/cli.md)，线缆上传输格式见 [USB 协议](../docs/usb-protocol.md)。目前没有 `go.mod` 或可执行代码。

建议实现结构：`cmd/hardware-reset-cli/` 放入口，`internal/transport/` 处理串口与超时，`internal/protocol/` 处理编解码及编号匹配，`internal/device/` 处理发现、握手与命令，`internal/output/` 统一 JSON 和退出码。串口库选定后再提交依赖文件。
