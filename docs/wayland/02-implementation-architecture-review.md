# Linux/Wayland 当前实现复核

- 日期：2026-10-30
- 基线：`5b1cd1e`
- 类型：只读源码复核
- 开发契约：[01-architecture-overview.md](01-architecture-overview.md)
- 后续任务：[task.md](task.md)

## 1. 结论

**宏观架构合理，不需要推倒重写；当前仍有三项实现问题。** 主循环的 prepare/read、surface/configure 事务、frame-callback 背压和 Vulkan 恢复边界可以保留。后续工作应收敛入口所有权和对象生命周期，而不是再次 redesign 已完成的主干。

“源码确认”表示控制流中存在问题，不表示已在真实 Linux/compositor/GPU 上复现可见症状。

## 2. 当前 findings

### F1（较高）：系统集成入口绕过单线程所有权

目标契约要求 proxy 和窗口状态由事件循环线程独占，但：

- `internal/platform/linux/system_linux.go:394` 在调用线程直接执行 `setCursor`；
- `internal/platform/linux/wayland_cursor.go:92` 读写 application 状态并直接操作 surface、proxy 和 display；
- `internal/platform/linux/system_linux.go:356` 的剪贴板写入同样直接进入 Wayland 实现；
- `internal/platform/linux/wayland_app.go:612` 的关闭路径会销毁对象并卸载动态库。

`cursorMgr.mu` 只保护 cursor manager，不能序列化 application、proxy 和 display 的整个生命周期。若这些 API 从后台 goroutine 调用，并与最后窗口关闭或 `Run` 返回并发，存在数据竞争和访问已销毁对象的风险。若产品约定这些 API 只能在 UI 线程调用，则当前缺少明确的公共契约与实现约束。

**处理：** Task 9。优先统一投递到主循环；同步返回值需要主线程快速路径或结果通道。同步审计其他直接 proxy/event-source 入口，并补关闭并发的原生 race 测试。

### F2（中等）：data device 的两个依赖迟到都不会触发初始化

- `setupDataDevice` 只在构造时调用一次：`internal/platform/linux/wayland_app.go:131`；
- registry 可以后绑定 seat：`internal/platform/linux/wayland_registry.go:703`；
- capabilities 只初始化 pointer/keyboard：`internal/platform/linux/wayland_app.go:592`；
- data-device-manager 同样可以迟到：`internal/platform/linux/wayland_registry.go:761`；
- data device 创建要求 manager 和 seat 同时存在：`internal/platform/linux/wayland_data.go:431`。

因此初始 discovery 缺少任一依赖时，后续依赖到齐也不会创建 data device。晚期 seat 可以恢复输入，但剪贴板和拖放持续不可用；已有 seat、仅有 manager 迟到时甚至不需要新的 capabilities 事件。

**处理：** Task 10。seat 或 manager 任一可用性变化时执行幂等初始化，并覆盖初始无 seat、初始无 manager、依赖乱序到达、重复事件和 teardown 测试。

### F3（中等）：desktop watcher 的临时失败通过两条锁存路径永久禁止恢复

`syncDesktopWatch` 仅在 `!desktopFailed` 时连接（`internal/platform/linux/wayland_app.go:397`）。连接或接收失败会设置该标记：

- `internal/platform/linux/wayland_app.go:419`
- `internal/platform/linux/wayland_app.go:465`

另一条独立路径在初始 portal 读取失败时设置进程级 `desktopState.portalGone`：`internal/platform/linux/wayland_app.go:426`。该锁存会阻止后续启动（`internal/platform/linux/wayland_app.go:408`），而 `NameHasNoOwner`、`NoReply` 等当前归为 missing 的错误也可能是服务重启期间的瞬时状态（`internal/platform/linux/portal_unix.go:45`）。X11 使用同一全局锁存（`internal/platform/linux/desktop_bus_linux.go:37`）。

当前两条路径都没有恢复机制。一次瞬时 D-Bus/portal 故障会使 watcher 永久退出，不符合后续配置变化或新窗口可触发恢复的目标。

**处理：** Task 12。同时覆盖 `desktopFailed` 和 `portalGone`，区分永久 unsupported 与临时 disconnected，为临时故障增加退避重试。修改共享锁存时必须同步验证 Wayland 与 X11，并覆盖 portal-read 失败后的恢复。

## 3. 已核对的主干

本次未发现需要改变以下设计的新证据：

- `internal/platform/linux/wayland_loop.go` 的 prepare/read/cancel、flush 背压和命令唤醒；
- `internal/platform/linux/window_wayland.go` 的异步初始 configure、集中 ACK/present 和 Hide/Show 状态迁移；
- `internal/platform/linux/wayland_frame.go` 的 callback 与实际呈现配对；
- `internal/platform/linux/vulkan_linux.go` 的有界恢复、重建后再次呈现和 old-swapchain 销毁；
- `internal/platform/linux/wayland_scale.go` 的缩放优先级与几何更新。

这些结论仅表示保留主干，不表示真实环境验收已经完成。

## 4. 本次验证

审查环境为 macOS。以下命令成功：

```sh
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
  go test -c -o /tmp/viva-wayland-review.test ./internal/platform/linux

GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
  go vet ./internal/platform/... ./app

git diff --check
go test ./internal/platform/...
```

当前主机 platform 测试为 302 个通过。未执行 Linux 测试二进制、原生 race、headless compositor、Vulkan validation/lavapipe 或真实桌面/性能矩阵；也未查询本基线的 CI 运行结果。上述成功不能作为 F1 已排除或真实闪烁已修复的证据。

## 5. 后续记录方式

完成 Task 9–12 后删除对应 finding，并同步任务清单和验证证据。已实现修复不再保留为历史章节；仅记录仍未解决的问题、未执行验证和新的基线。
