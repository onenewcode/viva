# Viva Linux 现代显示与渲染架构技术规范（Wayland-First）

本文档集面向 **Viva** GUI 框架核心架构师与平台层开发者，系统性规划并定义 Viva 在 Linux 平台向 **“Wayland 原生优先（Wayland-First）、X11 弹性后备（X11-Fallback）”** 演进的完整技术与架构规范。

---

## 1. 规范背景与战略意义

Viva 作为以“Go 优先、GPU 原生、无 WebView、零多余捆绑、直通操作系统底座”为核心特性的跨平台桌面框架，在 Linux 平台的初始实现采用了 X11/XCB 驱动。然而，随着现代 Linux 桌面生态的代际更替：

1. **上游生态全面去 X11 化**：Fedora 41+、RHEL 10、Ubuntu 等主流发行版，以及 GNOME 46+、KDE Plasma 6、Cosmic 等主流桌面环境已全面将 Wayland 列为唯一或绝对首选会话，X11 已经进入冻结与废弃周期；
2. **根治渲染架构顽疾**：X11 的异步解耦模型缺乏原子性提交机制，导致窗口在 Live Resize 与高频交互时产生严重的黑白频闪与撕裂（详见 [docs/linux-rendering-flicker-analysis.md](file:///Volumes/HIKSEMI/code/viva/docs/linux-rendering-flicker-analysis.md)）；
3. **架构现代化升级**：Wayland 的“Every frame is perfect”理念、原子提交机制（Atomic Commit）、点对点分数缩放（Fractional Scaling v1）以及显式同步（Explicit Sync），能够让 Viva 在 Linux 上获得与 macOS Metal 及 Windows D3D11 齐平的极致平滑与原生表现。

因此，Viva 确立了 **“Wayland 原生作为第一公民，X11 作为兼容后备”** 的核心战略。

---

## 2. 文档集结构与导航

本技术规范按关注点分离原则，划分为 7 篇深度技术文档：

| 序号 | 规范文档 | 核心内容概要 | 验收清单覆盖 |
| :---: | :--- | :--- | :--- |
| **01** | [01-architecture-overview.md](01-architecture-overview.md) | **现代化架构总览与多后端调度机制**<br>分层拓扑架构、环境探测器（selectBackend）、纯 Go 零 CGO 契约、事件循环整合。 | 架构拓扑分层规范、多后端调度优先级契约 |
| **02** | [02-vulkan-surface-and-rendering-pipeline.md](02-vulkan-surface-and-rendering-pipeline.md) | **Vulkan 渲染与表面解耦架构设计**<br>`vulkanSurfaceProvider` 抽象接口、`VK_KHR_wayland_surface` 集成、Mailbox 呈现丢弃过时帧、原子提交彻底终结频闪。 | **§6 渲染管道与表面验收清单**<br>ABI 大小、旧链时序、Suboptimal 幂等性、零频闪与背压对齐 |
| **03** | [03-protocol-and-window-management.md](03-protocol-and-window-management.md) | **Wayland 核心协议与窗口生命周期设计**<br>PureGo 动态绑定 `libwayland-client`、XDG-Shell (`xdg_wm_base`) 状态机、多窗口管理、CSD/SSD 装饰协商。 | **§6 协议与窗口生命周期验收清单**<br>免 CGO 构建、ack_configure 应答、三窗口模式、多窗口并发安全 |
| **04** | [04-input-scale-and-desktop-integration.md](04-input-scale-and-desktop-integration.md) | **输入、分数缩放与桌面生态集成设计**<br>`wl_seat` 与 XKB 键盘路由、`wp-fractional-scale-v1` 矢量级高分屏、剪贴板/拖放、现代 IME 与 D-Bus 共享桥接。 | **§5 输入、缩放与桌面集成验收清单**<br>亚像素指针、平滑滚动、125%~175% 点对点锐利度、D-Bus 读屏与主题 |
| **05** | [05-implementation-roadmap-and-migration.md](05-implementation-roadmap-and-migration.md) | **实施路线图、测试套件与迁移规划**<br>5 阶段代码改造清单、单元测试与 Conformance 测试适配方案、主流混成器兼容性矩阵。 | **全流程 5 阶段详细验收清单**<br>单测命令、回归覆盖、Conformance 场景全绿、多混成器实测 |
| **06** | [06-wayland-window-interaction-and-styling-diagnostics.md](06-wayland-window-interaction-and-styling-diagnostics.md) | **Wayland 窗口拖动误位移与样式对齐定位与修复**<br>Viewport 尺寸生命周期同步、拖拽滞后阈值（Drag Slop）、边框阴影判定校准、KDE/GNOME 原生窗口按钮自适应。 | 窗口拖动、边缘缩放与系统外观对齐验收清单 |
| **07** | [07-wayland-interactive-resize-and-border-hit-testing-specification.md](07-wayland-interactive-resize-and-border-hit-testing-specification.md) | **Wayland 交互式缩放抖动与延迟优化及 CSD 边框识别规范**<br>Top/Left 逆向缩放原点漂移根因、原子配置回包状态机、非阻塞 Mailbox 呈现与帧节流消除实时缩放延迟、对齐 GTK 4/Qt 6 的全空间 CSD 边框与 24px 角部命中判定算法。 | 逆向平滑缩放、实时缩放 0 延迟、单次原子 Commit、24px 角部判定验收清单 |

---

## 3. 核心设计准则

在实施 Wayland 架构改造的全过程中，必须恪守以下四项铁律：

1. **文档与规范完整性（Documentation Integrity）**：所有架构改动必须严格对应接口契约与 ABI 规格说明；
2. **零 CGO 约束（Pure Go Runtime Dynamic Linking）**：延续 Viva Linux 端全部基于 `github.com/ebitengine/purego` 动态加载系统 `.so` 的优良传统，不引入任何 gcc/cgo 编译依赖；
3. **无感知平滑降级（Transparent Fallback）**：在不支持 Wayland 的环境下，透明降级至现有的 XCB/X11 管线，对上层应用代码保持 100% 接口兼容；
4. **极致性能与无撕裂（Every Frame is Perfect）**：充分利用 Wayland 的原子状态提交与垂直同步时钟，彻底消除窗口缩放黑闪与中间过渡残影。

---

## 4. 实施阶段与当前进展

详细任务拆解与自动化验收清单请参阅 [05-implementation-roadmap-and-migration.md](05-implementation-roadmap-and-migration.md)。

| 阶段 | 核心任务 | 状态 | 验收与 Review |
| :---: | :--- | :---: | :---: |
| **Phase 1** | Vulkan 表面解耦与交换链平滑过渡 | 已完成 | PASS (Subagent Review 已通过) |
| **Phase 2** | PureGo Wayland 动态加载器与协议基座 | 已完成 | PASS (Subagent Review 已通过) |
| **Phase 3** | XDG-Shell 窗口控制器与首帧渲染 | 已完成 | PASS (Subagent Review 已通过) |
| **Phase 4** | 完整输入、分数缩放与光标支持 | 已完成 | PASS (Subagent Review 已通过) |
| **Phase 5** | 调度器整合、自动化后备与 Conformance 验证 | 已完成 | PASS (Subagent Review 已通过) |

