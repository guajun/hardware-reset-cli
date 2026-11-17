# USB Class 选型

状态：首版草案，采用 CDC ACM（虚拟串口）。

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
| 产品字符串 | 当前 `STM32 Virtual ComPort`；计划改为 `Hardware Reset Controller` |
| 序列号 | 当前由 ST 描述符代码生成；实现时固定为 MCU UID 对应的稳定唯一字符串，并验证多板不重复 |

CLI 先按已配置的 VID/PID 找候选设备，再通过 `HELLO` 的产品标识 `HWRESET`、协议版本与 `INFO` 验证。不能仅凭 ST 示例 VID/PID 将任意串口认定为本产品。候选仅发送只读探测命令；用户可用 `--port` 明确指定端口。

## CDC 控制请求

- CLI 打开串口使用 115200、8N1、无硬件流控。该设置只用于串口 API 兼容；没有 UART 转发，不决定 USB 传输速率。
- 固件实现 `SET_LINE_CODING` / `GET_LINE_CODING`，默认 115200、8N1，保存并返回合法的 7 字节结构；不支持的请求按 USB 栈规则处理，不能返回未初始化内存。
- `SET_CONTROL_LINE_STATE` 保存 DTR/RTS；DTR/RTS 变化本身不得触发 GPIO 复位。CLI 正常通讯时设置 DTR，关闭时清除；本功能不依赖 1200 baud 等 bootloader 约定。
- 主机退出、拔线、挂起时按总体方案释放或定时释放，不把 CDC 行控制信号直接接到外部 RESET。

## 兼容性与发现

`list` 为每个已验证设备提供端口、USB 序列号、VID/PID、固件及协议版本；忙、无权限或探测失败的候选标注原因，不能伪装成有效设备。普通命令只在唯一匹配时自动选择，多设备必须指定。Linux 权限错误应提示用户检查设备节点访问权限，不要求程序默认以 root 运行。

参考：[Microsoft Usbser.sys 驱动与匹配规则](https://learn.microsoft.com/en-us/windows-hardware/drivers/usbcon/usb-driver-installation-based-on-compatible-ids)、[Windows 支持的 USB 类](https://learn.microsoft.com/en-us/windows-hardware/drivers/usbcon/supported-usb-classes)。端点及现有描述符值以仓库中的 `usbd_cdc.h`、`usbd_cdc.c`、`usbd_desc.c` 为依据。
