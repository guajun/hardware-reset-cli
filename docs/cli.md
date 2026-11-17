# CLI 输入输出方案

状态：首版实现约定。可执行文件名 `hardware-reset-cli`；一次调用完成一次操作，不读取 stdin，不进入交互式菜单，不弹确认提示。Go 最低版本 1.23，无需 cgo。

## 命令与参数

```text
hardware-reset-cli list [--json]
hardware-reset-cli info [设备参数] [--json]
hardware-reset-cli status [设备参数] [--pin PA0] [--json]
hardware-reset-cli reset [设备参数] --pin PA0 [--duration-ms 100] [--json]
hardware-reset-cli assert [设备参数] --pin PA0 [--hold-ms 1000] [--json]
hardware-reset-cli release [设备参数] --pin PA0 [--json]
hardware-reset-cli version [--json]
hardware-reset-cli --help
```

| 参数 | 约定 |
| --- | --- |
| `--port` | 显式端口，如 `COM5`、`/dev/ttyACM0`；仍须握手验证 |
| `--serial` | USB 序列号精确匹配；与 `--port` 互斥 |
| `--pin` | `PA0`–`PA7`，大小写规范化后按设备能力校验；写命令必填 |
| `--duration-ms` | `reset` 的拉低时间，整数 1–10000，默认 100 |
| `--hold-ms` | `assert` 的租约时间，整数 1–10000，默认 1000 |
| `--timeout-ms` | 单次协议请求的总等待期限，默认 2000，整数 1–60000；不含打开端口和设备枚举 |
| `--json` | stdout 输出一个 UTF-8 JSON 对象和换行；格式不因 TTY 改变 |
| `--verbose` | 诊断信息仅写 stderr |

解析和本地范围校验在打开串口前完成。`reset` 需要 timeout ≥ duration + 500 ms；不足时返回参数错误，不静默修改用户值。`assert` 返回确认后可退出，但通常关闭端口会使设备提前释放；其用途是为保持串口打开的协议客户端提供有期限的拉低。需要稳定保持指定时长的 CLI 调用应使用 `reset`。

`status` 不传 pin 时查询全部白名单引脚。`list` 无设备时成功返回空数组；其他命令无匹配时报错。多设备不默认选第一个。`list` 中无法探测的候选单独放入 `unavailable` 数组；系统枚举本身失败则命令失败。

## stdout、stderr 与 JSON

文本模式 stdout 只打印结果，例如 `reset PA0 completed (100 ms)`；失败说明和日志写 stderr。JSON 模式成功或失败均只输出一个完整对象，不能混入进度条、启动 banner、串口日志或颜色码。帮助文本不受此约束；`--help` 成功退出。

JSON 顶层字段固定为 `schema_version`、`ok`、`command`、`device`、`data`、`error`；无适用内容用 `null`。`schema_version` 是 CLI 输出版本，独立于 USB 协议版本。`device` 包含 `port`、`serial`，`data` 为命令特定结构；未知附加字段可忽略。数值为 JSON number，GPIO 状态只使用 `asserted` / `released`。

```json
{"schema_version":1,"ok":true,"command":"reset","device":{"port":"COM5","serial":"EXAMPLE"},"data":{"pin":"PA0","duration_ms":100,"state":"released","completed":true},"error":null}
```

```json
{"schema_version":1,"ok":false,"command":"reset","device":{"port":"COM5","serial":"EXAMPLE"},"data":null,"error":{"code":"TIMEOUT","message":"No final response before deadline","outcome":"unknown"}}
```

`error` 固定包含 `code`、`message`、`outcome`，其中 outcome 为 `not_started`（确定未发送或设备已拒绝）、`unknown`（已尝试发送但未确认）或 `cancelled`（设备确认已取消）。即使部分写入失败也不能报告确定未执行。脚本按 `code` 和退出码分支，不解析 message。JSON 错误可在 stderr 附带可读诊断，但 stdout 仍是唯一机器结果。

| 命令 | 成功 data 内容 |
| --- | --- |
| `list` | `devices` 数组（每项含 port、serial、vid、pid、firmware、protocol），`unavailable` 数组（port、reason） |
| `info` | firmware、protocol、pins 数组、max_duration_ms；protocol 为整数，VID/PID 为四位十六进制字符串 |
| `status` | `pins` 数组，每项含 pin、state、remaining_ms |
| `reset` | pin、duration_ms、state、completed；仅收到设备最终 OK 才成功 |
| `assert` | pin、hold_ms、state、remaining_ms；表示已确认拉低，不保证端口关闭后持续保持 |
| `release` | pin、state |
| `version` | cli_version、supported_protocols 数组；不访问 USB |

## 退出码

| 码 | 含义 / 典型错误 code |
| --- | --- |
| 0 | 成功，包括 `list` 空结果 |
| 2 | 参数错误：`INVALID_ARGUMENT` |
| 3 | 无匹配设备：`DEVICE_NOT_FOUND` |
| 4 | 多设备歧义：`AMBIGUOUS_DEVICE` |
| 5 | 权限、端口占用、打开失败：`IO_ERROR` |
| 6 | 超时：`TIMEOUT` |
| 7 | 协议格式、产品或版本不兼容：`PROTOCOL_ERROR` |
| 8 | 设备拒绝：保留协议错误码，如 `BUSY`、`INVALID_PIN` |
| 9 | 执行中断线或传输失败：`DISCONNECTED` / `TRANSPORT_ERROR` |
| 130 | 用户中断：`INTERRUPTED` |

## 超时、取消和并发

一次调用打开设备、握手、验证能力、执行请求、读取最终响应并关闭。端口打开/枚举采用独立的 5000 ms 预算（实现须为可能阻塞的系统调用设置边界）；每个候选握手使用 timeout。禁止写命令自动重试；超时/断线后返回 unknown，用户可重新查询状态，但当前状态无法证明是否已经发生过复位。

CLI 对目标端口使用系统 API 和基于内核锁的合作文件锁，避免多个本程序进程共享端口；其他程序是否尊重独占取决于系统。Ctrl+C 时若端口仍可用，尽力发 `RELEASE` 并最多等待 500 ms，然后退出 130；只有原请求返回 CANCELLED 且 RELEASE 成功时 outcome 为 cancelled，否则 unknown。若确定发送前已取消则为 not_started。设备定时释放作为 MCU 正常运行时的最终保障。

Windows/Linux 候选为 USB `0483:5740`；macOS 候选为 `/dev/cu.usbmodem*`，不读取 USB VID/PID（列表字段为空）。所有候选均通过 HELLO/INFO 验证产品、协议及稳定序列号。

```powershell
hardware-reset-cli list --json
hardware-reset-cli reset --port COM5 --pin PA0 --duration-ms 100 --json
if ($LASTEXITCODE -ne 0) { throw 'Hardware reset failed or outcome is unknown' }
```
