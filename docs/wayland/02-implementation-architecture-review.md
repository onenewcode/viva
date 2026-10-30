# Linux/Wayland 当前实现复核

- 日期：2026-10-30
- 基线：`61a78c8` + Task 12 当前 diff
- 类型：只读源码复核
- 开发契约：[01-architecture-overview.md](01-architecture-overview.md)
- 后续任务：[task.md](task.md)

## 1. 结论

**宏观架构合理，不需要推倒重写；当前 diff 没有保留的源码 finding。** 主循环的 prepare/read、surface/configure 事务、frame-callback 背压、系统 API 的主线程调用边界、Vulkan 恢复边界和桌面服务重连边界可以保留。后续工作应完成真实验证，而不是再次 redesign 已完成的主干。

“源码确认”表示控制流中存在问题，不表示已在真实 Linux/compositor/GPU 上复现可见症状。

## 2. 当前 findings

当前 diff 没有已知源码阻断项。真实 session bus/portal 的终止、重启和设置更新仍未执行，不能用 fake 回归测试替代 Task 8 的真实服务恢复验收。

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
  go test -c -o /tmp/viva-wayland-task12.test ./internal/platform/linux

GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
  go vet ./internal/platform/... ./app

git diff --check
go test ./internal/platform/...
go test -race ./internal/platform/...
```

当前主机 platform 测试为 302 个通过，race 为 300 个通过；全仓普通/race 均只有既有 `app/TestDesktopThemeDetectionAndStyling` 失败（KDE/GNOME 样式识别为 0）。Task 12 新增 fake D-Bus 回归，覆盖 Wayland 与 X11 的 dial/portal-read/receive 临时失败恢复、显式即时重试、退避上限、永久 unavailable 和单一 FD source；这些 Linux 测试仅完成 amd64/arm64 交叉编译，未在原生 Linux/race 下执行。Weston/Vulkan 集成入口同样只完成交叉编译，原生 headless、lavapipe/validation 和真实服务重连留待另一台 Linux 机器执行。未查询本基线的 CI 运行结果，也未修改 CI。上述成功不能作为真实桌面、闪烁或服务恢复已验收的证据。

## 5. 后续记录方式

完成对应任务后删除其 finding，并同步任务清单和验证证据。已实现修复不再保留为历史章节；仅记录仍未解决的问题、未执行验证和新的基线。
