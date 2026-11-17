# 第三方组件

自有代码尚未选定许可证。以下许可只适用于对应上游组件，原始文本和文件头保持有效。

## Go CLI 依赖

| 组件 | 版本 | 许可文本 |
| --- | --- | --- |
| go.bug.st/serial | v1.6.4 | [BSD 3-Clause](third_party/licenses/serial-LICENSE.txt) |
| github.com/gofrs/flock | v0.12.1 | [BSD 3-Clause](third_party/licenses/flock-LICENSE.txt) |
| github.com/creack/goselect | v0.1.2 | [MIT](third_party/licenses/goselect-LICENSE.txt) |
| golang.org/x/sys | v0.22.0 | [BSD 3-Clause](third_party/licenses/x-sys-LICENSE.txt) |

文本从 `go.sum` 锁定版本的 Go 模块缓存原样复制，CLI 分发包附带这些文本。Go 标准库运行时代码遵循工具链的 BSD 风格许可，附 [Go LICENSE](third_party/licenses/go-LICENSE.txt)。

## 固件组件

STM32Cube HAL/LL、CMSIS、USB Device 中间件及启动/链接文件保留原有许可证与版权头。见 [STM32 HAL LICENSE](firmware/Drivers/STM32F1xx_HAL_Driver/LICENSE.txt)、[CMSIS LICENSE](firmware/Drivers/CMSIS/LICENSE.txt) 和对应源码文件头。固件打包脚本收集 Drivers/Middlewares 内现存许可文件。

本地验证使用的 Zig、clang-format、SEGGER J-Link 和 Arm GNU 工具链不作为项目安装包的一部分分发。
