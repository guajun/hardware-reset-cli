# USB Class 选型

状态：首版实现约定，采用 CDC ACM（虚拟串口）。

## 选型

| 方案 | 主机接口 | 对本项目的影响 |
| --- | --- | --- |
| CDC ACM（采用） | 系统串口 API | 复用现有 CubeMX CDC，方便串口工具调试和 Go 实现 |
| HID | HID report API | 需要定义 report descriptor 和分片规则，当前复位指令无此需要 |
| Vendor-specific bulk | WinUSB / libusb 等 | 需要额外处理驱动绑定和分发，留待出现更复杂传输需求时评估 |

Microsoft 文档说明，匹配 CDC class/subclass 的设备可加载系统 Usbser.sys；最终需用本工程描述符在 Windows 10/11 实测。CDC ACM 在 Linux、macOS 的目标使用方式分别为 `/dev/ttyACM*`、`/dev/cu.usbmodem*`，设备名不可作为稳定身份，驱动和权限作为联调验收内容。

## 描述符与端点

复用当前 ST CDC 实现，首版不增加复合设备或其他接口。

| 项目 | 当前值 / 设计要求 |
| --- | --- |
| 速率 | USB Full Speed |
| 控制接口 | Communication Class `0x02`，ACM Subclass `0x02` |
| 数据接口 | CDC Data Class `0x0A` |
| EP0 | 标准枚举和 CDC 控制请求 |
| `0x01` OUT | 主机到设备，bulk |
| `0x81` IN | 设备到主机，bulk |
| `0x82` IN | CDC notification，interrupt；不承载应用命令 |
| 数据包上限 | FS bulk 64 字节；与应用行边界无关 |
| 当前 VID/PID | `0x0483:0x5740`，CubeMX 示例配置，不作为项目专属身份 |
| 产品字符串 | `Hardware Reset Controller`，在 USER CODE 区域覆盖生成默认值 |
| 序列号 | MCU 完整 96 位 UID，24 位大写十六进制；与 INFO 一致 |

CLI 先按已配置的 VID/PID 找候选设备，再通过 `HELLO` 的产品标识 `HWRESET`、协议版本与 `INFO` 验证。不能仅凭 ST 示例 VID/PID 将任意串口认定为本产品。候选仅发送只读探测命令；用户可用 `--port` 明确指定端口。

## CDC 控制请求

- CLI 打开串口使用 115200、8N1、无硬件流控。该设置只用于串口 API 兼容；没有 UART 转发，不决定 USB 传输速率。
- 固件实现 `SET_LINE_CODING` / `GET_LINE_CODING`，默认 115200、8N1，保存并返回合法的 7 字节结构；不支持的请求按 USB 栈规则处理，不能返回未初始化内存。
- `SET_CONTROL_LINE_STATE` 接受控制状态并追踪 DTR，不使用 RTS。DTR 上升和 RTS 变化不触发 GPIO 复位；DTR 下降清理会话并释放。CLI 正常通讯时设置 DTR，关闭时清除；不依赖 1200 baud 等 bootloader 约定。
- 主机退出、拔线、挂起时按总体方案释放或定时释放，不把 CDC 行控制信号直接接到外部 RESET。

## 兼容性与发现

`list` 为每个已验证设备提供端口、USB 序列号、VID/PID、固件及协议版本；忙、无权限或探测失败的候选标注原因，不能伪装成有效设备。普通命令只在唯一匹配时自动选择，多设备必须指定。Linux 权限错误应提示用户检查设备节点访问权限，不要求程序默认以 root 运行。

macOS 为保持无需 cgo，枚举 `/dev/cu.usbmodem*` 并验证协议，列表 VID/PID 为空；Windows/Linux 使用详细 USB 枚举，仅探测开发 VID/PID 候选。显式 `--port` 可绕过候选过滤，但仍验证协议。

ST CDC 中间件 `usbd_cdc.c` 已补充 class request 的固定缓冲长度检查、空上下文检查，并使应用控制请求失败正确 stall；更新中间件时必须保留或合并这些修复。

Blue Pill 固定 D+ 上拉场景下，固件在启动 USB 前将 PA12 开漏拉低 100 ms，再恢复浮空输入，以便主机重新检测连接。这是启动阶段等待，PA0–PA7 已在此前释放，不影响正常主循环的非阻塞调度。

参考：[Microsoft Usbser.sys 驱动与匹配规则](https://learn.microsoft.com/en-us/windows-hardware/drivers/usbcon/usb-driver-installation-based-on-compatible-ids)、[Windows 支持的 USB 类](https://learn.microsoft.com/en-us/windows-hardware/drivers/usbcon/supported-usb-classes)。端点及现有描述符值以仓库中的 `usbd_cdc.h`、`usbd_cdc.c`、`usbd_desc.c` 为依据。
