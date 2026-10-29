# Wayland 当前修复实施任务

- 创建日期：2026-10-29
- 架构基线提交：`f90e3bc`（`docs(wayland): define protocol-correct target architecture`）
- 架构 gate：详细审查发现的问题已修正；连续两轮独立 gate review 均为 PASS
- 目标架构：[01-architecture-overview.md](01-architecture-overview.md)
- 未解决清单：[08-linux-wayland-review-findings.md](08-linux-wayland-review-findings.md)
- 完整审查：[09-current-implementation-review.md](09-current-implementation-review.md)

## 执行规则

1. 按 Task 1 → Task 6 顺序实施，不在前一个任务提交前开始下一个任务。
2. 每个任务必须包含实现和对应回归测试。
3. 每个任务完成后进行独立 review；发现阻断问题则修复并重新 review，PASS 后才提交。
4. 每个任务单独提交，提交信息包含 `Task N`，提交哈希以 `git log` 为准。
5. 每个任务至少执行 gofmt、`git diff --check`、Linux 交叉 vet 和 Linux 测试包交叉编译。
6. 当前主机没有 Linux 运行环境、Docker 或 QEMU；静态检查和交叉编译不得写成真实 Linux/compositor/GPU 验收。
7. macOS 基线测试已有 `TestDesktopThemeDetectionAndStyling` 失败，后续需区分该既有失败与新增回归。

## Task 1：修复 oldSwapchain 重复销毁

- 状态：已提交
- Review：PASS（独立只读审查）
- 目标：`vkCreateSwapchainKHR` 失败时，退休的旧 swapchain 恰好销毁一次。
- 主要文件：
  - `internal/platform/linux/vulkan_linux.go`
  - `internal/platform/linux/api_linux_test.go`
- 实施要点：统一局部失败分支与 deferred cleanup 的销毁所有权，保留成功重建和部分资源清理行为。
- 验收条件：
  - 非零 oldSwapchain + 创建失败时 destroy 次数为 1；
  - 成功重建仍只销毁一次旧 handle；
  - 新 swapchain 创建后的部分失败清理不回归。

## Task 2：在受控帧边界恢复 SUBOPTIMAL swapchain

- 状态：已提交
- Review：PASS（独立只读审查）
- 目标：当前 SUBOPTIMAL 帧可以完成，但随后必须消费标志并在受控边界重建，不能永久忽略。
- 主要文件：
  - `internal/platform/linux/vulkan_linux.go`
  - `internal/platform/linux/api_linux_test.go`
- 实施要点：避免同帧无限重建；重建失败保留错误并进入现有恢复/应用错误通道。
- 验收条件：
  - acquire=SUBOPTIMAL 后当前帧完成并安排一次重建；
  - present=SUBOPTIMAL 后安排一次重建；
  - 重建成功后清除标志；
  - 重建失败返回错误，不形成忙循环。

## Task 3：处理 eventfd 唤醒写入 EINTR

- 状态：已提交
- Review：PASS（独立只读审查）
- 目标：命令入队后，写入被信号打断不能导致唯一唤醒丢失。
- 主要文件：
  - `internal/platform/linux/wayland_loop.go`
  - `internal/platform/linux/api_linux_test.go`
- 实施要点：仅对写入前的 EINTR 重试；区分 EAGAIN；命令仍只执行一次。
- 验收条件：
  - 首次写入 EINTR、随后成功时命令可被唤醒并执行；
  - 不引入非 EINTR 错误的无限重试；
  - 现有 waker、close 和 post-without-windows 测试保持兼容。

## Task 4：传播 frame-margin 变化错误

- 状态：已提交
- Review：PASS（独立只读审查）
- 目标：装饰、最大化和取消最大化路径不再吞掉 `setFrameMargin` 错误。
- 主要文件：
  - `internal/platform/linux/window_wayland.go`
  - `internal/platform/linux/wayland_window_test.go`
- 实施要点：错误进入 `app.fail`；失败后停止当前状态推进和后续 configure/present。
- 验收条件：
  - server-side decoration 失败可到达应用错误；
  - client-side decoration 失败可到达应用错误；
  - maximize 和 unmaximize 失败可到达应用错误；
  - 成功路径不改变现有 margin 行为。

## Task 5：修复初始 configure 与 Show 重新映射协议序列

- 状态：待开始
- Review：未进行
- 目标：首次映射和 newly-unmapped 后的重新映射都遵守 empty commit → configure → ACK → buffer commit。
- 主要文件：
  - `internal/platform/linux/window_wayland.go`
  - `internal/platform/linux/wayland_window_test.go`
  - 必要时调整协议 fake
- 实施要点：
  - 检查首次 `displayRoundtrip` 错误并保留根因；
  - 首次 configure 未完成前不得 present；
  - Show 只发起 empty commit并等待 configure，不直接 render/present；
  - Show 后、configure 前的 Waker 不得绕过门限；
  - frame callback 不得在等待 configure 时成为孤立 pending。
- 验收条件：
  - roundtrip 失败会清理并返回错误；
  - 未收到首次 configure 不会 present；
  - Hide/Show 协议事件顺序正确；
  - configure 到达后才执行 ACK 和 buffer present；
  - Show/configure 竞态、重复 Show 和 Close 不破坏生命周期。

## Task 6：分离 scene apply 与 surface commit，统一强制呈现

- 状态：待开始
- Review：未进行
- 依赖：Task 5 已提交。
- 目标：clean scene 只在无强制提交原因时跳过 present；configure/scale/remap/rebuild 即使 clean 也正确提交。
- 主要文件：
  - `internal/platform/linux/window_wayland.go`
  - `internal/platform/linux/wayland_frame.go`
  - `internal/platform/linux/wayland_scale.go`
  - 相关测试
- 实施要点：
  - 拆分场景准备和实际 present/commit；
  - 只有确定实际提交时才建立对应 frame callback；
  - 集中 configure ACK 和强制提交原因；
  - 消除 scale 路径重复 present；
  - 保持 frame 背压与 Waker 合并行为。
- 验收条件：
  - 普通 clean redraw 不 present，也不留下 framePending；
  - clean remap/resize/scale 仍提交实际 buffer；
  - configure serial ACK 与对应提交一致；
  - present/apply 失败不会留下孤立 callback 或报告成功几何；
  - 后续 Waker 可以继续驱动呈现。

## 后续但不属于本任务列表的实施项

以下项目已在目标架构中记录，但不混入本轮六个确定性缺陷修复：libwayland prepare/read 全流程、主循环 timer/repeat 重构、完整 event-source 抽象、桌面服务生命周期抽取、托盘 keep-alive 产品决策，以及真实 Linux/compositor/GPU 分层验收环境建设。
