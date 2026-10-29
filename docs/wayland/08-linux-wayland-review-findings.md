# Linux/Wayland 审查：未解决项

- 更新日期：2026-10-29
- 分支：`docs/linux-render-refresh-flicker`
- 最新完整审查：[09-current-implementation-review.md](09-current-implementation-review.md)
- 目标架构：[01-architecture-overview.md](01-architecture-overview.md)

## 1. 文档说明

本文原先记录的 19 项问题均已有对应修复提交，因此已从当前未解决清单中移除。原问题与修复提交的对应关系保留在最新审查文档第 3 节，不再把已修复路径描述为当前缺陷。

“已有源码修复”不等于“真实 Linux/Wayland 环境验收完成”。

## 2. 当前未解决的代码问题

### 1. 高：Show 重新映射缺少 configure，clean frame 还会破坏背压状态

当前 `Hide` 通过 nil buffer unmap 后，`Show` 直接调用 `renderFrame`，缺少 xdg-shell 要求的 empty commit → configure → ACK → buffer commit 重新映射序列。即使强制 present，也不能绕过该协议门限。

同时，`renderFrame` 在确定 clean frame 不提交 buffer 前就建立 `framePending`，可能让后续 Waker 永久等待一个没有对应 commit 的 callback。部分 resize/scale/rebuild 路径存在相同的 clean/forced present 混淆。

- 位置：`internal/platform/linux/window_wayland.go:263`、`:289`、`:750`、`:774`、`:961`
- 详细分析：最新审查 Finding 1

### 2. 高：旧 swapchain 在创建失败时被重复销毁

`vkCreateSwapchainKHR` 失败分支显式销毁 `oldSwapchain`，deferred cleanup 随后再次销毁同一个 handle。该共享 Vulkan 路径同时影响 Wayland 和 X11。

- 位置：`internal/platform/linux/vulkan_linux.go:670`、`:742`
- 详细分析：最新审查 Finding 2

### 3. 中：eventfd 写入未处理 EINTR

命令已经入队但 eventfd 写入在写入前被信号打断时，主线程可能继续阻塞在 `poll(-1)`，使命令失去唯一唤醒。

- 位置：`internal/platform/linux/wayland_loop.go:179`
- 详细分析：最新审查 Finding 3

### 4. 中：装饰和最大化路径吞掉 frame-margin 错误

多处 `_ = renderer.setFrameMargin(...)` 使 Vulkan 失败后窗口状态仍继续推进，尚未进入统一应用错误通道。

- 位置：`internal/platform/linux/window_wayland.go:373`、`:379`、`:543`、`:552`
- 详细分析：最新审查 Finding 4

### 5. 中：SUBOPTIMAL 标志只记录、不消费

acquire 或 present 返回 `VK_SUBOPTIMAL_KHR` 后仅设置 `swapchainSuboptimal=true`，没有恢复路径读取它。若之后没有独立 resize 或 OUT_OF_DATE，交换链可以永久保持不匹配状态。

- 位置：`internal/platform/linux/vulkan_linux.go:961`、`:991`、`:1019`、`:1038`
- 详细分析：最新审查 Finding 5

### 6. 高：首次 display roundtrip 错误被吞掉

初始 empty commit 后忽略 `displayRoundtrip` 返回值，可能在没有有效初始 configure 时继续创建 renderer、present 和 commit，同时丢失显示/协议错误根因。

- 位置：`internal/platform/linux/window_wayland.go:911`、`:1004`
- 详细分析：最新审查 Finding 6

## 3. 架构与生命周期差距

### 7. 托盘/后台常驻契约尚未实现

`Run` 仍以 `hasWindows()` 为循环条件，只有托盘项而没有窗口的应用无法按目标架构持续处理 eventfd 和 D-Bus 事件。

当前 `01` 已明确这是目标能力而非现有保证。下一步需要先决定 keep-alive 产品契约，再统一实现、测试和退出语义。

### 8. 内部架构边界仍需收敛

宏观技术路线可以保留，但以下内部抽象尚不完整：

- scene dirty 与 surface commit/present 分离；
- 正确的 newly-unmapped 重新 configure 状态机；
- 集中 serial、ACK 和 commit 所有权；
- 统一错误传播与主线程状态所有权；
- 带完整 revents/error 的 event-source 接口；
- Vulkan 原生资源的唯一销毁所有权和 SUBOPTIMAL 恢复。

改造方向和业界实践见目标架构文档及最新审查第 1.1 节。

## 4. 验证缺口

- 当前 CI 没有实际执行 `internal/platform/linux` 测试，只做交叉检查；
- 现有 Hide/Show 测试未覆盖重新 configure 和 clean frame；
- 现有 swapchain 测试未覆盖旧 handle 创建失败时的销毁次数，以及 SUBOPTIMAL 后续恢复；
- 首次 roundtrip 失败和“未收到 configure 不得 present”缺少回归测试；
- 尚未完成真实 compositor、Vulkan GPU、多屏缩放、IBus、拖放和 AT-SPI 验收。

详细修复顺序和验证记录见 [09-current-implementation-review.md](09-current-implementation-review.md)。
