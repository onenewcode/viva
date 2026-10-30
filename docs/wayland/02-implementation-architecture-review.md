# Linux/Wayland 当前实现复核

- 日期：2026-10-30
- 基线：`4e77606` + Task 11 Weston 复跑与 remap 修复
- 类型：只读源码复核 + 原生 Linux 验收
- 开发契约：[01-architecture-overview.md](01-architecture-overview.md)
- 后续任务：[task.md](task.md)

## 1. 结论

**宏观架构合理，不需要推倒重写；当前 diff 没有保留的源码 finding。** 主循环的 prepare/read、surface/configure 事务、frame-callback 背压、系统 API 的主线程调用边界、Vulkan 恢复边界和桌面服务重连边界可以保留。Task 11 的入口和 KWin、Weston 两个 headless compositor 的原生验收已完成（lavapipe + validation），Weston 复跑暴露的 remap 缺陷已修复并有回归测试，任务关闭。Task 8 已在本机可执行范围内完成：新增 scale、多输出、事件交错和性能测量，受限项（GNOME/Sway/IBus、真实 portal 重启、device loss 注入、运行时 output 热插拔）按 §4.5 记录为限制，未声称验证。评审期间曾发现按定时器对所有 compositor 生效的 remap 兜底会让 KWin 在竞态下以 `unconfigured_buffer` 终止，已改为只对识别出的 Weston 启用（见 4.3）。

“源码确认”表示控制流中存在问题，不表示已在真实 Linux/compositor/GPU 上复现可见症状；真实环境结果只在第 4 节按层记录。

## 2. 当前 findings

**F1（既有，非本次引入，未修复）：真实 X11/Plasma 会话下拖放测试偶发收不到回复。**

`TestAFileDragFromAnotherClientIsAcceptedAndDropped` 在 `-race` 重复运行中约 5%–8% 失败（一次 120 次运行失败 6 次），表现为目标窗口没有收到 `XdndPosition` 或 `XdndDrop`，源端等不到 `XdndStatus`/`XdndFinished`。证据：

- 失败时目标客户端连接 `poll` 返回 `ready=0, revents=0`，而同一时刻源连接往返只需 0.3–1.2ms，X 服务端没有阻塞；
- 目标客户端 goroutine 停在事件循环的 `poll`，不是死锁在业务代码；
- 临时加入“poll 前先取 libxcb 内部队列事件”的修改后失败率不变，说明事件没有进入客户端队列；
- 在基线 `cb6958b` 的独立 worktree 中以相同命令复现；
- 换用 lavapipe（`VK_ICD_FILENAMES=lvp_icd.json`）仍然复现，与渲染驱动无关。

结论：属于 Xwayland/KWin 下跨客户端 `SendEvent` 的投递异常，客户端侧没有可验证的修复点；与 Task 11 的 Wayland 改动无关。本次只把测试中 3 处无界 channel 接收改为 10s 有界等待（新增 `nextDropEvent` 帮助函数），避免失败时整包挂起到超时。该问题保留到 Task 8 的真实桌面矩阵继续跟踪，不属于本次 Task 11 修复范围。

没有其他已知源码阻断项。真实 session bus/portal 的终止、重启和设置更新仍未执行，不能用 fake 回归测试替代 Task 8 的真实服务恢复验收。

## 3. 已核对的主干

本次未发现需要改变以下设计的新证据：

- `internal/platform/linux/wayland_loop.go` 的 prepare/read/cancel、flush 背压和命令唤醒；
- `internal/platform/linux/window_wayland.go` 的异步初始 configure、集中 ACK/present、Hide/Show 状态迁移和仅限 Weston 的有界 remap 兜底；
- `internal/platform/linux/wayland_registry.go` 的手写接口表及其对象创建请求的 `types` 数组；
- `internal/platform/linux/wayland_frame.go` 的 callback 与实际呈现配对；
- `internal/platform/linux/vulkan_linux.go` 的有界恢复、重建后再次呈现和 old-swapchain 销毁；
- `internal/platform/linux/wayland_scale.go` 的缩放优先级与几何更新。

这些结论仅表示保留主干，不表示第 4 节之外的环境验收已经完成。

## 4. 本次验证

### 4.1 环境

- Debian 13，Linux 6.12.105 amd64，Go 1.27.1；
- 真实桌面：KDE Plasma 6.3.6（KWin 6.3.6，Wayland 会话 `wayland-0`，Xwayland `DISPLAY=:1`）；
- headless compositor：`kwin_wayland --virtual`，以及 Weston 14.0.2。本机没有安装 Weston，也没有 root；Weston 从 Debian 包解包到 `/tmp/weston-root`，用 `unshare -rm` + overlayfs 把模块放到其编译期搜索路径，启动脚本只用于本机验收，不进入仓库；
- 软件 Vulkan：Mesa lavapipe（`VK_ICD_FILENAMES=/usr/share/vulkan/icd.d/lvp_icd.json`）；
- Validation：系统未安装 Khronos validation layer；使用 Steam 运行时自带的 1.3.290，通过 `/tmp/vkval` 下的临时清单加载（`VK_LAYER_PATH`、`VK_INSTANCE_LAYERS`）。

### 4.2 命令与结果

静态检查和交叉编译全部成功：

```sh
gofmt -l internal/platform/linux
git diff --check
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go vet ./internal/platform/... ./app
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go vet ./internal/platform/... ./app
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go test -c -o /tmp/viva-task11-amd64.test ./internal/platform/linux
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go test -c -o /tmp/viva-task11-arm64.test ./internal/platform/linux
```

原生单元测试与 race（含真实 Plasma/Xwayland 会话中的 X11 测试）：

```sh
go test -race -count=1 ./internal/platform/linux
go test -race -count=1 -v ./internal/platform/linux   # 407 pass / 14 skip / 0 fail（F1 偶发失败见 §2）
```

headless compositor + lavapipe + validation 集成入口：

```sh
VIVA_WAYLAND_INTEGRATION=1 VIVA_WAYLAND_COMPOSITOR=kwin_wayland \
VK_ICD_FILENAMES=/usr/share/vulkan/icd.d/lvp_icd.json \
VK_LAYER_PATH=/tmp/vkval VK_INSTANCE_LAYERS=VK_LAYER_KHRONOS_validation VK_LOADER_DEBUG=layer \
  go test ./internal/platform/linux -run '^TestWaylandHeadlessIntegration$' -v -count=1
```

结果 `PASS (0.62s)`，`initial 320x240, maximized 1024x740, restored 320x240, remapped 320x240, maximized remap 320x240, submissions 9`：

- 首次呈现：`initial 320x240`，renderer 尺寸与窗口一致且提交次数大于 0；
- swapchain 重建：maximize 到 `1024x740`、restore 回 `320x240`，每次 renderer 尺寸跟随窗口且提交次数递增；
- Hide/Show：重新映射后 `remapped 320x240` 并再次呈现；
- 最大化后 Hide/Show：KWin 在 remap 时重新推导为未最大化（`maximized remap 320x240`），仍完成呈现；整轮总提交 9 次。另一次 `WAYLAND_DEBUG=1` 运行 `PASS (0.63s)`，日志中 `weston_` 全局 0 处、`unconfigured_buffer`/`error 3` 0 处，确认走严格等待 configure 的路径；
- 失败清理：杀掉 compositor 后 `Run` 返回非 nil 错误，窗口关闭（`state == nil`），renderer 释放（`api == nil`）；
- 本次运行的 loader 输出显示 validation layer 已作为实例层插入，捕获的输出里没有 `VUID-`/`Validation Error`。该结论来自人工检查日志，测试本身不断言 validation 输出；测试失败时打印的应用错误也记录在日志中，用于确认非 nil 退出确实来自 compositor 断开。

Weston 14.0.2 + lavapipe + validation（同一入口，compositor 换成解包后的 Weston）：

```sh
VIVA_WAYLAND_INTEGRATION=1 VIVA_WAYLAND_COMPOSITOR=/tmp/weston-run.sh \
VIVA_WAYLAND_COMPOSITOR_ARGS='--backend=headless --renderer=gl --socket={socket} --idle-time=0 --width=1024 --height=768' \
VK_ICD_FILENAMES=/usr/share/vulkan/icd.d/lvp_icd.json \
VK_LAYER_PATH=/tmp/vkval VK_INSTANCE_LAYERS=VK_LAYER_KHRONOS_validation \
  go test ./internal/platform/linux -run '^TestWaylandHeadlessIntegration$' -v -count=1
```

结果 `PASS (0.59s)`：`initial 320x240, maximized 1024x736, restored 320x240, remapped 320x240, maximized remap 1024x736, submissions 6`；首次呈现、重建、普通 Hide/Show、最大化后 Hide/Show 和失败清理都与 KWin 一致，区别是 Weston 在 remap 后保留最大化和保留的 configure。另一次带 `VK_LOADER_DEBUG=layer` 的运行显示 `Insert instance layer "VK_LAYER_KHRONOS_validation"`，日志没有 `VUID-`/`Validation Error`。打开 `WAYLAND_DEBUG=1` 重跑同样 `PASS (0.59s)`，日志包含 `get_xdg_surface(new id xdg_surface#10, wl_surface#3)` 和 `get_toplevel(new id xdg_toplevel#11)`（类型化输出），包含 `weston_desktop_shell`/`weston_capture_v1` 全局，且没有 `error 4` 或几何不匹配。

### 4.3 本次修复的原生失败

在这台 Linux 机器上首次原生执行时，两个既有测试失败：`TestWaylandRestoreToSavedFloatingSize`（期望最大化尺寸 1920x1080，实际 548x448）和 `TestWaylandFractionalScaleTransitionsAcrossOutputs`（scale 1.25 后 viewport 未更新）。基线上用同一命令可以复现，原因是测试窗口没有标记为 `mapped`，resize/recreate 事务被跳过。本次：

- 在两个测试中把窗口置为 `waylandSurfaceMapped`，使其真正执行恢复缩放事务；
- 为 `newWaylandPresentTestRenderer` 安装 fake swapchain 调用，使 scale/resize 事务在没有驱动时也能完成重建，而不是在原生 Linux 上 panic。

这些是测试替身缺陷，不改变生产代码。

Weston 复跑暴露了一个生产缺陷：Hide/Show 的空 commit 之后 Weston 不再发送 configure，窗口永远无法重新映射；Weston 的 `xdg_surface` 在 unmap 后仍保持 configured（`weston_desktop_xdg_toplevel_committed` 对无内容 commit 直接返回），因此它接受没有新 serial 的 buffer。修复只对 Weston 生效：registry 暴露 `weston_*` 全局时，已呈现过的窗口在 100ms 宽限后按最近一次 configure 的几何映射。其他 compositor 继续严格等待 configure，因为提前附加 buffer 是协议错误：评审在临时副本上把宽限改成 1ns 后，KWin 30 次运行中有 4 次以 `xdg_surface#N: error 3: attached a buffer before configure event` 终止。最终树的 KWin registry 没有 `weston_*` 全局（`WAYLAND_DEBUG=1` 日志中 0 处），仍走严格路径并通过；Weston 的日志包含 `weston_desktop_shell` 和 `weston_capture_v1`，因此被识别并走兜底。

兜底路径最初还尝试在映射前清除最大化/全屏状态并回到保存的浮动尺寸；评审在 Weston 14.0.2 上证明这与 xdg-shell 的无内容 commit 语义冲突：Weston 在 unmap 后保留 toplevel 状态（`toplevel->added` 之后早期返回、`configured` 不复位），并按保留的最大化几何校验客户端提交，于是“恢复浮动尺寸后呈现 320x240”在 3 次运行中 3 次以 `xdg_wm_base ... error 4: xdg_surface geometry (320 x 240) does not match the configured maximized state (1024 x 736)` 终止。去掉状态改写、直接按保留的 configure（1024x736，`maximized=true`）呈现的反事实运行 3 次全部通过，提交 4 次。因此最终代码不做任何状态恢复，只复用最近一次 configure；`TestWaylandRemapFallbackKeepsRetainedConfigure` 守护该行为，headless 集成也新增了“最大化后 Hide/Show”阶段：Weston 上额外断言 remap 后仍保留 maximized 和几何，并在杀掉 compositor 前执行一次 `wl_display_roundtrip`，使排队在提交之后的显示错误在往返中暴露，而不是被测试自己的 kill 掩盖。

新增测试覆盖：轮询超时与到期兜底、未呈现窗口不启动兜底、非 Weston 不启动兜底、Hide/Close 清除宽限、多窗口取最早截止时间，以及兜底保留最近一次 configure 和最大化状态。

扩展矩阵的 scale 测试在 validation 下又暴露了一个真实缺陷：`vkBeginCommandBuffer` 报 `VUID-vkBeginCommandBuffer-commandBuffer-00050`，因为 `setScaleOnce`、`refreshText` 和 `setFrameMargin` 在 `deviceWaitIdle` 之后复用池内命令缓冲重新录制，却没有先 reset 命令池，而池没有 `VK_COMMAND_POOL_CREATE_RESET_COMMAND_BUFFER_BIT`。已在这三条路径重新录制前调用 `resetCommandPool`，并新增 `TestRendererReRecordingResetsCommands` 锁定 `wait → reset → begin → end` 顺序。修复后 Weston 与 KWin 的完整 headless 套件（Integration + ScaledOutputs + Interleaving + Performance）各运行 5 次，共 20 个测试全部通过，validation 输出 0 条 `VUID-`/`Validation Error`。测试的 compositor 清理也改为 `Setpgid` + 进程组 `SIGKILL`，避免 `unshare` 这类 wrapper 遗留真实 compositor。

调试同一问题时发现 `WAYLAND_DEBUG=1` 会让客户端在 libwayland 内段错误：手写的 fallback 接口（xdg/zxdg/wp）在创建对象的请求上把 `wl_message.types` 留成 NULL，而 libwayland 打印请求时直接下标访问该数组。已为这些请求补齐 `types` 数组（未建模的接口保留 nil 项，输出为 `[unknown]`），并新增 `TestWaylandFallbackNewIDTypes` 守护；Weston 集成入口在 `WAYLAND_DEBUG=1` 下重跑通过，见 4.2。

### 4.4 扩展 headless 矩阵与性能

scale 与多输出（`TestWaylandHeadlessScaledOutputs`，同一集成入口）：

```sh
# Weston 整数 2x 缩放
VIVA_WAYLAND_INTEGRATION=1 VIVA_WAYLAND_COMPOSITOR=/tmp/weston-run.sh \
VIVA_WAYLAND_COMPOSITOR_ARGS='--backend=headless --renderer=gl --socket={socket} --idle-time=0 --width=1024 --height=768' \
VIVA_WAYLAND_EXPECT_SCALE=2 VIVA_WAYLAND_EXPECT_OUTPUTS=1 \
VK_ICD_FILENAMES=/usr/share/vulkan/icd.d/lvp_icd.json \
  go test ./internal/platform/linux -run '^TestWaylandHeadlessScaledOutputs$' -v -count=1

# KWin 两个输出
VIVA_WAYLAND_INTEGRATION=1 VIVA_WAYLAND_COMPOSITOR=kwin_wayland \
VIVA_WAYLAND_COMPOSITOR_ARGS='--virtual --socket={socket} --width=1024 --height=768 --output-count=2 --no-lockscreen --no-global-shortcuts' \
VIVA_WAYLAND_EXPECT_SCALE=1 VIVA_WAYLAND_EXPECT_OUTPUTS=2 \
VK_ICD_FILENAMES=/usr/share/vulkan/icd.d/lvp_icd.json \
  go test ./internal/platform/linux -run '^TestWaylandHeadlessScaledOutputs$' -v -count=1
```

- Weston 14.0.2，`[output] scale=2` 配置：`PASS`，`scale=2 outputs=1 maximized 2048x1472 (configured 1024x736) submissions=2`；1024x736 的 logical configure 乘以 2 后与设备像素一致，日志中可见 `wl_surface.set_buffer_scale(2)`；与其余两个矩阵测试一起连续 10 次通过；
- KWin `--output-count=2`：`PASS`，`scale=1 outputs=2 maximized 1024x740 (configured 1024x740) submissions=3`；两个 `wl_output` 都被绑定，窗口仍正常呈现；连续 10 次通过，KWin 默认单输出同样 10 次通过；
- 评审发现的最初一版等待条件会在 KWin 的过渡 configure 上提前返回：maximize 后 KWin 先发一个 `320x240` 的瞬时 configure，随后才发 `1024x740`。现在所有矩阵等待都要求“已呈现尺寸等于最近一次完整 configure 换算后的设备尺寸”，因此有尺寸的过渡 configure 不会再被当作最终结果；Weston 的恢复/remap configure 仍可能是 `(0,0)`，这时没有可等待的几何，等待只要求 renderer 与窗口一致，恢复后的 `320x240` 由原集成测试断言。上述 10 次运行即修复后的结果；
- KWin 虚拟后端的 `--scale` 不改变客户端观测值：`--scale=2` 和 `--scale=1.25` 下仍发送 `wl_output.scale(1)` 与 `wp_fractional_scale.preferred_scale(120)`。因此 compositor 侧的整数缩放证据来自 Weston 的 2.0；小数缩放的客户端路径由 fake 回归测试和 §5 的真实会话探针覆盖，虚拟 KWin 不能作为小数缩放证据。

复杂事件交错（`TestWaylandHeadlessEventInterleaving`）：先执行三个 maximize/restore/hide/show 循环，再把 maximize、restore、hide、show 四条转换排进同一条事件循环命令，最终要求重新映射并呈现（同一 compositor 环境变量下用 `-run '^TestWaylandHeadlessEventInterleaving$'`）：

- Weston `PASS (0.82s)`，最终 `320x240 submissions=11`；与其余两个矩阵测试一起连续 10 次通过；
- KWin `PASS (0.69s)`，最终 `320x240 submissions=16`；连续 5 次通过。

性能测量（`TestWaylandHeadlessPerformance`，1ms 轮询；数值为本机代表值，Weston 与另外两个矩阵测试一起连续 10 次通过，KWin 连续 5 次通过）：

```sh
VIVA_WAYLAND_INTEGRATION=1 VIVA_WAYLAND_COMPOSITOR=kwin_wayland \
VIVA_WAYLAND_COMPOSITOR_ARGS='--virtual --socket={socket} --width=1024 --height=768 --output-count=2 --no-lockscreen --no-global-shortcuts' \
VIVA_WAYLAND_EXPECT_SCALE=1 VIVA_WAYLAND_EXPECT_OUTPUTS=2 \
VK_ICD_FILENAMES=/usr/share/vulkan/icd.d/lvp_icd.json \
  go test ./internal/platform/linux -run '^TestWaylandHeadless(EventInterleaving|Performance)$' -v -count=1
```

Weston 用上方 scale 命令的环境变量运行同一 `-run`。代表结果：

- Weston + lavapipe：`firstPresent=1ms maximize=106ms restore=30ms remap=103ms idleVoluntary=1 idleInvoluntary=0 requested=32 presented=3 frameWait=1ms`；
- KWin + lavapipe：`firstPresent=40ms maximize=36ms restore=16ms remap=2ms idleVoluntary=0 idleInvoluntary=0 requested=32 presented=3 frameWait=1ms`。

`requested=32` 是在一条命令中调用 32 次 `renderFrame`，随后实际提交 `presented=3`（测试断言 `1 <= presented < 32`），说明 frame callback 合并了请求；`frameWait` 是从请求到下一次呈现的等待（包含 frame callback 和 present 调用），不是驱动级 GPU wait。空闲计数来自 `/proc/self/status` 的 2 秒增量，作用于整个测试进程，不是单窗口计数。

### 4.5 限制

- Weston 14.0.2 不是发行版安装：模块通过 userns overlay 放到编译期搜索路径，`/tmp/weston-root`、`/tmp/weston-run.sh` 和 `/tmp/weston-inner.sh` 只存在于本机且未提交，换机复跑需要重新准备；restore 断言要求恢复最大化前的 320x240，KWin 和 Weston 本次都满足，但这是 compositor 行为而非协议保证，其他 compositor 重新推导普通窗口尺寸时需要先判断是客户端事务问题还是定义差异；
- remap 后的 toplevel 状态同样由 compositor 决定：Weston 保留最大化和保留的 configure，客户端按它呈现；KWin 在 remap 时重新推导为未最大化。集成测试对两者只断言“呈现的 renderer 尺寸等于窗口尺寸且提交数递增”，只在 Weston 上额外断言 remap 后仍保留 maximized 和几何；`TestWaylandRemapFallbackKeepsRetainedConfigure` 锁定兜底路径不改写保留状态；
- Weston 兜底靠 registry 的 `weston_*` 全局识别，不是正式的能力协商：若某个 Weston 构建不暴露这类全局，行为退化为等待 configure（窗口保持隐藏），不会提前附加 buffer；若其他 compositor 暴露同名全局，则会误用 Weston 路径。目前只验证了 Weston 14.0.2，其他 Weston 版本和其他 compositor 未验证；
- validation layer 不是发行版安装，且应用请求 Vulkan 1.0，未覆盖更高 API 版本的校验；测试不断言 validation 输出，只人工检查日志；
- 多输出只验证了启动时创建（KWin `--output-count=2`）；headless compositor 没有稳定的运行时增删 output 接口，KScreen 会作用到锁定的真实会话，因此运行时热插拔未执行；KWin 虚拟后端的 `--scale` 不影响客户端观测值（见 4.4），小数缩放只有真实会话探针和 fake 测试证据；
- 未覆盖 GNOME、Sway/wlroots、IBus、拖放、AT-SPI、session bus/portal 真实重启和 device loss 注入：本机没有这些软件或可用注入层，KDE 会话锁定；性能测量已由 4.4 覆盖，但只是进程内代理指标，不含驱动级 GPU wait；真实驱动只执行了第 5 节的先行片段；
- F1 使原生 race 门禁不是确定性的，结果按第 2 节记录，不视为已修复。

## 5. Task 8 先行片段

在 Weston 之外，用同一集成入口补了可执行的驱动和真实会话片段：

- **NVIDIA（nvidia_icd，RTX 5070 Ti Laptop）+ validation**：KWin 虚拟后端 `PASS (0.74s)`，`initial 320x240, maximized 1024x740, restored 320x240, remapped 320x240, maximized remap 320x240, submissions 9`；Weston 虚拟后端 `PASS (0.67s)`，`initial 320x240, maximized 1024x736, restored 320x240, remapped 320x240, maximized remap 1024x736, submissions 6`；两种情况下 loader 输出都确认实例/设备层插入，没有 `VUID-`/`Validation Error`。
- **lavapipe**：`PASS`，见 4.2。
- **Intel（intel_icd，RPL-S）+ KWin 虚拟后端**：首次呈现失败，compositor 报 `[destroyed object]: error 7: importing the supplied dmabufs failed`，客户端把它作为 `Run` 错误返回并完成清理；关闭 validation 后同样复现。同一 ICD 的 `vkcube --wsi wayland` 在真实会话中启动并创建了设备，12 秒内没有报错；但会话锁定，无法确认它是否真正呈现，因此不能用它证明真实 KWin 能导入 Intel dmabuf。该失败目前只确认发生在 KWin 虚拟后端的导入路径，尚未确立为客户端缺陷，根因未隔离；本地不做静默回退。
- **真实 KDE 会话**：会话当前锁定（`LockedHint=yes`，`kscreenlocker_greet` 运行）。临时会话模式探针能连接 `wayland-0`，在 125% 缩放下完成首次映射并提交首帧（320x240 逻辑 → 400x300 设备，客户端提交计数 1，未收到协议错误）；随后 maximize 停在 `framePending=true`，锁屏下 KWin 不为不可见 surface 发送 frame callback（推断），resize 事务无法完成，探针已回退。IBus 守护进程也未运行。

未执行：GNOME、Sway/wlroots、运行时多输出热插拔、device loss 注入、IBus/拖放/AT-SPI、session bus/portal 真实重启。scale、多输出启动、事件交错和性能测量见 4.4；真实桌面验收需要解锁的真实会话或一台可控制的测试机。

## 6. 后续记录方式

完成对应任务后删除其 finding，并同步任务清单和验证证据。第 2 节只保留仍未解决的问题和未执行验证；§4.3 记录本轮验证中暴露并修复的失败，作为本轮环境结论，修复进入主干后不再作为 finding 保留。
