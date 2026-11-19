# LiveMix

桌面端视频剪辑应用，UI 使用 Qt 开发。

## Qt 模块使用约束（强制）

以下 Qt 模块**禁止**在本项目中引入或使用：不得出现在构建配置（CMakeLists.txt / .pro / qmake 的 QT 变量）、`find_package`、`.ui` 文件、QML 导入或 `#include` 中。确需使用前必须先征求用户明确同意。

| 模块 | 用途 | 禁止原因 / 替代方案 |
| --- | --- | --- |
| Qt Canvas Painter | Canvas 绘图 | 避免使用 |
| Qt CoAP | CoAP 物联网协议 | 避免，UI 通常不需要 |
| Qt Graphs | 数据图表与可视化 | 避免，除非确实需要 |
| Qt GRPC | gRPC 通信支持 | 避免，可评估其他 gRPC 实现 |
| Qt HTTP Server | 内嵌 HTTP 服务端 | 避免，可使用独立 HTTP 服务库 |
| Qt Lottie Animation | Lottie 动画 | 避免，可评估其他动画方案 |
| Qt MQTT | MQTT 消息协议 | 避免，通常与剪辑 UI 无关 |
| Qt Network Authorization | 网络授权 | 避免 |
| Qt Qml Compiler | QML 编译 | 避免，使用 QML 时尤其要注意 |
| Qt Quick 3D | QML 3D 渲染 | 避免，普通视频时间轴不需要 |
| Qt Quick 3D Physics | 3D 物理模拟 | 避免 |
| Qt Quick Timeline | QML 动画时间线 | 避免，可自行实现剪辑时间轴 |
| Qt Virtual Keyboard | 虚拟键盘 | 避免，桌面端一般不需要 |
| Qt Wayland Compositor | Wayland 合成器 | 避免，Windows/macOS 桌面 UI 通常不需要 |

### 实施要求

- 新增依赖前先检查该模块是否在上表中；命中则改用替代方案，或停下来向用户确认。
- 不要在代码中写「暂时用一下」的违规引入或注释掉的违规依赖。
- 代码审查 / 依赖变更时，将命中上表视为阻断项。
