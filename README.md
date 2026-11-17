# Hardware reset CLI

通过 Go CLI 与 STM32F103C8T6 Blue Pill 通讯，控制外部开发板的复位信号，让脚本和 AI harness 无需人工按复位键。

## 当前状态

项目处于方案草拟阶段。`firmware/` 是 STM32CubeMX 生成的 USB CDC 固件骨架；`cli/` 尚未实现。下列命令和协议均为设计目标，当前固件尚不能执行复位命令。

## 设计方案

- [总体方案与实施顺序](docs/design.md)
- [USB Class 选型](docs/usb-class.md)：CDC ACM 虚拟串口。
- [CLI 输入输出](docs/cli.md)：命令、设备选择、JSON 输出和退出码。
- [USB 通讯协议 v1 草案](docs/usb-protocol.md)：ASCII 行协议、请求编号、错误和超时。

首版支持固件白名单中的 PA0–PA7，以开漏方式控制低有效复位：拉低表示复位，释放表示由目标板上拉恢复。其他引脚需匹配固件和接线后扩展。

```text
AI harness / shell
        | argv / stdout JSON / exit code
        v
Go CLI ---- USB CDC ACM / ASCII lines ---- Blue Pill ---- GPIO ---- target RESET
```

## 目录

```text
cli/          Go CLI（待实现）
docs/         设计草案
firmware/     CubeMX 工程、HAL/CMSIS、USB Device 中间件、CMake 配置
```

## 固件构建

需安装 ARM GNU Toolchain（`arm-none-eabi-gcc`）、CMake 和 Ninja。在 `firmware/` 下执行已有 preset：

```sh
cmake --preset Debug
cmake --build --preset Debug
```

构建目录为 `firmware/build/Debug/`。当前只提交工程骨架与方案，尚未进行硬件联调；构建成功也不表示协议已实现。修改引脚初始化时应同步修改 `.ioc`，自定义代码优先放在 `USER CODE` 区域或独立模块。

## 接线约束

Blue Pill 与目标板共地；选择固件允许的 GPIO 接目标 RESET，使用目标板匹配的外部上拉。接线前确认目标复位脚的电压、极性及电气兼容性。当前生成代码仍为推挽低电平，必须完成方案中的开漏及启动释放改造后再用于复位控制。

第三方 HAL、CMSIS 和 USB 中间件保留各自已有许可文件及文件头；本项目自有代码的许可证尚未选定。
