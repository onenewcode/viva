# Linux/Wayland 架构审查记录

- 日期：2026-10-29
- 分支：`docs/linux-render-refresh-flicker`
- 基准文档：[01-architecture-overview.md](01-architecture-overview.md)
- 范围：当前分支 Linux/Wayland 架构及 `internal/platform/linux` 的 39 个变更文件
- 类型：只读源码审查；本文不修改实现代码

## 1. 总体结论

当前分支符合主流 GUI 框架的架构方向：后端运行时调度、平台适配器、共享 Vulkan 渲染核心、`eventfd` 命令队列、单一事件循环、帧回调背压，以及 PureGo 动态绑定，均有合理实现。

但实现尚不能按 `01-architecture-overview.md` 宣告完整落地或验收通过。主要差距集中在：

1. 异步命令的窗口生命周期安全；
2. 主循环与渲染路径的错误传播和背压恢复；
3. 初始 configure、隐藏窗口、最小尺寸和缩放等跨后端窗口契约；
4. IBus、拖放和 AT-SPI 的桌面集成完整性；
5. 文档声明的共享边界与实际实现不一致。

因此，更准确的定位是：**主干架构可保留，但当前实现仍需完成可靠性修复和真实 Linux/Wayland 环境验收。**

## 2. 四项专项结论

### 2.1 错误处理没有形成闭环

Vulkan 核心会返回错误，但 Wayland 调用层在 redraw、resize、scale、input、dispatch、flush 等路径吞错；`fail` 和 `Run` 还会把故障变成正常 `nil` 退出。持续的非 EINTR `poll` 错误会形成满 CPU 忙循环，`wl_display_flush` 的背压也没有 `POLLOUT` 恢复路径。

### 2.2 旧 compositor 的整数 HiDPI 回退失效

`getOutputScale` 固定返回 1，且没有 `wl_output.scale` 绑定或缓存。在 `wl_surface` v4/v5、无 `wp_fractional_scale_manager_v1` 的高分屏环境中，窗口会一直保持 1x 缓冲区。

### 2.3 键盘 repeat 存在窗口销毁后的崩溃路径

已入队的 repeat 闭包没有重新验证窗口生命周期，可按 `[destroy, repeat]` 顺序执行并解引用已经清空的 `win.state`。

### 2.4 桌面集成不是文档所称的“100% 共享”

Portal、AT-SPI 和 D-Bus tray 复用了协议核心，但连接生命周期和 FD dispatch 是 Wayland/X11 两套平行实现。文档的共享边界不准确，实际行为也已发生分叉：Wayland 的 AT-SPI watcher 在重连失败后会永久失去恢复能力。

## 3. Findings

以下 19 项按严重度排序。“确认”指源码路径与控制流分析确认，不代表每一项都已在真实 compositor 上复现。

### 1. 键盘 repeat 闭包可在窗口销毁后崩溃

- 位置：`internal/platform/linux/wayland_keyboard.go:374`
- 场景：Close 先入队 destroy，repeat goroutine 随后通过入队前检查并入队 repeat；`drainCommands` 先执行 destroy、停止 repeat 并设置 `win.state=nil`，再执行无法取消的 repeat 闭包，在 `win.state.router.dispatchKey` 处 panic。

### 2. 持续的非 EINTR poll 错误形成满 CPU 忙循环

- 位置：`internal/platform/linux/wayland_loop.go:223`
- 场景：`poll` 持续返回 EBADF 或 EINVAL 时，`step` 返回 `l.running.Load()==true`；`Run` 立即再次调用 `step`，既不渲染也不返回错误。

### 3. 初始 compositor configure 尺寸被丢弃

- 位置：`internal/platform/linux/window_wayland.go:866`
- 场景：平铺或最大化 compositor 在初始 roundtrip 返回不同于 `Config.Width/Height` 的正尺寸；`applyPendingResize` 因 `win.state==nil` 提前返回，renderer 随后按旧尺寸创建，pending serial 仍被 ACK，旧尺寸帧被提交且没有后续 configure 保证能纠正。

### 4. 隐藏窗口的 dirty redraw 可重新映射 surface

- 位置：`internal/platform/linux/window_wayland.go:259`
- 场景：Hide 通过 nil buffer 隐藏窗口后，Waker 触发有内容变化的 `OnRedraw`；hidden 分支仍调用 `router.present`，WSI present 可附加并提交非 nil buffer，使窗口重新出现而 `win.hidden` 仍为 true。

### 5. wl_display_flush 背压没有恢复路径

- 位置：`internal/platform/linux/wayland_loop.go:211`
- 场景：socket 背压使 flush 返回 EAGAIN；poll 只监听 POLLIN，缓冲的初始 map、resize、move 或 commit 请求可能一直不发送，同时错误也不向应用报告。

### 6. dispatch、协议和 socket 故障被变成正常退出

- 位置：`internal/platform/linux/wayland_loop.go:261`
- 场景：compositor 断开、`displayDispatch` 返回协议错误或 FD 出现 POLLERR/POLLHUP；`step` 返回 false，清理销毁窗口，但 `RunApplication` 收到正常 `nil`，无法获得传输故障原因。

### 7. waylandApplication.fail 抹掉触发故障的错误

- 位置：`internal/platform/linux/wayland_app.go:425`
- 场景：IME 或 AT-SPI 回调以 presentation error 调用 `fail`；所有窗口关闭后 `Run` 返回 `nil`，表现为用户正常退出，而 X11 会保存并返回 `firstErr`。

### 8. 普通 redraw 丢弃应用、上传和 present 错误

- 位置：`internal/platform/linux/window_wayland.go:289`
- 场景：`OnRedraw` 返回错误或恢复后的 acquire/submit/present 失败；`needsRedraw` 已清除，窗口继续保留旧帧且无错误传到 `RunApplication`，`OnRedraw` 错误甚至触发旧场景 present。

### 9. resize 失败后仍确认 configure 并报告新几何

- 位置：`internal/platform/linux/window_wayland.go:426`
- 场景：resize、场景上传或 present 失败时，返回错误被丢弃，`presented` 甚至可在 `router.present` 失败时设为 true；configure 已 ACK，`OnFrameChange` 仍执行，留下空白、旧帧或错误尺寸 surface。

### 10. scale 切换忽略 renderer 和 present 错误

- 位置：`internal/platform/linux/wayland_scale.go:182`
- 场景：显示器 scale 变化期间 `setScale`、`setFrameMargin`、resize 或后续 present 失败；窗口逻辑尺寸和 scale 继续前进而 renderer 保持旧状态或部分重建，造成错位、模糊或空白且无错误原因。

### 11. 输入路径丢弃共享 router 的错误

- 位置：`internal/platform/linux/wayland_pointer.go:140`
- 场景：pointer、key、scroll 与部分 drop 回调返回错误，或对应 frame 的 Vulkan apply/present 失败；事件仍被视为已处理且主循环继续，视觉更新静默消失，而 X11 会将错误传到 `fail`。

### 12. FileDropOver 的返回 frame 和错误被完全丢弃

- 位置：`internal/platform/linux/wayland_data.go:178`
- 场景：文件拖入或移动时，`OnFileDrop` 返回用于高亮的 repaint frame；enter/motion 两处均不调用 `router.present`，高亮只能等无关重绘，回调错误也被吞掉。

### 13. Viva modifier 位集被错误地直接传给 IBus

- 位置：`internal/platform/linux/wayland_keyboard.go:305`
- 场景：IBus 需要 X modifier mask，而 Viva Control=0x02 会被解释为 X Lock，Viva Alt=0x04 会被解释为 Control；Ctrl+Space 和 Alt+x 等 IME 快捷键与组合键因此被错误解释。

### 14. 键盘释放事件没有发送给 IBus

- 位置：`internal/platform/linux/wayland_keyboard.go:317`
- 场景：IBus 引擎依靠 lone Shift release 切换中英文时，press 能到达引擎，release 却只可能到达应用；模式切换不发生，引擎还可能保留陈旧按键状态。

### 15. 旧 Wayland compositor 的整数 HiDPI 回退失效

- 位置：`internal/platform/linux/wayland_scale.go:210`
- 场景：在 `wl_surface` v4/v5、无 fractional-scale manager 的 2x output 上打开窗口；surface enter 得到 scale 1，`applyScale(1)` 因初始值相同直接返回，且没有 `wl_output.scale` listener/cache，最终保持 1x 低密度 buffer 并被 compositor 放大模糊。

### 16. 非文件 drag offer 也会产生无终止事件的 FileDropOver

- 位置：`internal/platform/linux/wayland_data.go:177`
- 场景：拖入仅含 `text/plain` 的 offer 时，enter 正确地不发文件回调，但 motion 无条件发送 `FileDropOver`；drop 发现不支持后直接返回，既无 `FileDropDrop` 也无 `FileDropLeave`，应用停留在幻影 file-hover 状态。

### 17. AT-SPI 重连失败后无法自动恢复

- 位置：`internal/platform/linux/atspi_linux.go:459`
- 场景：accessibility 启用后 Dial 或 `bridge.register` 失败；watcher 已不在 poll 集合但仍非 nil，后续 `PropertiesChanged` 不再分发，新窗口也不重新 probe，只能重启恢复。

### 18. Wayland 没有应用窗口最小尺寸

- 位置：`internal/platform/linux/window_wayland.go:854`
- 场景：在 native Wayland 下配置 `MinWidth=400`、`MinHeight=300` 并拖动边框缩小；初始提交前从未调用现有的 `xdg_toplevel_set_min_size`，compositor 允许内容缩到要求以下，而 X11/XWayland 会限制。

### 19. 架构文档的“100% 共享”边界不成立

- 位置：`docs/wayland/01-architecture-overview.md:207`
- 场景：维护者依据文档只修改 `desktop_bus_linux.go`、`status_item_linux.go` 或 `atspi_linux.go` 的 X11 生命周期，会误以为两个后端都已修复；Wayland 仍执行另一套实现，AT-SPI 恢复差异即为实际例证。

## 4. 排除项

### 最后窗口 Close 后 poll(-1) 永久 hang：REFUTED

该候选不成立。Close 的 `app.post` 会写入 wakeFD，保证本次 poll 返回，并让 `Run` 重新检查窗口列表，因此不会仅因最后一个窗口关闭而永久挂在当前 poll 中。

## 5. 建议修复顺序

1. **崩溃与主循环安全**：repeat 生命周期、持续 poll 错误、flush 背压。
2. **窗口呈现契约**：初始 configure、隐藏窗口 present、resize 错误传播、最小尺寸。
3. **统一错误通道**：让 `fail` 保存首个错误，所有 redraw/input/scale/dispatch 路径汇入同一错误处理，避免正常 `nil` 退出。
4. **桌面集成**：IBus modifier/release、拖放状态机、AT-SPI watcher 恢复。
5. **缩放与文档边界**：实现 `wl_output.scale` 回退，并将共享服务生命周期抽取为真正的跨后端实现；同步修订架构文档。

## 6. 验证记录与限制

- `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go test -c ./internal/platform/linux`：通过。
- `go vet ./internal/platform/linux ./app`：通过。
- 主机测试：555 通过、1 失败、1 跳过。失败项为 `TestDesktopThemeDetectionAndStyling`，原因是 Darwin 下的 `runtime.GOOS` 守卫与测试的 Linux 主题期待不一致。
- `git diff --check` 另发现 `internal/platform/linux/wayland_window_test.go:943` 文件末尾多余空行。
- 审查环境为 macOS，没有完成真实 Linux compositor、Vulkan GPU、多屏缩放、撕裂和能耗验证；mock 测试与静态分析不能替代这些验收。
