# Linux/Wayland 当前实现审查

- 日期：2026-10-29
- 分支：`docs/linux-render-refresh-flicker`
- 审查基准：当前 HEAD `3b4e331`
- 架构文档：[01-architecture-overview.md](01-architecture-overview.md)
- 前次审查：[08-linux-wayland-review-findings.md](08-linux-wayland-review-findings.md)
- 范围：Wayland 后端、窗口呈现、共享 Vulkan 重建路径、事件循环及相关测试和 CI
- 类型：只读源码审查；本文档不修改实现代码

> 后续状态：本文 findings 已进入 [task.md](task.md)，六项任务均已完成实现、独立 review 和提交。本文保留审查时的控制流证据；当前未解决事项以 [08-linux-wayland-review-findings.md](08-linux-wayland-review-findings.md) 和任务状态为准。

## 1. 总体结论

当前 HEAD 已针对前次审查的 19 项问题逐一提交修复，主干架构和大部分原始触发路径已经得到针对性处理。

本次复核新确认：

1. 6 个代码级正确性问题；
2. 1 个托盘常驻场景的实现与文档差距；
3. Linux/Wayland 回归测试未由现有 CI 执行的验证缺口；
4. 前次审查记录和架构文档需要同步（已在本次文档更新中另行处理）。

因此，当前实现仍不宜标记为“Wayland 完整验收通过”。下述代码 findings 均已通过源码控制流确认，但未宣称已在真实 compositor 或 Vulkan 驱动上复现。

### 1.1 架构是否合理

结论是：**宏观方向合理，但内部抽象尚不足以支撑文档宣称的生产可靠性。不需要推倒重来，也不能只靠继续修补单个 bug。**

#### 合理且应保留的部分

- Wayland 优先、X11 后备的运行时调度边界清晰；
- `platform.Application` 统一上层契约，Vulkan 渲染和输入路由具备跨后端复用基础；
- 单一 `poll` 主循环配合 `eventfd` 命令队列符合 Wayland 的事件模型；
- PureGo 动态绑定满足零 CGO 和交叉编译约束；
- frame callback 背压、共享桌面协议核心和错误向应用返回等设计方向正确。

这些属于主干能力，不建议因当前 findings 而替换整体技术路线。

#### 不足一：内容 dirty 与 compositor commit 被错误地视为同一件事

当前 `router.present(func(Frame) error)` 同时承担文本合并、场景更新和最终呈现，但只返回错误，无法表达：

- 内容没有变化；
- 场景已更新但未提交；
- 因重新 configure 后的 Show、resize、scale 或 swapchain 重建而必须重新提交旧场景；
- 当前帧是否已与 frame callback 配对提交。

Finding 1 说明这不是单纯的实现疏忽，而是 present 抽象缺少关键结果和提交原因。调用者只能在“返回 nil”后猜测是否发生 present，最终形成“无 commit 却有 framePending”的非法状态。

应把“更新场景”和“提交 compositor 帧”拆开，由窗口层统一决定提交时机，并显式区分 content redraw 与 initial/show/configure/scale/rebuild 等强制提交原因。

#### 不足二：窗口生命周期是多个布尔量组成的隐式状态机

`waylandWindow` 同时维护 `configured`、`firstConfigureDone`、`initialResizePending`、`framePending`、`needsRedraw`、`geometryDirty`、`maximized`、`fullscreen`、`resizing`、`hidden` 和 `closed` 等状态。configure ACK 又分散在 open、resize 和普通 render 路径。

这些字段可以组合出实现并未定义的状态，例如 hidden、framePending、clean content 和待强制 commit 的组合。继续增加布尔守卫容易修复一个排列、遗漏另一个排列。

应定义明确的 surface/configure 生命周期和单一 commit 入口。每次 configure 应作为带 serial、几何和缩放信息的事务处理，而不是由多个路径分别推断何时 ACK 和 present。

#### 不足三：错误传播和线程所有权依赖约定，而不是一致的接口

当前同时存在返回 `error`、回调内直接 `app.fail`、忽略错误和主循环保存 `loop.err` 等模式。Finding 4 说明只要新增一条回调路径，就可能再次绕开统一错误通道。

线程约束也主要依赖“大多数代码在主线程运行”的约定。后台 repeat、跨 goroutine post 和窗口字段之间存在细粒度人工同步，文档中的严格线程独占并没有被类型或封装边界完整保证。

应保持 Wayland proxy、窗口状态和渲染状态的明确单线程所有权；后台 goroutine 只传递不可变事件快照。主循环中的平台回调应统一通过一种错误处理入口，避免每个监听者自行决定返回、忽略或直接关闭应用。

#### 不足四：FD event source 抽象过弱，桌面服务生命周期仍然分叉

`waylandLoop` 通过 `extraFDs` 和 `extraCBs` 两个平行切片管理 D-Bus、IBus 和 AT-SPI，回调只接收“可读”，没有统一的 `revents`、错误返回和 hangup 处理契约。连接建立、重连和移除又分散在应用及各服务中。

这正是桌面集成行为容易分叉的结构性原因。建议抽象统一的 event source 接口，由事件循环分发完整 readiness/error 状态，并将可共享的 D-Bus 重连状态机从 Wayland/X11 传输适配中抽离。

#### 不足五：原生资源所有权过于分散

Vulkan swapchain、render pass、pipeline、framebuffer 等 handle 直接存放在 renderer 上，由多个成功、失败和恢复路径手工清理。Finding 2 的重复销毁说明 deferred cleanup 与局部错误分支没有形成唯一所有权规则。

应将需要成对释放的原生资源封装为小型、单一 owner 的对象或资源组，使创建失败、替换和最终关闭共用同一套释放逻辑。Wayland proxy 的手动 opcode/listener 绑定也适合通过协议元数据或代码生成降低版本和 ABI 漂移风险。

#### 架构改造建议

优先重构顺序不是“再增加一层后端接口”，而是：

1. 统一 scene update、强制 present、surface commit 和 frame callback 的契约；
2. 收敛窗口/configure 状态机及 ACK 所有权；
3. 统一错误传播和主线程所有权；
4. 抽象 FD event source 与共享桌面服务生命周期；
5. 收敛 Vulkan/Wayland 原生资源所有权，并补协议感知的测试 fake。

如果目标只是演示 Wayland 窗口和验证技术可行性，当前架构足够继续迭代；如果目标是文档所述的高可靠、低能耗、长期维护的生产 GUI 运行时，当前内部边界还不够合理。

## 2. 当前 Findings

### 1. 高：Show 重新映射缺少 configure，clean frame 还会留下无 commit 的背压状态

- 主要位置：
  - `internal/platform/linux/window_wayland.go:263`
  - `internal/platform/linux/window_wayland.go:289`
  - `internal/platform/linux/window_wayland.go:750`
  - `internal/platform/linux/window_wayland.go:774`
  - `internal/platform/linux/window_wayland.go:961`
  - `internal/platform/linux/wayland_frame.go:33`
- 状态：源码和 xdg-shell 协议约束确认；未在真实 compositor 上复现。

#### 问题 A：Show 直接 present，违反重新映射序列

`Hide` 通过 nil buffer commit 使 xdg_toplevel 成为 newly-unmapped surface。xdg-shell 要求这种 surface 在再次附加 buffer 前重复 initial commit，等待新的 `xdg_surface.configure`，再按正常 configure 流程 ACK 并提交 buffer。

当前 `Show` 仅设置 `hidden=false` 并直接调用 `renderFrame`。因此，即使强制 `renderer.present`，也缺少重新映射必需的 empty commit/configure 事务，不能保证窗口恢复。

#### 问题 B：clean frame 不提交 buffer，却提前建立 frame 背压

`renderFrame` 在知道是否真正 present 之前调用 `requestFrameCallback()`。但 `router.present` 会在以下情况直接返回 `nil`：

- `textInput.prepare` 返回 `dirty=false`；
- `renderer.apply` 返回 `changed=false`。

典型路径为：

1. 内容没有变化时触发 `renderFrame`；
2. 先请求 frame callback；
3. `OnRedraw` 产生 clean frame，`router.present` 不调用 `renderer.present`；
4. 没有新 buffer 和对应 commit；
5. `framePending` 已设为 true，后续 Waker 不再直接重绘；
6. 没有与 callback 配套的新 commit，frame callback 无法驱动后续恢复。

同类风险也存在于 resize/scale 后必须提交新 buffer、但 `OnResize` 或 `OnRedraw` 返回 clean frame 的路径。问题不只是“跳过一次无意义绘制”，而是 clean frame、强制呈现、重新 configure 和 frame 背压之间缺少统一事务。

#### 现有测试缺口

`TestWaylandHiddenRedrawWaitsForShow` 的 mock `present` 总是返回成功，没有模拟真实的 `dirty=false` 或 `changed=false` 分支，也没有验证 Show 后的 empty commit、configure、ACK 和 buffer commit 顺序，因此无法发现上述问题。

#### 修复建议

- 将生命周期改为 `Unmapped -> AwaitInitialConfigure -> Mapped`；
- Show 先执行无 buffer initial commit，等待新的 configure，不得直接 remap；
- 在重新 configure 后，即使 scene clean 也必须提交实际 buffer；
- 让 scene apply 与 surface commit 分离，并返回是否实际提交；
- 只为实际 buffer commit 建立对应的 frame callback；
- 增加协议 fake，验证 Hide/Show 的 empty commit、configure、ACK、present 顺序，以及 clean resize、clean scale 和后续 Waker 恢复。

### 2. 高：swapchain 创建失败会重复销毁旧 swapchain

- 位置：
  - `internal/platform/linux/vulkan_linux.go:670`
  - `internal/platform/linux/vulkan_linux.go:742`
- 影响范围：共享 Vulkan 路径，Wayland 和 X11 均受影响。
- 状态：源码确认；未在真实 Vulkan 驱动上复现。

#### 触发路径

`createSwapchainWithOld` 的 deferred cleanup 会在返回错误时销毁非零的 `oldSwapchain`。但 `vkCreateSwapchainKHR` 失败分支已经显式销毁旧 handle：

1. `vulkan_linux.go:743-745` 调用 `destroySwapchain(oldSwapchain)`；
2. 返回错误时未将 `oldSwapchain` 清零；
3. deferred cleanup 在 `vulkan_linux.go:673-675` 再次销毁同一个 handle。

重复销毁 Vulkan 对象可能导致 validation 错误、驱动异常或进程崩溃。resize、scale 和 present 的 swapchain 恢复路径均可能到达该分支。

#### 现有测试缺口

- `TestCreateSwapchainCleansPartialResources` 覆盖新 swapchain 创建后的部分资源清理；
- `TestSwapchainRecreationPassesOldHandle` 覆盖成功重建；
- 没有覆盖“非零 oldSwapchain + vkCreateSwapchainKHR 失败 + destroy 恰好一次”。

#### 修复建议

统一旧 handle 的销毁所有权：删除失败分支中的重复销毁，或在显式销毁后立即将 `oldSwapchain` 清零。增加记录每个 handle 销毁次数的回归测试。

### 3. 中：`eventfd` 写入被 `EINTR` 打断时会丢失唯一唤醒

- 位置：`internal/platform/linux/wayland_loop.go:179`
- 状态：源码确认；属于低概率信号时序问题，未实机复现。

#### 触发路径

`waylandLoop.post` 先把命令追加到 `cmdQueue`，再写入 eventfd，但忽略 `write` 的返回值。

若 8 字节写入在真正写入前被信号打断：

1. 命令已经入队；
2. eventfd 仍不可读；
3. 主线程可能继续阻塞在 `poll(-1)`；
4. 如果没有其他 FD 事件，`Quit`、Close 或 redraw 命令会一直滞留。

后台线程收到信号并被打断，并不意味着主线程的 `poll` 也一定收到 `EINTR`，因此不能依赖主循环自行恢复。

#### 修复建议

- 至少对写入前的 `EINTR` 进行重试；
- 区分 `EINTR` 与 `EAGAIN`：后者通常表示计数器已非零、FD 已可读，不应按丢失唤醒处理；
- 增加 mock 首次写入 interrupted、随后重试成功的回归测试。

### 4. 中：装饰和最大化路径仍吞掉 Vulkan 错误

- 位置：
  - `internal/platform/linux/window_wayland.go:373`
  - `internal/platform/linux/window_wayland.go:379`
  - `internal/platform/linux/window_wayland.go:543`
  - `internal/platform/linux/window_wayland.go:552`
- 状态：源码确认；未在真实 Vulkan 故障环境中复现。

#### 触发路径

装饰模式、最大化和取消最大化路径使用：

```go
_ = win.state.renderer.setFrameMargin(...)
```

但 `setFrameMargin` 可能因以下操作失败：

- `deviceWaitIdle`；
- frame pipeline 或 buffer 创建；
- command recording。

错误被丢弃后，`win.margin`、装饰状态和 renderer 状态已经部分改变，主循环却继续运行。这与本轮 redraw、resize、scale 和 input 路径统一传播错误的目标不一致。

#### 修复建议

将这些错误汇入 `app.fail`，并中止后续 configure、ACK 和 present 状态推进。增加注入 `deviceWaitIdle` 或 command recording 失败的回归测试。

### 5. 中：`VK_SUBOPTIMAL_KHR` 只记录、不消费，交换链可永久不重建

- 位置：
  - `internal/platform/linux/vulkan_linux.go:961`
  - `internal/platform/linux/vulkan_linux.go:991`
  - `internal/platform/linux/vulkan_linux.go:1019`
  - `internal/platform/linux/vulkan_linux.go:1038`
- 影响范围：共享 Vulkan 路径，Wayland 和 X11 均受影响。
- 状态：源码确认；未在持续返回 SUBOPTIMAL 的真实驱动上复现。

`presentOnce` 在 acquire 或 present 返回 `VK_SUBOPTIMAL_KHR` 时设置 `swapchainSuboptimal=true`，但 `presentRebuilding` 只根据 `VK_ERROR_OUT_OF_DATE_KHR` 重建。除成功创建 swapchain 时清零外，没有恢复路径读取该标志。

如果驱动持续返回 SUBOPTIMAL，且之后没有独立 resize 或 OUT_OF_DATE，当前交换链会一直被沿用，可能持续呈现与 surface 不匹配的图像。现有 `TestSuboptimalDoesNotRecreateInFrame` 只约束“不在当前帧立即重建”，不能证明后续受控边界会恢复。

修复时应允许当前 SUBOPTIMAL 帧完成，再在下一受控帧边界消费标志并重建一次。分别覆盖 acquire=SUBOPTIMAL、present=SUBOPTIMAL、重建成功和重建失败，避免同帧无限重建。

### 6. 高：首次 `wl_display_roundtrip` 错误被吞掉，可绕过初始 configure 门限

- 位置：
  - `internal/platform/linux/window_wayland.go:911`
  - `internal/platform/linux/window_wayland.go:1004`
- 状态：源码确认；未在真实 compositor 断开场景中复现。

初始 empty commit 后，代码执行 `_ = app.api.displayRoundtrip(app.display)`，完全忽略失败。随后即使未获得有效初始 configure，也会继续创建 renderer，并执行 `renderer.present` 和 `surfaceCommit`。

这同时造成两个问题：显示/协议错误的根因被丢失，以及“首次 configure 前不得附加用于映射的 buffer”的协议门限被绕过。

修复时应检查 roundtrip 返回值，失败时保留 display 错误并统一清理返回；首次 buffer present/commit 前还应显式验证初始 configure 已完成。长期目标是用异步 `AwaitInitialConfigure` 状态替代阻塞 roundtrip，而不是省略该门限。需要增加 roundtrip 失败和未收到 configure 不得 present 的回归测试。

### 7. 中：托盘常驻实现缺口仍需明确产品契约

- 实现位置：`internal/platform/linux/wayland_app.go:176`
- 目标契约：`docs/wayland/01-architecture-overview.md` 第 7.4 节
- 状态：源码确认；当前架构文档已明确该能力尚未实现，不再将其描述为现有保证。

`Run` 的循环条件为 `app.hasWindows()`。只创建 StatusItem、没有窗口的应用会在启动回调后直接退出并关闭托盘服务，无法进入目标架构所述的 eventfd/D-Bus 常驻处理阶段。

现有 `TestApplicationPostWithoutWindows` 只验证无窗口时命令入队和 eventfd 唤醒，不验证 `Run` 在无窗口时是否继续运行。

需要先决定是否支持“窗口或托盘项或显式后台常驻”的 keep-alive 契约，再调整运行条件、退出语义和测试。普通 Portal/AT-SPI watcher 不应无意中让应用永久存活。

## 3. 前次 19 项问题的当前状态

下表只表示当前 HEAD 已存在对应的源码修复，不代表已完成真实 Linux/Wayland 环境验收。

| 前次编号 | 主题 | 对应修复 | 当前审查说明 |
| --- | --- | --- | --- |
| 1 | 键盘 repeat 销毁后崩溃 | `a4aba97` | 已加入闭包生命周期复核 |
| 2 | 持续 poll 错误忙循环 | `9a87f38` | 非 EINTR 错误停止循环并记录错误 |
| 3 | 初始 configure 尺寸丢失 | `1243583`、`1dd3060` | 保留初始几何并处理初始布局 |
| 4 | 隐藏窗口 dirty redraw 重新映射 | `10df8b0` | 原 dirty 路径已修复；clean Show 见本次 Finding 1 |
| 5 | flush 背压无恢复 | `13a993e` | EAGAIN 启用 POLLOUT 恢复 |
| 6 | dispatch/socket 故障正常退出 | `3ecfa04` | transport 错误进入 loop error |
| 7 | `app.fail` 丢失错误 | `62b2a9d` | 保存并返回首个应用错误 |
| 8 | redraw 吞错 | `3f6c46b` | redraw/apply/present 错误进入应用错误通道 |
| 9 | resize 吞错 | `2d63631` | 错误传播已补；强制 present/clean frame 见 Finding 1 |
| 10 | scale 吞错 | `a969ee5` | 错误传播已补；强制 present/clean frame 见 Finding 1 |
| 11 | input 吞错 | `c7ca05d` | 输入错误进入统一错误通道 |
| 12 | FileDropOver 不呈现 | `dd51f05` | hover frame 和错误得到处理 |
| 13 | IBus modifier 位集错误 | `0216856` | 转换为 X modifier mask |
| 14 | IBus 缺少 key release | `6447aa3` | release 使用 IBus ReleaseMask 转发 |
| 15 | 整数 HiDPI 回退失效 | `0066204` | 增加 `wl_output.scale` 绑定和缓存 |
| 16 | 非文件 drag 幻影 hover | `5fea918` | motion 过滤非文件 offer |
| 17 | AT-SPI 重连后无法恢复 | `3e8c3d9` | 失败后恢复 watcher 轮询能力 |
| 18 | 未应用窗口最小尺寸 | `2f82499` | 调用 `xdg_toplevel_set_min_size` |
| 19 | 桌面集成“100% 共享”边界错误 | `3b4e331` | 表格、边界说明及后续架构图均已修正 |

## 4. 文档和工程问题

### 4.1 前次审查记录的处理

`08-linux-wayland-review-findings.md` 原先将全部 19 项问题描述为当前缺陷。本次文档更新已移除这些已修复条目，只保留当前未解决项；原问题与修复提交的对应关系保留在本文第 3 节，避免重复实现或编写与当前行为相反的测试。

### 4.2 架构文档已与业界实践重新对齐

`01-architecture-overview.md` 原先仍存在“100% 共用”等绝对表述，并把部分目标设计写成当前能力。本次更新已将其重构为“目标设计与实施契约”，明确共享边界、当前实现差距和迁移顺序，并补充 libwayland、xdg-shell、fractional-scale、Vulkan WSI、Calloop、GTK 和 GLFW 的参考实践。

### 4.3 Linux/Wayland 测试未由现有 CI 执行

`.github/workflows/ci.yml:35-36` 明确说明 Linux 后端只进行交叉检查。`text-input.yml` 在 Ubuntu 上只执行 `./app`，没有执行 `internal/platform/linux` 测试。

因此，本轮新增的 Wayland 回归测试即使能在 Linux 上运行，也不会由现有 CI 自动提供持续保护。建议增加无需真实 compositor 的 Linux mock/unit 测试任务，并将真实 compositor/Vulkan 验收单独分层。

### 4.4 文档 patch 清理

本次更新已移除原架构文档中的 trailing whitespace 和不可移植的本机绝对 `file://` 链接，并将源码位置改为仓库相对路径。

## 5. 建议修复顺序

1. **协议门限与呈现事务**：修复 Show 重新 configure、首次 roundtrip 错误传播、clean/forced present 和 frame callback 配对。
2. **Vulkan 资源与恢复**：确保 oldSwapchain 恰好销毁一次，并在受控边界消费 SUBOPTIMAL。
3. **主循环可靠性**：处理 eventfd 写入 EINTR，补齐丢失唤醒测试。
4. **错误处理闭环**：清理装饰和最大化路径的 `setFrameMargin` 吞错。
5. **明确应用生命周期**：决定托盘常驻是否为支持场景，并统一实现、测试和文档。
6. **让验证持续生效**：在 Linux CI 执行平台单元测试，再开展真实 compositor、Vulkan、多屏缩放、IBus、拖放和 AT-SPI 验收。
7. **同步文档**：保持未解决清单、目标架构和实现状态一致。

## 6. 验证记录与限制

本次审查完成以下验证：

- `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go test -c ./internal/platform/linux`：通过；
- `GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go test -c ./internal/platform/linux`：通过；
- `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go test -c ./app`：通过；
- `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go vet ./...`：通过；
- `go test ./...`：5535 通过、1 失败、23 跳过；
- 唯一失败为 `TestDesktopThemeDetectionAndStyling`，原因是 Darwin 下 `runtime.GOOS` 守卫与测试的 Linux 主题期待不一致，与前次审查记录一致；
- Linux 源码 gofmt 检查：通过；
- 当前工作区普通 `git diff --check`：通过；完整文档分支 patch 的 whitespace 结果见第 4.4 节。

限制：

- 审查主机为 macOS；
- Linux 测试包只完成交叉编译，没有在本环境执行；
- 未完成真实 Linux compositor、Vulkan GPU、多屏缩放、IBus、拖放和 AT-SPI 验证；
- mock 测试、静态分析和交叉编译不能替代真实桌面环境验收。
