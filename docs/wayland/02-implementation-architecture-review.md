# Linux/Wayland 当前实现复核

- 日期：2026-10-30
- 基线：`cb6958b` + Task 11 当前 diff
- 类型：只读源码复核 + 原生 Linux 验收
- 开发契约：[01-architecture-overview.md](01-architecture-overview.md)
- 后续任务：[task.md](task.md)

## 1. 结论

**宏观架构合理，不需要推倒重写；当前 diff 没有保留的源码 finding。** 主循环的 prepare/read、surface/configure 事务、frame-callback 背压、系统 API 的主线程调用边界、Vulkan 恢复边界和桌面服务重连边界可以保留。Task 11 的入口和 KWin 虚拟后端原生验收已完成（headless compositor + lavapipe + validation），Weston 复跑仍然缺失，因此任务不关闭；后续工作转入 Task 8 的环境矩阵，而不是再次 redesign 已完成的主干。

“源码确认”表示控制流中存在问题，不表示已在真实 Linux/compositor/GPU 上复现可见症状；真实环境结果只在第 4 节按层记录。

## 2. 当前 findings

**F1（既有，非本次引入，未修复）：真实 X11/Plasma 会话下拖放测试偶发收不到回复。**

`TestAFileDragFromAnotherClientIsAcceptedAndDropped` 在 `-race` 重复运行中约 5%–8% 失败（一次 120 次运行失败 6 次），表现为目标窗口没有收到 `XdndPosition` 或 `XdndDrop`，源端等不到 `XdndStatus`/`XdndFinished`。证据：

- 失败时目标客户端连接 `poll` 返回 `ready=0, revents=0`，而同一时刻源连接往返只需 0.3–1.2ms，X 服务端没有阻塞；
- 目标客户端 goroutine 停在事件循环的 `poll`，不是死锁在业务代码；
- 临时加入“poll 前先取 libxcb 内部队列事件”的修改后失败率不变，说明事件没有进入客户端队列；
- 在基线 `cb6958b` 的独立 worktree 中以相同命令复现；
- 换用 lavapipe（`VK_ICD_FILENAMES=lvp_icd.json`）仍然复现，与渲染驱动无关。

结论：属于 Xwayland/KWin 下跨客户端 `SendEvent` 的投递异常，客户端侧没有可验证的修复点；与 Task 11 的 Wayland 改动无关。本次只把测试中 3 处无界 channel 接收改为 10s 有界等待（新增 `nextDropEvent` 帮助函数），避免失败时整包挂起到超时。该问题保留到 Task 8 的真实桌面矩阵继续跟踪，不阻断 Task 11。

没有其他已知源码阻断项。真实 session bus/portal 的终止、重启和设置更新仍未执行，不能用 fake 回归测试替代 Task 8 的真实服务恢复验收。

## 3. 已核对的主干

本次未发现需要改变以下设计的新证据：

- `internal/platform/linux/wayland_loop.go` 的 prepare/read/cancel、flush 背压和命令唤醒；
- `internal/platform/linux/window_wayland.go` 的异步初始 configure、集中 ACK/present 和 Hide/Show 状态迁移；
- `internal/platform/linux/wayland_frame.go` 的 callback 与实际呈现配对；
- `internal/platform/linux/vulkan_linux.go` 的有界恢复、重建后再次呈现和 old-swapchain 销毁；
- `internal/platform/linux/wayland_scale.go` 的缩放优先级与几何更新。

这些结论仅表示保留主干，不表示第 4 节之外的环境验收已经完成。

## 4. 本次验证

### 4.1 环境

- Debian 13，Linux 6.12.105 amd64，Go 1.27.1；
- 真实桌面：KDE Plasma 6.3.6（KWin 6.3.6，Wayland 会话 `wayland-0`，Xwayland `DISPLAY=:1`）；
- headless compositor：`kwin_wayland --virtual`。本机没有 Weston，且无网络/无 sudo 安装，因此通过 `VIVA_WAYLAND_COMPOSITOR=kwin_wayland` 使用替代后端；Weston 特定行为未验证；
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
go test -race -count=1 -v ./internal/platform/linux   # 399 pass / 11 skip / 0 fail
```

headless compositor + lavapipe + validation 集成入口：

```sh
VIVA_WAYLAND_INTEGRATION=1 VIVA_WAYLAND_COMPOSITOR=kwin_wayland \
VK_ICD_FILENAMES=/usr/share/vulkan/icd.d/lvp_icd.json \
VK_LAYER_PATH=/tmp/vkval VK_INSTANCE_LAYERS=VK_LAYER_KHRONOS_validation VK_LOADER_DEBUG=layer \
  go test ./internal/platform/linux -run '^TestWaylandHeadlessIntegration$' -v -count=1
```

结果 `PASS (3.00s)`：

- 首次呈现：`initial 320x240`，renderer 尺寸与窗口一致且提交次数大于 0；
- swapchain 重建：maximize 到 `1024x740`、restore 回 `320x240`，每次 renderer 尺寸跟随窗口且提交次数递增；
- Hide/Show：重新映射后 `remapped 320x240` 并再次呈现（总提交 6 次）；
- 失败清理：杀掉 compositor 后 `Run` 返回非 nil 错误，窗口关闭（`state == nil`），renderer 释放（`api == nil`）；
- 本次运行的 loader 输出显示 validation layer 已作为实例层插入，捕获的输出里没有 `VUID-`/`Validation Error`。该结论来自人工检查日志，测试本身不断言 validation 输出；测试失败时打印的应用错误也记录在日志中，用于确认非 nil 退出确实来自 compositor 断开。

### 4.3 本次修复的原生失败

在这台 Linux 机器上首次原生执行时，两个既有测试失败：`TestWaylandRestoreToSavedFloatingSize`（期望最大化尺寸 1920x1080，实际 548x448）和 `TestWaylandFractionalScaleTransitionsAcrossOutputs`（scale 1.25 后 viewport 未更新）。基线上用同一命令可以复现，原因是测试窗口没有标记为 `mapped`，resize/recreate 事务被跳过。本次：

- 在两个测试中把窗口置为 `waylandSurfaceMapped`，使其真正执行恢复缩放事务；
- 为 `newWaylandPresentTestRenderer` 安装 fake swapchain 调用，使 scale/resize 事务在没有驱动时也能完成重建，而不是在原生 Linux 上 panic。

这些是测试替身缺陷，不改变生产代码。

### 4.4 限制

- 没有 Weston，headless 结论只来自 KWin 虚拟后端；Weston 下的复跑未执行，Task 11 因此保留在任务清单中。复跑时注意 restore 断言要求恢复到最大化前的 320x240，这是 KWin 的行为；若 Weston 重新推导普通窗口尺寸，需要先判断是客户端事务问题还是 compositor 定义差异；
- validation layer 不是发行版安装，且应用请求 Vulkan 1.0，未覆盖更高 API 版本的校验；测试不断言 validation 输出，只人工检查日志；
- 未覆盖 GNOME、Sway/wlroots、真实 GPU 驱动、多显示器热插拔、IBus、拖放、AT-SPI、session bus/portal 重启和性能测量，这些仍属于 Task 8；
- F1 使原生 race 门禁不是确定性的，结果按第 2 节记录，不视为已修复。

## 5. 后续记录方式

完成对应任务后删除其 finding，并同步任务清单和验证证据。已实现修复不再保留为历史章节；仅记录仍未解决的问题、未执行验证和新的基线。
