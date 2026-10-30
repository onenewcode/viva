# Linux/Wayland 当前实现复核

- 日期：2026-10-30
- 基线：`4a2b96d` + Task 11 当前 diff
- 类型：只读源码复核
- 开发契约：[01-architecture-overview.md](01-architecture-overview.md)
- 后续任务：[task.md](task.md)

## 1. 结论

**宏观架构合理，不需要推倒重写；当前剩一项实现问题。** 主循环的 prepare/read、surface/configure 事务、frame-callback 背压、系统 API 的主线程调用边界和 Vulkan 恢复边界可以保留。后续工作应完成桌面服务恢复和真实验证，而不是再次 redesign 已完成的主干。

“源码确认”表示控制流中存在问题，不表示已在真实 Linux/compositor/GPU 上复现可见症状。

## 2. 当前 findings

### F1（中等）：desktop watcher 的临时失败通过两条锁存路径永久禁止恢复

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
  go test -c -o /tmp/viva-wayland-task11.test ./internal/platform/linux

GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
  go vet ./internal/platform/... ./app

git diff --check
go test ./internal/platform/...
```

当前主机 platform 测试为 302 个通过。新增 Weston/Vulkan 集成入口覆盖初始映射、Hide/Show、maximize/restore 和 compositor 断开，但仅完成交叉编译；原生 race、headless compositor 和 lavapipe/validation 均留待另一台 Linux 机器执行，Task 11 不因测试入口存在而关闭。未查询本基线的 CI 运行结果，也未修改 CI。上述成功不能作为当前 finding 已排除或真实闪烁已修复的证据。

## 5. 后续记录方式

完成对应任务后删除其 finding，并同步任务清单和验证证据。已实现修复不再保留为历史章节；仅记录仍未解决的问题、未执行验证和新的基线。
