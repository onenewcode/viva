# Linux/Wayland 实现真实性与架构合理性复核

- 日期：2026-10-29
- 审查基准：HEAD `3293e74b02e7ffb143351f6f3380392a423d3488`
- 适用分支：`docs/linux-render-refresh-flicker`
- 目标架构：[01-architecture-overview.md](01-architecture-overview.md)
- 范围：Wayland 后端选择、事件循环、surface/configure/呈现、输入/IME、缩放、共享 Vulkan WSI、测试与验收文档
- 类型：只读源码与文档审查；本报告不修改实现代码

> 证据口径：“已实现”只表示当前 HEAD 存在对应代码路径；“有回归测试”只表示测试源码存在。除非明确写出运行环境和结果，不表示测试已经执行，也不表示真实 compositor/GPU 验收通过。下列 findings 均由源码控制流确认，未宣称已在真实 Linux 环境复现其可见症状。

## 1. 结论

### 1.1 是否真的实现

**不是只有设计文档。** 原生 Wayland 后端、PureGo 绑定、eventfd 命令队列、Wayland/Vulkan surface、输入与缩放主路径，以及本轮六项修复对应的代码和回归测试都真实存在。

但不能据此认定目标架构或六项修复已经完整闭环。本次新确认：

- **4 个高严重度正确性问题**：直接 present 绕过 remap 事务、frame pending 期间 resize/scale 丢失 redraw、服务关闭后残留 FD 忙循环、eventfd `post/close` 复用 FD 竞态；
- **5 个中严重度问题**：SUBOPTIMAL 新交换链不强制呈现、Wayland IBus 超时缺失、repeat 无按键代际、scale 往返截断、fractional-scale 与 viewporter 组合错误；
- **1 个低严重度资源释放问题**：正常关闭遗漏 IBus `Bus.Close`；
- **1 处跨文档矛盾**：`docs/conformance.md` 仍声称原生 Wayland 未实现。

因此，`01` 第 9 节中“clean/forced present 已实现”“frame callback 与 commit 配对已实现”“SUBOPTIMAL 恢复已修复”等表述过强，应改为**主路径已落地，但仍有本文列出的入口和组合场景未闭合**。

### 1.2 架构是否真的合理

**宏观方向合理，不需要推倒重写；当前内部实现仍是“主路径事务 + legacy 直接 present + 分散资源所有权”的混合架构，尚不足以支撑生产可靠性结论。**

应保留的主干：

1. Wayland 原生优先、X11 后备，强制后端失败时不静默降级；
2. 统一上层平台契约，共享场景、输入路由与 Vulkan 核心；
3. PureGo 动态绑定，保留零 CGO 和交叉编译能力；
4. 单一主循环配合 eventfd 命令队列；
5. 将场景 dirty 与 surface commit 分离，以 configure、frame callback 和 WSI 状态驱动呈现。

当前最关键的架构问题不是缺少更多布尔守卫，而是：

- 并非所有呈现都经过同一个窗口事务；
- FD、eventfd 和连接的关闭所有权不统一；
- frame callback、resize 和 pending redraw 没有统一代际/事务语义；
- timer、IME 超时和 repeat 尚未纳入主循环调度；
- 多组布尔状态仍代替完整 surface/configure 状态机。

### 1.3 是否可以宣称验收完成

**不能。** 当前证据支持“核心实现与针对性回归测试已落地”，不支持“Wayland 全链路已在 Linux、真实 compositor 和真实 Vulkan 驱动上验收”。本文还确认了现有单元测试未覆盖的确定性控制流缺陷。

## 2. 高严重度 findings

### 2.1 H1：focus-loss 和普通输入 present 可绕过 hidden/remap 门限

**证据**

- `internal/platform/linux/window_wayland.go:384-399`：activation 丢失时直接调用 `state.router.present`。
- `internal/platform/linux/wayland_keyboard.go:107-127`：keyboard leave 也直接调用同一通道。
- `internal/platform/linux/input_linux.go:264-270`：普通 pointer/key/scroll 等输入通过 `router.present` 呈现。
- `internal/platform/linux/window_wayland.go:1055-1060`：该通道在 `changed=true` 时直接调用 `renderer.present`，不经过 `renderFrame`、configure ACK 或 `presentWithFrameCallback`。
- `internal/platform/linux/window_wayland.go:271-277` 的 hidden/awaitingConfigure/framePending 门限只在主 render 路径；`Show` 在 `:844-850` 设置的 remap 等待无法约束上述直接入口。

**失败场景**

已激活窗口 Hide 后 Show，remap 的 toplevel configure 不含 activated，并先于 `xdg_surface.configure` 被处理。若 `OnFocusLost` 返回 dirty frame，代码会在新 serial ACK 前进行 Vulkan/WSI 呈现，可能向 newly-unmapped surface 提前提交 buffer。Hide 后收到 focus-loss 也存在同类风险。

**架构结论**

remap 修复在主 render/resize 路径有效，但并非全局有效；“所有实际 commit 都与 frame callback 配对”目前不成立。真实 compositor 是报协议错误、意外 remap 还是表现出其他时序症状，仍需集成测试验证。

### 2.2 H2：frame pending 期间的 resize/scale 可吞掉 redraw，且新 commit 没有新 callback

**证据**

- `internal/platform/linux/window_wayland.go:275-277`、`:1109-1114`：旧帧 pending 时，render/Waker 只保留 `needsRedraw`。
- `internal/platform/linux/window_wayland.go:435-443`：`applyPendingResize` 不检查 `framePending`，并无条件清除 `needsRedraw`。
- `internal/platform/linux/window_wayland.go:495-506`、`:529-536`：`OnResize == nil` 时只呈现已有场景，不调用 `OnRedraw`；成功后 `:553` 又清除 `forcePresent`。
- `internal/platform/linux/wayland_frame.go:33-35`：pending 时 `requestFrameCallback` 直接返回，但 `:55-58` 仍执行 present，所以新 commit 没有配对的新 callback。
- 旧 callback 到达后，`internal/platform/linux/wayland_frame.go:78-80` 看到两个 redraw 标记均为 false，不再重绘。

**失败场景**

旧帧 pending → Waker 请求内容更新 → compositor 发来实际尺寸变化且窗口没有 `OnResize` → resize 呈现旧场景并清除标记 → 已排队内容更新滞留，直到另一次外部唤醒。即使配置了 `OnResize`，pending 期间直接 commit 而没有新 callback 仍表明背压事务未统一。

现有 `internal/platform/linux/wayland_window_test.go:758-807` 覆盖 present 失败，不覆盖这一成功路径。

### 2.3 H3：IBus/AT-SPI 先关闭 FD 后无法移除，可能形成 POLLNVAL 忙循环

**IBus 控制流**

1. `internal/platform/linux/wayland_app.go:457-470` 先 `Dispatch`，再用 `app.ibus.FD()` 移除 FD。
2. 接收失败时，`internal/ibus/ibus.go:270-278` 进入 `fail`；`:226-232` 先关闭连接。
3. `internal/dbus/conn.go:196-200` 关闭后把 FD 设为 `-1`。
4. `internal/platform/linux/wayland_loop.go:163-166` 对负 FD 直接返回，旧 FD 留在 `pollFDs`。
5. 额外 FD 只检查 `POLLIN`（`internal/platform/linux/wayland_loop.go:322-328`），不处理 `POLLNVAL`。

daemon 断开 socket 后，下一轮 `poll(-1)` 可对已关闭 FD 立即返回 `POLLNVAL`；代码既不分发也不清理，于是持续唤醒并形成 CPU 忙循环。

AT-SPI 也存在相同的“服务先关闭、移除时已经拿不到旧 FD”模式：`internal/platform/linux/atspi_bridge.go:514-522`、`:96-101` 与 `internal/platform/linux/atspi_linux.go:449-456`。

**架构结论**

这不是 prepare/read 缺失导致的，而是 event-source 注册身份与连接关闭所有权不一致。仅在正常路径“先 remove 再 close”不能覆盖异常路径。

### 2.4 H4：eventfd `post/close` 存在写入已复用 FD 的竞态

**证据**

- `internal/platform/linux/wayland_loop.go:183-196`：`post` 在 `cmdMu` 内保存 `wakeFD`，解锁后才执行写系统调用。
- `internal/platform/linux/wayland_loop.go:372-385`：`close` 可把同一 FD 设为 `-1` 并关闭，但不等待已经解锁的 writer。
- `internal/platform/linux/wayland_app.go:127-132`、`:549-559`：`app.loop` 的读写没有统一同步；loop 为 nil 时还会在调用 goroutine 直接执行命令。

**失败场景**

后台命令保存 FD → 主线程结束并 close → 其他线程打开的新资源复用该整数 → 后台向新资源写入 8 字节。即使 FD 尚未复用，写错误也被忽略。该竞态与已经修复的 write EINTR 是不同问题，现有 EINTR 回归测试不能覆盖它。

## 3. 中低严重度 findings

### 3.1 M1：SUBOPTIMAL 重建后没有在新交换链上强制呈现

- `internal/platform/linux/vulkan_linux.go:988-1002`：当前帧成功后，若 `swapchainSuboptimal`，调用 `recreateSwapchain()` 随即返回 `nil`，不会再次 acquire/submit/present。
- `internal/platform/linux/vulkan_linux.go:1016-1049`：acquire/present 返回 SUBOPTIMAL 时完成当前帧并设置标志，这部分符合 WSI 语义。
- `internal/platform/linux/api_linux_test.go:704-731`：测试明确断言 `submissions == 1`，把“旧交换链呈现一次、重建后不呈现”固定为预期。
- `docs/wayland/01-architecture-overview.md:249-253` 则要求 swapchain 重建后即使 clean scene 也必须重新呈现已有场景。

新交换链在下一次外部 redraw/resize/scale 前没有被呈现，实现与文档不能同时成立。是否出现旧尺寸帧、空白或闪烁取决于真实 WSI/compositor，本次未实机复现。

### 3.2 M2：Wayland 的 IBus 两秒超时实际不会触发

- `internal/ibus/ibus.go:348-362` 为按键记录 deadline，只有 `Expire`（`:315-329`）会释放未答按键并关闭 bus。
- Wayland 固定 `step(-1)`（`internal/platform/linux/wayland_app.go:176`），IBus 分发仅调用 `Dispatch/checkInputMethod`（`:457-461`），没有接入 `Deadline/Expire`。
- X11 已在 `internal/platform/linux/window_linux.go:223-235`、`:417-425` 接入对应超时机制。

当 daemon hang 但不断开 socket 时，已发给 IME 的按键可无限等待，不会按既有设计回退为直接输入。

### 3.3 M3：repeat 没有按键代际，旧命令可混入同键的新按压

- `internal/platform/linux/wayland_keyboard.go:343-400` 使用 timer/ticker goroutine 产生并排布 repeat。
- 排队后的执行检查 `:403-410` 只比较 scancode 和焦点窗口，没有 press generation。

若旧 repeat 已入队，同一批后续输入处理“release → 同 scancode press”，下一轮旧命令会看到相同 scancode/焦点并通过校验，产生一次不属于新按压代的 repeat。销毁保护存在，不能把该问题误写为“销毁后必然访问已释放窗口”。

### 3.4 M4：fractional scale 往返截断逻辑尺寸，产生一像素漂移

- `internal/platform/linux/wayland_scale.go:236-248` 先由设备尺寸反算浮点逻辑尺寸，随后用 `int(logicalW/H)` 截断。
- `:280-285` 把截断值写入 pending 尺寸，最终由 `internal/platform/linux/window_wayland.go:451-456` 固化到保存的逻辑尺寸。

确定示例：401 logical px 从 1.0 到 1.25 得到 501 device px；回到 1.0 时反算为 400.8，当前代码截成 400，窗口由 401 缩为 400。现有 `internal/platform/linux/wayland_window_test.go:1769-1842` 使用恰好整除的 400 宽度，没有覆盖该情况。

### 3.5 M5：只有 fractional-scale manager、没有 viewporter 时表面尺寸错误

- `internal/platform/linux/wayland_scale.go:145-158` 独立创建 fractional-scale 和 viewport，允许 `fractionalScale != 0 && viewport == 0`。
- 此组合下 `:246-262` 按分数 scale 分配 buffer，`:274-275` 却设置 `ceil(scale)` 的整数 buffer scale，且没有 viewport destination 恢复逻辑尺寸。

例如 scale=1.25 时，buffer 宽为 1.25W、buffer scale 为 2，实际 surface 宽成为 0.625W，而 geometry 仍按 W 计算（`internal/platform/linux/window_wayland.go:195-213`）。这是有效协议全局组合下的确定性尺寸错误。

### 3.6 L1：正常关闭遗漏 IBus 连接释放

`internal/platform/linux/wayland_app.go:532-537` 只移除 FD 并将 `app.ibus=nil`，未调用 `Bus.Close`；真正关闭 socket、释放 pending keys 的代码位于 `internal/ibus/ibus.go:221-239`。

进程直接退出时由 OS 兜底，但在同进程重复运行/测试或需要保留其他服务连接时，会泄漏 FD，pending key 也不会走正常释放路径。

## 4. 文档与实现状态复核

### 4.1 当前状态矩阵

| 能力 | 复核状态 | 证据边界 |
| :--- | :--- | :--- |
| Wayland-First、X11 override 与自动后备 | 代码路径已实现 | `internal/platform/linux/backend_select.go:18-51`；强制 Wayland 不降级，X11 选择不等于已连接 X server |
| PureGo 动态绑定 | 已实现 | libwayland、libc、Vulkan 通过 purego 绑定；Linux/amd64 零 CGO 交叉编译通过 |
| 共享 Vulkan 与 eventRouter | 已实现主干 | 跨后端复用存在，不代表协议时序已等价验收 |
| eventfd 基础队列与 EINTR | **部分实现** | 先入队后唤醒、EINTR 重试已落实；`post/close` 竞态见 H4 |
| flush POLLOUT 背压恢复 | 该路径已实现 | 额外 FD 的 ERR/HUP/NVAL 和关闭所有权未统一，见 H3 |
| prepare/read 标准流程 | 未完整实现 | 当前是 dispatchPending + poll + displayDispatch；该差距本身不等于已证明当前协议错误 |
| 主循环 timer/键盘 repeat | 未实现目标架构 | 独立 goroutine 仍存在，且有代际问题 M3；IBus deadline 也未接入 M2 |
| 初始 configure 与 Hide/Show remap | **主路径实现** | 初始/remap 测试存在；直接 present 可绕过门限 H1 |
| scene update 与 present/commit 分离 | **主路径实现** | clean/forced 和失败事务测试存在；legacy 直接 present 未统一 H1 |
| frame callback 与 commit 配对 | **未全局实现** | 主 render 路径有配对和失败清理；直接输入及 pending resize 可绕过 H1/H2 |
| frame-margin 错误传播 | 已实现并有回归测试 | `TestWaylandFrameMarginErrorsStopTransitions` |
| oldSwapchain 唯一销毁 | 已实现并有回归测试 | `internal/platform/linux/vulkan_linux.go:666-677`、`:1186-1193`；成功/失败路径均有测试 |
| OUT_OF_DATE 重建后重试 | 代码路径已实现 | `internal/platform/linux/vulkan_linux.go:988-1008`；acquire/present 行为测试仍不足 |
| SUBOPTIMAL 恢复 | **部分实现** | 当前帧完成和重建已落实；新交换链强制呈现未落实 M1 |
| fractional/整数缩放 | **主路径存在，仍有正确性缺口** | 往返截断 M4、缺少 viewporter 的组合 M5 |
| 桌面协议核心共享 | 部分实现 | 协议核心可共享，FD、重连和显示连接生命周期仍有分叉 H3 |
| 托盘/后台常驻 | 未完整实现 | `Run` 仍以 `hasWindows()` 为条件，不能宣称完整 keep-alive |
| Linux 平台测试进入 CI | 未完成 | `.github/workflows/ci.yml:19-55` 只做 Linux vet；`:71-81` 的 Ubuntu 测试不包含 `internal/platform/linux` |
| 真实 Wayland/Vulkan 验收 | 未完成 | mock/offscreen 测试不能替代 compositor、WSI、validation layer 和桌面矩阵 |

### 4.2 六项修复应如何解读

| 修复 | 提交与测试证据 | 本次复核结论 |
| :--- | :--- | :--- |
| eventfd 写入 EINTR | `dd07f2c`；`TestWaylandLoopPostWakeErrors` | 该修复真实存在，但不覆盖 H4 关闭竞态 |
| oldSwapchain 唯一销毁 | `b8c4189`；成功/失败销毁计数测试 | 控制流符合唯一销毁目标 |
| SUBOPTIMAL 当前帧后重建 | `2f978a9`；两个 SUBOPTIMAL 测试 | 重建已实现，强制呈现未闭合 M1 |
| frame-margin 错误传播 | `de1e05f`；`TestWaylandFrameMarginErrorsStopTransitions` | 已在相关 configure/decoration 路径传播 |
| remap configure 序列 | `8fcd62a`；初始/remap/关闭/隐藏 resize 测试 | 主路径有效，入口覆盖不完整 H1 |
| scene/present 与回调事务 | `e5f8700`；clean/forced、resize/scale、失败事务测试 | 拆分方向正确，但 H1/H2 证明尚未形成全局事务 |

因此，正确表述是：**六项均有实际代码工作和针对性测试，不等于六项都已完整满足架构契约，也不等于已在真实环境验收。**

### 4.3 全仓库验收文档仍有直接矛盾

`docs/conformance.md:72-80` 把 “Native Wayland” 列在 “Not done” 下，并声称 “Only XCB is spoken; a Wayland session runs through XWayland”。这与当前源码及 `docs/wayland` 直接矛盾。

应改为“原生 Wayland 主路径已实现，但尚未纳入完整 conformance/实机矩阵”，并补充 Wayland 专用场景；不要用继续声称“未实现”来表达“尚未验收”。

## 5. 架构合理性分项判断

### 5.1 后端与共享边界：合理

后端选择集中，Wayland/X11 专属协议与共享渲染、输入核心的划分符合维护需求。桌面集成采用“协议核心共享、FD 与连接生命周期由后端适配”的边界也合理，不应追求不真实的 100% 共享。

### 5.2 事件循环：方向合理，所有权契约不合格

eventfd + 命令队列适合跨 goroutine 唤醒，flush POLLOUT 背压也符合非阻塞 socket 模型。但 H3/H4 表明，FD 注册身份、writer 生命周期和关闭顺序必须先修复，再讨论接口美化。

prepare/read 应按 libwayland 契约补齐；同时应区分“目标流程未实现”和“当前已经发生协议错误”，没有真实时序证据时不应混为一谈。

### 5.3 Surface/configure/呈现：关键拆分正确，但尚未单一化

将场景更新与强制提交分离是本轮最重要的架构修正。clean scene 不再自动等于无需 commit，能够覆盖 Show、resize、scale 和资源重建等非内容驱动呈现。

H1/H2 说明当前仍是“主路径事务 + legacy 直接 present”的混合结构。继续增加布尔特判不能替代统一入口；所有 input/focus/redraw/resize/configure 都应进入同一个窗口呈现协调器，并明确 pending frame、queued redraw 和 configure 代际。

### 5.4 Vulkan WSI：恢复策略合理，SUBOPTIMAL 契约需补齐

当前帧完成后再处理 SUBOPTIMAL、OUT_OF_DATE 有限次重试、device/surface loss 进入恢复，以及 oldSwapchain 唯一销毁，都是合理边界。问题是 SUBOPTIMAL 重建后的强制呈现没有闭合，而不是整体恢复路线错误。

### 5.5 验证架构：mock 有价值，但当前覆盖与执行都不足

当前 fake 能覆盖许多手工难以稳定注入的错误，适合保护控制流和资源计数。本文 findings 也说明，仅记录回调次数或只覆盖主 render 路径，会把旁路入口和状态组合留在测试之外。

mock 不能证明真实 compositor 的 Hide/Show、configure 合并和不可见时序，也不能证明新 swapchain 的可见结果、GNOME/KDE/Sway 的差异、多屏/IME/AT-SPI 的端到端行为或实际性能。

## 6. 本地验证记录

审查环境：Darwin/arm64，Go `go1.26.4`；没有可用的 Linux、Docker、Podman 或 QEMU 运行环境。

以下命令成功：

```sh
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
  go test -c -o /tmp/viva-linux-platform.test ./internal/platform/linux

GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
  go test -c -o /tmp/viva-linux-app.test ./app

GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
  go vet ./app ./internal/platform/...
```

这些结果只证明相关 Linux 测试可交叉编译、静态检查通过。本次没有执行 Linux 测试二进制、race detector、compositor、Vulkan WSI/validation 或真实桌面/性能验收。

## 7. 建议优先级

### P0：先修正确性和所有权

1. **统一所有 present 入口**：focus-loss、pointer/key/scroll/drop 不得绕过 hidden/remap/framePending 和 ACK/callback 事务；补 H1 时序测试。
2. **修复 pending resize/scale**：保留 queued redraw，明确一次 commit 对应的新 callback，补 H2 成功路径测试。
3. **统一 FD 关闭顺序**：event-source 持有稳定注册身份，异常路径也能 remove；处理 ERR/HUP/NVAL，补 IBus/AT-SPI 断开和忙循环回归测试。
4. **同步 eventfd writer 与 close**：关闭后 post 不得写旧 FD，也不得在调用 goroutine 直接执行平台命令；补并发/race 测试。

### P1：闭合现有协议和输入契约

5. SUBOPTIMAL 重建后在有限恢复边界内强制再呈现，并更新“只提交一次”的测试预期。
6. 将 IBus deadline/Expire 和 repeat 纳入主循环调度，引入 press generation；正常关闭调用 `Bus.Close`。
7. 修复 scale 往返舍入，并在缺少 viewporter 时禁用 fractional 路径或采用协议正确的回退。
8. 为本文每项 finding 增加行为测试，并在 Linux CI 真正执行 `go test ./internal/platform/linux`。

### P2：完成架构与验收收敛

9. 收敛显式 surface/configure 状态机、通用 event source 和桌面服务重连生命周期。
10. 建立 headless compositor、lavapipe/validation、真实 GNOME/KDE/Sway 矩阵，覆盖多屏、缩放、IBus、拖放、AT-SPI 和性能。
11. 同步 `docs/conformance.md` 与 `01` 的状态表，区分“源码存在、Linux 测试通过、真实环境验收”三个证据等级。

## 8. 最终判断

当前实现可以描述为：**原生 Wayland 主干和关键修复已落地，宏观架构合理，但呈现事务、FD 生命周期、输入调度和缩放仍存在明确正确性缺口。**

当前实现不能描述为：**目标架构全部完成、六项修复均已闭环、所有 commit 均已与 frame callback 配对，或 Linux/Wayland/Vulkan 已完整验收。**
