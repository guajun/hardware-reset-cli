# Hardware reset CLI

通过 Go CLI 与 STM32F103C8T6 Blue Pill 通讯，控制外部开发板的复位信号，让脚本和 AI harness 无需人工按复位键。

## 当前状态

首版软件已实现：USB CDC ACM、V1 通讯协议、Go CLI、非阻塞 GPIO 定时控制，以及 PC13 1.25 Hz 心跳。主机集成测试直接让 Go CLI 与真实 C 协议核心交互。构建、测试及板上验收见 [验证记录](docs/validation.md)。

## 快速使用

直接安装 Release 中的 Go 编译二进制，**不需要安装 Go**。Windows PowerShell 一行安装：

```powershell
irm https://github.com/guajun/hardware-reset-cli/releases/latest/download/install.ps1 | iex
```

Linux / macOS 一行安装：

```sh
curl -fsSL https://github.com/guajun/hardware-reset-cli/releases/latest/download/install.sh | sh
```

安装器按操作系统及 amd64/arm64 架构下载对应压缩包，核对 SHA-256 后安装到用户目录。也可从 [Releases](https://github.com/guajun/hardware-reset-cli/releases/latest) 手动下载。二进制、skill 和控制器固件分别安装，CLI 安装器不会烧录硬件。

从源码安装需要 Go 1.23 或更新版本：

```sh
go install github.com/guajun/hardware-reset-cli/cli/cmd/hardware-reset-cli@latest
```

连接已烧录固件的 Blue Pill 后使用：

```sh
hardware-reset-cli list --json
hardware-reset-cli reset --port COM5 --pin PA0 --duration-ms 100 --json
hardware-reset-cli status --serial YOUR_DEVICE_SERIAL --json
```

`COM5` 替换为 `list` 返回的端口，Linux 示例为 `/dev/ttyACM0`，macOS 为 `/dev/cu.usbmodem*`。只有一个已验证设备时可省略设备参数；多设备必须指定端口或序列号。

命令包括 `list`、`info`、`status`、`reset`、`assert`、`release`、`version`。不带 `--json` 时输出可读结果，带 `--json` 时 stdout 只有一个完整 JSON 对象；日志和诊断写 stderr。

`reset` 等设备完成脉冲并释放后才成功，默认拉低 100 ms，范围 1–10000 ms。超时或断线可能意味着复位已发生，CLI 报告 `outcome: unknown`，不会自动重试。GPIO 时序完成不保证目标系统已经启动。

`assert` 只确认有期限的拉低，CLI 退出关闭串口可能立即释放；需要固定长度脉冲时使用 `reset`。

## 安装 Agent Skill

使用带 `gh skill` 支持的 GitHub CLI 安装；该命令安装使用说明，不安装二进制：

```sh
gh skill install guajun/hardware-reset-cli hardware-reset-cli --agent codex --scope user
```

Claude Code / GitHub Copilot 分别将 `--agent` 改为 `claude-code` / `github-copilot`。skill 发布于 [skills/hardware-reset-cli/SKILL.md](skills/hardware-reset-cli/SKILL.md)，包含设备选择、GPIO 接线确认、复位时序、JSON/退出码与断线处理指引。需要固定版本时使用 `hardware-reset-cli@v0.1.0`。

## 没接板子时

可以明确报告错误。`list --json` 在未发现控制器时成功返回 `devices: []`，退出码 0；需要设备的 `info`、`status`、`reset` 等命令返回退出码 **3**，并输出：

```json
{"schema_version":1,"ok":false,"command":"status","device":null,"data":null,"error":{"code":"DEVICE_NOT_FOUND","message":"...","outcome":"not_started"}}
```

指定不存在、被占用或无权限的 `--port` 返回退出码 5 / `IO_ERROR`。请求已经发出后再断线返回退出码 9 / `DISCONNECTED`，`outcome` 为 `unknown`；超时为退出码 6。程序不会崩溃，也不会自动重复复位。脚本使用退出码与 `error.code` 判断，不匹配 message 文案。

## 设计方案

- [总体方案与实施顺序](docs/design.md)
- [USB Class 选型](docs/usb-class.md)：CDC ACM 虚拟串口。
- [CLI 输入输出](docs/cli.md)：命令、设备选择、JSON 输出和退出码。
- [USB 通讯协议 v1](docs/usb-protocol.md)：ASCII 行协议、请求编号、错误和超时。

首版支持固件白名单中的 PA0–PA7，以开漏方式控制低有效复位：拉低表示复位，释放表示由目标板上拉恢复。其他引脚需匹配固件和接线后扩展。

```text
AI harness / shell
        | argv / stdout JSON / exit code
        v
Go CLI ---- USB CDC ACM / ASCII lines ---- Blue Pill ---- GPIO ---- target RESET
```

## 目录

```text
cli/          Go CLI、模拟传输及真实 Go/C 集成测试
docs/         接口约定与验证记录
firmware/     CubeMX 工程、HAL/CMSIS、USB Device 中间件、CMake 配置
scripts/      Windows 构建与跨平台打包脚本
```

## 构建与测试

Windows 安装 Go、CMake、Ninja、Arm GNU Toolchain 和原生 C 编译器后，在仓库根目录执行：

```powershell
.\scripts\build.ps1 -Configuration All
python scripts/package.py --configuration Release
```

构建脚本自动发现常见安装路径，只修改进程环境，不修改机器或用户 PATH。测试需要原生 C 编译器，可通过 `-NativeCompiler` 指定。本工作区使用局部安装的 Zig；其他 Windows 环境可选择 clang/GCC，或执行 `python -m pip install --target .cache/tooling ziglang`。跳过测试须显式使用 `-SkipTests`。

从 `cli/` 单独执行：

```sh
go test ./...
go vet ./...
go build -trimpath -o ../dist/hardware-reset-cli ./cmd/hardware-reset-cli
```

Windows 输出文件加 `.exe`。原生 C 测试从仓库根目录执行：

```sh
cmake -S firmware/tests -B build/native
cmake --build build/native
ctest --test-dir build/native --output-on-failure
```

Go/C 集成测试读取 `HWRESET_SIM` 环境变量，值为原生构建生成的 `reset_protocol_sim` 绝对路径（Windows 加 `.exe`）。未设置时跳过集成测试；Windows 构建脚本和 CI 均会设置并执行。

需安装 ARM GNU Toolchain（`arm-none-eabi-gcc`）、CMake 和 Ninja。在 `firmware/` 下执行已有 preset：

```sh
cmake --preset Debug
cmake --build --preset Debug
```

也可选择 `Release` preset。固件输出目录为 `firmware/build/Debug/` 或 `Release/`，包含 ELF、BIN、HEX、MAP。应用 Flash 起始地址为 `0x08000000`；ELF/HEX 自带地址，BIN 需烧录器指定地址。打包输出在 `dist/packages/`，附 SHA-256 校验和与构建信息。

CI 在 Windows/Linux/macOS 测试和构建 CLI，在 Ubuntu 运行 Go/C 集成测试、竞态检查及 ARM Debug/Release 构建，可在仓库 Actions 下载产物。修改引脚初始化时应同步 `.ioc`，自定义接入点放在 `USER CODE` 区域，协议与调度放在独立模块。

仓库已加入 `.agents/skills/jlink-cli/`。该通用 skill 的默认目标为 STM32H750VB，本项目使用时必须显式指定 `STM32F103C8`，不能直接沿用默认芯片或示例中的 H750 参数。

## 接线约束

Blue Pill 与目标板共地；选择 PA0–PA7 中一路接低有效 RESET，使用目标板匹配的外部上拉。接线前确认复位脚电压、极性及兼容性，必要时使用电平转换或隔离。USB、SWD、晶振与 LED 引脚不能通过协议访问。

设备最多同时执行一个拉低操作，脉冲/租约由 MCU 计时。`RELEASE` 可取消正在执行的复位；USB 总线复位、断开及检测到 DTR 下降时释放并清理会话。定时保障依赖 MCU 正常运行，LED 心跳不是独立硬件看门狗。

Windows/Linux 按开发 VID/PID `0483:5740` 找候选，再通过 HELLO/INFO 验证产品与完整 96 位 UID。该 VID/PID 为 ST 示例配置，不是项目专属分配。macOS 无需 cgo，按 `cu.usbmodem` 找候选并握手，列表 VID/PID 为空。Linux 需当前用户具有串口节点访问权限。

PC13 使用 CubeMX 的 `LED_Pin` / `LED_GPIO_Port` 标签，每 400 ms 翻转一次，完整亮灭周期为 800 ms（1.25 Hz，50% 占空比），主循环使用 `HAL_GetTick()` 计时，不调用阻塞延时。该 LED 为运行心跳，不表示 USB 已连接或复位命令已成功。

第三方组件保留各自许可文件及文件头，详见 [第三方说明](THIRD_PARTY_NOTICES.md)。本项目自有代码的许可证尚未选定，仓库公有状态不代表统一授权。
