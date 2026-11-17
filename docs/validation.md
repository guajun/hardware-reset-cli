# 验证记录与板上验收

本文件区分自动化软件验证与实际电气测量。软件测试可重复运行，不能替代外部 RESET 脉宽、上拉及电压确认。

## 自动化验证

2026-11-17 首版实现的验证入口：

- `firmware/tests/` 编译真实 `reset_protocol.c`，覆盖握手、所有分包位置、粘包、CRLF、行长度边界、NUL、超长恢复、数字溢出、非法引脚、重复请求、编号冲突、BUSY、取消、到期释放、tick 回绕、RX overflow 及 TX 满时释放。
- `cli/internal/protocol/` 测试严格帧解析、部分写入、超时不重试与响应匹配。
- `cli/internal/app/` 测试命令、JSON、退出码、选择歧义、设备错误、参数错误与取消；设置 `HWRESET_SIM` 后，`simulator_test.go` 运行真实 Go/C 集成测试，覆盖全部设备命令及取消复位。
- `scripts/build.ps1 -Configuration All` 执行原生 C 测试、Go 测试/vet、CLI 构建及 ARM Debug/Release。
- GitHub Actions 在 Windows/Linux/macOS 测试 CLI，Ubuntu 的 Go/C 集成任务额外启用 `-race`，并编译两种 ARM 配置。

Windows 本地工具链：Go 1.27.4（模块要求至少 1.23）、Arm GNU 14.2.rel1、CMake 4.4.4、Ninja、Zig 原生 C 编译器。

## 板上验收步骤

1. 使用明确匹配的探针烧录 F103C8，复位运行；观察 PC13 LED 心跳。
2. 执行 list、info、status；确认 UID 与 USB 描述符一致，八路均为 released。
3. 执行 `reset --port ... --pin PA0 --duration-ms 100 --json`；确认设备完成后才返回，随后查询释放。
4. 用保持串口的协议客户端测试 ASSERT、STATUS、RELEASE，验证 BUSY、租约到期及 RESET 取消。
5. 拉低期间关闭串口，验证 DTR 下降释放；重新握手查询，确认会话可恢复。
6. 拉低期间 USB 总线复位/拔线、主机进程退出，验证输出释放；Blue Pill 若随 USB 掉电，电平由目标侧上拉决定。
7. 用示波器或逻辑分析仪测量 LED 周期及 RESET 脉宽，检查上电、重枚举、断线无多余脉冲；确认目标板真正复位和启动。

STATUS 是固件控制状态，不是 GPIO 实测电平；串口交互通过不代表已测量电气波形。

## 当前板上记录

2026-11-17，通过用户确认的 J-Link STLink `773736562` 烧录并校验 Debug 固件，复位运行。最终 Debug ELF SHA-256：`593760ADE1E449A3DA05B2168784883FD540197CB8C237E51D98A4EC782EAE76`。

实机结果：

- Windows 正常枚举为 USB 串行设备 COM5，VID/PID `0483:5740`，UID `0672FF515656857187054312`；CLI 验证产品 HWRESET、固件 0.1.0、协议 1。
- list/info/status 均通过，八路初始 released；`reset --pin PA0 --duration-ms 100` 返回 completed=true，后续状态为 released。
- `scripts/hardware-smoke.ps1 -Port COM5 -Pin PA0` 通过：ASSERT 与重复响应、STATUS、BUSY、RELEASE、RESET 取消顺序、租约到期、非法引脚、超长帧恢复、DTR 下降释放及重新握手。
- 自动唯一设备选择可执行复位；`--serial` 连续 12 次查询通过。针对 Windows 探测关闭后立即重开可能忙的问题，改为复用唯一匹配的已验证 session，并加入回归测试。
- SWD 读取 GPIOA 配置 CRL=`0x66666666`（PA0–PA7 开漏输出），ODR 低 8 位为 `0xFF`（释放）。
- SWD 确认 RCC 主时钟配置和递增的 HAL tick；实际 LED 周期与外部 RESET 波形尚未用测量仪器确认。Linux/macOS 的物理 USB 插拔和权限仍需对应主机验证。

首次烧录后 Windows 报设备描述符请求失败；加入启动时 PA12 开漏拉低 100 ms、再恢复浮空并启动 USB 后，重新枚举恢复正常。该等待仅在启动发生，正常协议和 LED 调度保持非阻塞。

当前固件容量：Debug Flash 38,716 B，Release Flash 19,192 B，RAM 15,032 B（含 2 KB 栈保留）；限额分别为 64 KB / 20 KB。

上电/拔线的完整波形、实物 LED 2.5 Hz 的精度，以及外部目标板真正完成复位和启动，仍需接线后用示波器或逻辑分析仪验收。已完成的串口及寄存器验证不替代这些电气测量。
