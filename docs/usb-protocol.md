# USB 通讯协议 v1 草案

本文件是待实现的应用层约定，运行在 CDC ACM bulk 字节流上。首版只支持协议主版本 1；所有写操作必须先完成 HELLO。USB 包、串口 read/write 的边界不等于应用帧边界。

## 帧、字段与边界

```text
请求：V1 <id> <COMMAND> [arg ...]\n
成功：V1 <id> OK <COMMAND> [result ...]\n
失败：V1 <id> ERR <CODE>\n
```

- ASCII、大小写敏感，以单个空格分隔 token，不允许引号、转义、空 token、前后空格；发送 LF，接收可接受紧邻 LF 的一个 CR（CRLF）。无回显、无 banner、无异步日志。
- `id` 为 1–4294967295 的十进制整数，无符号、无前导零；0 保留给无法识别请求编号的错误。响应原样携带请求编号，CLI 同时校验 id 和成功响应的 COMMAND。
- 数字参数为无符号十进制；允许值 0 时写 `0`，其他数字无前导零。所有整数先检查溢出再转换。命令名、pin、枚举值严格按文档白名单匹配。
- 每行最多 256 字节，包含 LF（或 CRLF），响应同限；解析缓冲另留 C 字符串结尾空间。超长行丢弃到下一个 LF，返回 `V1 0 ERR LINE_TOO_LONG`，不执行截断内容。
- 接收半行自首字节起 1000 ms 内必须完整，否则丢弃已收内容并进入丢弃状态至下一 LF；可返回 `V1 0 ERR FRAME_TIMEOUT`。主机新建会话先发送一个 LF 作为重同步分隔符，空行忽略；这不撤销已经执行的请求。
- 对版本不支持但结构可识别的请求，返回当前格式的 `V1 <id> ERR UNSUPPORTED_VERSION`；其他非法帧返回 `BAD_FRAME`，编号无法识别时用 0。错误帧绝不改变 GPIO。
- 主机通常只有一个未完成请求；允许在 RESET 等待期间发送 STATUS 或 RELEASE，每帧单独分配 id，响应可能交错，必须按编号匹配。

v1 不增加应用层 CRC，依赖 USB 传输校验，并用严格语法、长度及范围检查拒绝错误输入；不提供身份认证或远程传输安全。未来若增加其他传输媒介再评估校验与认证。

## 握手与命令

连接后发送 LF，再发送 `HELLO 1`。HELLO 只验证协议，不触发 GPIO 或重置请求编号窗口；成功后才允许写操作。INFO 和 STATUS 也允许在握手前用于诊断。每次 USB 重新配置后重新握手；串口打开不保证触发设备侧 USB 重配置。

| 请求正文（省略 V1 与 id） | 成功响应正文（省略 V1 与 id） | 语义 |
| --- | --- | --- |
| `HELLO 1` | `OK HELLO HWRESET 1` | 验证产品与协议主版本 |
| `INFO` | `OK INFO <fw> <serial> 1 <max_ms> <pins>` | fw 和 serial 均为 1–32 个 ASCII 字母/数字/点/短横线/下划线；max_ms=10000；pins 为逗号分隔列表 |
| `STATUS <pin>` | `OK STATUS <pin> <state> <remaining_ms>` | state 为 ASSERTED 或 RELEASED；释放时 remaining_ms=0 |
| `RESET <pin> <duration_ms>` | `OK RESET <pin> RELEASED <duration_ms>` | 从已释放状态拉低、设备计时、释放后返回；duration 1–10000 |
| `ASSERT <pin> <hold_ms>` | `OK ASSERT <pin> ASSERTED <hold_ms>` | 从已释放状态拉低并启动租约后立即响应；hold 1–10000 |
| `RELEASE <pin>` | `OK RELEASE <pin> RELEASED` | 立即释放，已释放也成功；可取消执行中的 RESET |

`INFO` 的引脚列表首版固定为 `PA0,PA1,PA2,PA3,PA4,PA5,PA6,PA7`，未来匹配固件可扩展，整行仍不得超过上限。`status` 查询全部引脚由 CLI 按 INFO 列表逐个执行 STATUS；读回的是控制状态，无物理电平测量。INFO 中 serial 必须与 USB 描述符一致。

执行 RESET 或 ASSERT 前，若已有写操作活动则返回 BUSY；对已拉低引脚不得叠加或延长脉冲。ASSERT 的租约到期只释放，不发异步通知。RESET 只在结束后返回一次最终响应，不返回提前成功的 ACK。

```text
> V1 101 HELLO 1
< V1 101 OK HELLO HWRESET 1
> V1 102 INFO
< V1 102 OK INFO 0.1.0 EXAMPLE 1 10000 PA0,PA1,PA2,PA3,PA4,PA5,PA6,PA7
> V1 103 RESET PA0 100
  （设备拉低，100 ms 后释放）
< V1 103 OK RESET PA0 RELEASED 100
> V1 104 STATUS PA0
< V1 104 OK STATUS PA0 RELEASED 0
```

若 RESET 103 执行期间收到 `V1 104 RELEASE PA0`，先释放，排队输出 `V1 103 ERR CANCELLED` 再输出 `V1 104 OK RELEASE PA0 RELEASED`。释放其他 pin 不取消当前 RESET。响应写不出去也不得推迟释放。

## 重复请求、超时与会话

为避免长脉冲中的重复执行，设备记录当前执行中请求和最近 8 个已完成请求（包括原始规范请求行及最终响应）；每个连接的编号不能复用。完全相同 id 和内容命中缓存时重放最终响应；命中执行中请求时不重复动作，原请求结束后输出一次响应。相同 id 不同内容返回 `ID_CONFLICT`。

缓存是有界的：已淘汰编号再次出现无法保证去重。因此 CLI 对写操作一律不自动重发，并使用系统随机源生成起始 id，单次会话递增，耗尽时关闭重连并重新选择编号。跨进程或 USB 重枚举没有 exactly-once 保证；随机编号只是降低碰撞概率，不能用于证明请求未执行。USB 总线复位或断开时清除握手、队列和缓存，释放引脚。DTR 下降可清理会话并取消写操作，但不能假定每个系统必然产生该事件。

GPIO 改变后如果最终响应丢失，CLI 必须报告 outcome unknown。重新 STATUS 只能说明现在的输出状态，不能推断历史脉冲是否发生。该语义也适用于部分写入、主机超时和取消确认丢失。

## 错误码

| CODE | 含义 |
| --- | --- |
| `BAD_FRAME` | 格式/字符/字段数量非法 |
| `LINE_TOO_LONG` / `FRAME_TIMEOUT` | 行超长 / 半帧过期，需重新同步 |
| `UNSUPPORTED_VERSION` | 帧版本或 HELLO 版本不支持 |
| `HANDSHAKE_REQUIRED` | 写操作前未完成 HELLO |
| `UNKNOWN_COMMAND` | 未知命令 |
| `INVALID_PIN` | 不在固件白名单 |
| `INVALID_ARGUMENT` | 数值格式、溢出或范围错误 |
| `BUSY` | 活动写操作或命令队列无空位；本请求未开始 |
| `ID_CONFLICT` | 缓存或活动请求中编号与不同内容冲突 |
| `CANCELLED` | 操作被 RELEASE 或可检测的会话关闭取消 |
| `INTERNAL` | 内部错误，先释放控制引脚，再尝试返回错误 |

完整格式正确但拒绝的请求最多返回一个错误响应；畸形帧错误可使用 id 0，主机把其视为本次通讯失败，不应无限等待匹配编号。

## 固件实现约束

- USB RX 回调只将数据复制到有界队列，随后重新挂接接收；必须在复用 USB 缓冲前完成复制。解析和 GPIO 调度在主循环完成，不在 USB 回调里等待脉冲或 TX。
- 建议 RX 环形队列 1024 字节、TX 完整帧队列 4×256 字节、最多 2 条待调度命令；结合 8 项去重缓存与 ST USB 缓冲估算总 RAM，按实际链接 map 验证。空间不足不能截断或覆盖尚未处理的请求。
- RX 队列满时进入丢弃模式至下一 LF，报告 `BAD_FRAME`；重新恢复前不能执行被截断帧。若还能完整解析新请求而命令槽已满，返回 BUSY。
- 写操作之前预留其最终响应槽；RELEASE 另保留一个应急响应槽，允许释放逻辑绕过普通队列，不能让 TX 拥塞阻止释放。发送拥塞到无法报告错误时关闭会话并释放，不继续接受写操作。
- `CDC_Transmit_FS` 返回 BUSY 时保留队首重试；确认已配置且 pClassData 非空后才访问 TxState。发送缓冲必须保持到 USB 发送完成，不能引用已失效栈变量。处理长度恰为 64 的倍数的 USB 传输结束条件。
- 使用单调毫秒时钟进行非阻塞期限比较，正确处理 tick 回绕；脉冲计时优先于解析和 TX。不能用 HAL_Delay 阻塞整个命令处理循环。

## 联调验收用例

覆盖逐字节输入、多个帧一次到达、CRLF、64 字节边界、256 字节边界及超长后恢复；非法 pin/数字/版本均不改变 GPIO；相同请求重复到达不重复拉低；不同内容同编号被拒绝；并发写返回 BUSY；RESET 完成、取消及 ASSERT 到期均释放；TX busy、接收溢出、USB 断线和主机崩溃不会无限拉低。用逻辑分析仪核对脉宽和意外脉冲，使用模拟传输验证 CLI JSON 与退出码。
