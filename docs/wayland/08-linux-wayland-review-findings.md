# Linux/Wayland 审查：未解决项

- 更新日期：2026-10-29
- 分支：`docs/linux-render-refresh-flicker`
- 修复进度：[task.md](task.md)
- 完整审查记录：[09-current-implementation-review.md](09-current-implementation-review.md)
- 目标架构：[01-architecture-overview.md](01-architecture-overview.md)

## 1. 文档说明

本文只保留当前仍未完成的架构、产品契约和验证事项。已经实现并通过独立 review 的修复会移出未解决清单，不再把历史 findings 当作当前缺陷。

“源码修复和 review 通过”不等于“真实 Linux/Wayland 环境验收完成”。

## 2. 本轮代码 findings 已修复

前次详细审查的六项代码问题已全部进入 `task.md` 并分别完成实现、回归测试、独立 review 和提交：

1. Show 重新 configure、clean/forced present、geometry 顺序和 frame callback 配对/失败清理；
2. oldSwapchain 创建失败时重复销毁；
3. eventfd 写入 EINTR 丢失唤醒；
4. decoration/maximize 的 frame-margin 错误吞掉；
5. SUBOPTIMAL 只记录、不恢复；
6. 首次 display roundtrip 错误和初始 configure 门限。

各任务的具体提交见 `task.md` 和 `git log`。上述条目不再属于当前未解决代码 findings。

## 3. 架构与生命周期差距

### 1. 托盘/后台常驻契约尚未实现

`Run` 仍以 `hasWindows()` 为循环条件，只有托盘项而没有窗口的应用无法按目标架构持续处理 eventfd 和 D-Bus 事件。

当前 `01` 已明确这是目标能力而非现有保证。下一步需要先决定 keep-alive 产品契约，再统一实现、测试和退出语义。

### 2. 长期内部架构改造尚未全部完成

本轮修复保留了宏观技术路线，并收敛了呈现事务和关键资源生命周期；以下长期目标仍未全部实施：

- libwayland prepare/read 完整读取流程；
- 主循环 timer/repeat 统一调度；
- 带完整 revents/error 的通用 event-source 接口；
- 更完整的 surface/configure 显式状态机；
- X11/Wayland 可共享的桌面服务重连生命周期；
- 原生协议元数据/代码生成及进一步的资源所有权收敛。

这些属于后续架构任务，不应与本轮已经修复的确定性缺陷混为一项。

## 4. 验证缺口

- 当前 CI 没有实际执行 `internal/platform/linux` 测试，只做交叉检查；
- 本机没有 Linux 运行环境、Docker 或 QEMU，新增 Linux 测试目前只完成交叉编译和静态 review；
- 尚未完成真实 compositor 的首次映射、Hide/Show、resize/scale、隐藏竞态和断开验收；
- 尚未在真实 Vulkan 驱动上验证 oldSwapchain、SUBOPTIMAL、持续 resize 和故障清理；
- 多屏缩放、IBus、拖放和 AT-SPI 仍需真实桌面环境验收；
- 需要让 Linux mock/unit 测试进入 CI，并建立 headless compositor、validation/lavapipe 和真实桌面分层验证。

详细审查证据和验证记录见 [09-current-implementation-review.md](09-current-implementation-review.md)。
