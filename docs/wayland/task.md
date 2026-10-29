# Wayland 当前修复实施任务

- 创建日期：2026-10-29
- 架构基线提交：`d5f4157`（`docs(wayland): review implementation architecture`）
- 架构 gate：连续两轮独立只读 review 均为 PASS
- 目标架构：[01-architecture-overview.md](01-architecture-overview.md)
- 完整审查：[02-implementation-architecture-review.md](02-implementation-architecture-review.md)

## 执行规则

1. 按 Task 1 → Task 11 顺序实施，不在前一个任务提交前开始下一个任务。
2. 每个任务必须包含实现和对应回归测试；CI/文档任务按其实际交付物验收。
3. 每个任务完成后进行独立只读 review；发现阻断问题则修复并重新 review，PASS 后才提交。
4. 每个任务单独提交，提交信息包含 `Task N`，提交哈希以 `git log` 为准。
5. 每个代码任务至少执行 gofmt、`git diff --check`、Linux 交叉 vet 和 Linux 测试包交叉编译；可运行的共享测试必须实际执行。
6. 当前主机没有 Linux 运行环境、Docker、QEMU 或已配置的远程 Linux 测试环境；静态检查和交叉编译不得写成真实 Linux/compositor/GPU 验收。
7. macOS 基线 `go test ./app` 已有 `TestDesktopThemeDetectionAndStyling` 失败（255 通过、1 失败、1 跳过），后续需区分该既有失败与新增回归。
8. 任一任务 review 发现的问题必须先修复再提交，不得通过扩大后续任务范围来绕过当前门禁。

## Task 1：统一输入与 focus-loss 的 Wayland 呈现入口

- 状态：已提交
- Review：PASS（独立只读审查）
- 目标：消除 dirty input/focus-loss frame 绕过 hidden/remap、configure ACK 和 frame callback 事务的直接 present 旁路。
- 主要文件：
  - `internal/platform/linux/window_wayland.go`
  - `internal/platform/linux/wayland_keyboard.go`（仅需要时）
  - `internal/platform/linux/wayland_window_test.go`
- 实施要点：
  - 保留 clean frame 不呈现的行为；
  - hidden/awaitingConfigure 时只保留后续 redraw，不提交 buffer；
  - framePending 时合并更新，不建立无 callback 的提交；
  - 输入造成的实际呈现与 configure/geometry/ACK 使用同一事务边界。
- 验收条件：
  - Hide/Show 后、remap configure 前的 focus-loss 不 present；
  - keyboard leave 和普通 dirty input 不绕过相同门限；
  - 可见且无 pending 的 dirty input 正常呈现并配对 callback；
  - clean input 不增加提交或孤立 `framePending`。

## Task 2：修复 frame pending 期间 resize/scale 的 redraw 与 callback 事务

- 状态：已提交
- Review：PASS（首轮 FAIL 的 remap 残留和失败测试契约问题已修复，复审通过）
- 依赖：Task 1 已提交。
- 目标：旧 frame pending 时发生 resize/scale，不清除尚未执行的 redraw，不在没有新 callback 的情况下直接提交。
- 主要文件：
  - `internal/platform/linux/window_wayland.go`
  - `internal/platform/linux/wayland_frame.go`
  - `internal/platform/linux/wayland_window_test.go`
- 实施要点：明确 pending resize 的延迟提交语义，保留 configure serial、forcePresent 和 needsRedraw，直到旧 callback 到达后的统一事务。
- 验收条件：
  - pending → Waker → resize（含 `OnResize=nil`）后，旧 callback 能驱动最新内容呈现；
  - resize/scale 不在 pending 期间产生无 callback 的新 commit；
  - 已排队的 OnResize/OnRedraw 不错误重复或丢失；
  - 现有 present/apply 失败事务测试不回归。

## Task 3：收敛 IBus/AT-SPI FD 生命周期并处理异常 readiness

- 状态：已提交
- Review：PASS（首轮 FAIL 的 AT-SPI 自关闭、批量回复失败和关闭测试缺口已修复，复审通过）
- 目标：异常路径先关闭连接时也能移除原注册 FD，消除 POLLNVAL 忙循环，并补齐正常 IBus 关闭。
- 主要文件：
  - `internal/platform/linux/wayland_loop.go`
  - `internal/platform/linux/wayland_app.go`
  - `internal/platform/linux/atspi_linux.go`
  - `internal/platform/linux/atspi_bridge.go`（仅需要时）
  - 相关测试
- 实施要点：
  - event-source 使用稳定注册身份，而不是关闭后重新查询连接 FD；
  - POLLERR/POLLHUP/POLLNVAL 进入清理或错误分发路径；
  - 正常关闭先 remove 再 `Bus.Close`，失败路径保持幂等；
  - 不改变 Portal 已正确的正常清理顺序。
- 验收条件：
  - IBus Dispatch 失败关闭 socket 后，原 FD 被移除且后续 poll 不忙循环；
  - AT-SPI 回复写失败后的同类路径可清理；
  - ERR/HUP/NVAL 不再被静默忽略；
  - 正常关闭释放 IBus 连接和 pending keys。

## Task 4：保留 pending configure 在 scale 前的最新几何

- 状态：已提交
- Review：PASS（零尺寸恢复和 helper 递归问题已修复，最终复审通过）
- 目标：configure 因旧 frame pending 而延迟时，后续 scale 不得用旧窗口尺寸覆盖待提交逻辑几何。
- 主要文件：
  - `internal/platform/linux/window_wayland.go`
  - `internal/platform/linux/wayland_scale.go`
  - `internal/platform/linux/wayland_window_test.go`
- 实施要点：区分 compositor 待确认尺寸与由 scale 派生的尺寸；延迟的是 renderer/ACK/callback/present，不是最新 configure 几何记录。
- 验收条件：
  - 400×300@1 pending 到 800×600 后，再在 callback 前 applyScale(2)，最终按 800×600 逻辑尺寸和 1600×1200 设备尺寸完成；
  - OnResize 收到最新逻辑尺寸，ACK 对应正确 serial；
  - 连续 configure 不被旧 savedLogical 尺寸覆盖。

## Task 5：在持续 deferred resize 中应用最新 redraw

- 状态：已提交
- Review：PASS（独立只读复审；失败夹具已改为验证真实的关闭生命周期）
- 依赖：Task 4 已提交。
- 目标：`OnResize=nil` 时，deferred resize 事务获取并应用最新 OnRedraw，避免连续 resize 一直呈现旧场景。
- 主要文件：
  - `internal/platform/linux/window_wayland.go`
  - `internal/platform/linux/wayland_window_test.go`
- 实施要点：在同一 resize 事务内选择 OnResize frame 或最新 OnRedraw frame，成功呈现后才消费 needsRedraw。
- 验收条件：
  - 每次 callback 前都有新 configure 的连续 resize 仍能推进最新内容；
  - OnResize 存在时不额外重复 OnRedraw；
  - apply/present 失败保留可恢复状态，不伪造 redraw 已完成。

## Task 6：让 frame callback 分配失败可恢复

- 状态：待实施
- Review：待进行
- 依赖：Task 5 已提交。
- 目标：新 `wl_callback` 创建失败时不得 ACK、present 或消费事务状态，恢复后仍能建立配对 callback 并提交。
- 主要文件：
  - `internal/platform/linux/wayland_frame.go`
  - `internal/platform/linux/window_wayland.go`
  - `internal/platform/linux/wayland_window_test.go`
- 实施要点：让 callback 获取返回成功/失败；将失败纳入现有错误和事务回滚路径，避免旧 callback 被误销毁。
- 验收条件：
  - `proxyMarshalFlags` 返回 0 时没有 ACK、present 或新 commit；
  - pendingSerial、forcePresent、needsRedraw 和旧 callback 按事务语义保留；
  - 后续分配成功只提交一次并恢复背压；
  - open、普通 redraw、resize/scale 路径一致处理。

## Task 7：修复 eventfd post/close 竞态

- 状态：待实施
- Review：待进行
- 目标：`post` 不得与 `close` 交错后写入已关闭并可能复用的 FD，关闭后的命令不得进入调用 goroutine 直接执行。
- 主要文件：
  - `internal/platform/linux/wayland_loop.go`
  - `internal/platform/linux/wayland_app.go`
  - `internal/platform/linux/api_linux_test.go`
  - `internal/platform/linux/wayland_app_test.go`
- 实施要点：同步 writer 与 FD 关闭所有权；关闭后 post 明确拒绝，不入队、不写 FD；保留 stop 唤醒和 EINTR/EAGAIN 行为。
- 验收条件：
  - writer/close 交错不会向旧 FD 写入；
  - close 后 post 不执行命令、不增长队列；
  - 应用 teardown 后外部 post 不直接在后台线程执行；
  - 现有 waker、Quit 和无窗口 post 测试保持兼容或按新契约更新。

## Task 8：将 IBus 超时和键盘 repeat 纳入主循环调度

- 状态：待实施
- Review：待进行
- 依赖：Task 3、Task 7 已提交。
- 目标：Wayland 使用最近 deadline 驱动 poll timeout，实际执行 IBus Expire，并以主循环按键代际替代 repeat ticker goroutine。
- 主要文件：
  - `internal/platform/linux/wayland_app.go`
  - `internal/platform/linux/wayland_keyboard.go`
  - `internal/platform/linux/wayland_keyboard_test.go`
  - `internal/platform/linux/wayland_app_test.go`
- 实施要点：
  - 合并 IBus deadline 与 repeat deadline，计算有限 poll timeout；
  - 到期后在主线程执行 Expire/repeat 并重新计算下一次唤醒；
  - 每次 press 创建新代，release、focus loss、窗口关闭和重新按同键均使旧代失效；
  - 不引入空转或重复 catch-up 风暴。
- 验收条件：
  - daemon hang 时 pending key 按既有两秒契约释放并回退直接输入；
  - release → 同键 press 后不会执行旧代 repeat；
  - 无 timer 时仍可持续 poll，Quit 可唤醒；
  - repeat 不再依赖独立 ticker goroutine。

## Task 9：修复 Wayland 缩放往返与 viewporter 回退

- 状态：待实施
- Review：待进行
- 目标：scale 往返不截断逻辑尺寸；缺少 viewporter 时不进入需要 viewport 的 fractional-scale 呈现路径。
- 主要文件：
  - `internal/platform/linux/wayland_scale.go`
  - `internal/platform/linux/wayland_scale_test.go`
  - `internal/platform/linux/wayland_window_test.go`
- 实施要点：统一逻辑尺寸舍入规则；确保 fractional-scale 与 viewport 作为匹配能力启用，缺失时回退 preferred_buffer_scale/output scale。
- 验收条件：
  - 401 px 在 1.0 → 1.25 → 1.0 后仍为 401 px；
  - 非整除高度和保存的浮点尺寸不回归；
  - 只有 scale manager、没有 viewporter 时不产生缩小 surface；
  - 正常 fractional + viewport 和整数回退测试保持通过。

## Task 10：在 SUBOPTIMAL 新交换链上强制呈现

- 状态：待实施
- Review：待进行
- 目标：当前 SUBOPTIMAL 帧完成并重建后，在受控恢复边界内于新交换链再呈现已有场景。
- 主要文件：
  - `internal/platform/linux/vulkan_linux.go`
  - `internal/platform/linux/api_linux_test.go`
- 实施要点：限制恢复次数，避免持续 SUBOPTIMAL/OUT_OF_DATE 忙循环；保持 oldSwapchain 唯一销毁和错误传播。
- 验收条件：
  - acquire/present SUBOPTIMAL 后发生旧交换链一次提交和新交换链一次提交；
  - 第二次 present 使用新 handle；
  - 重建失败返回错误；
  - 新交换链持续异常时有明确终止条件，不形成无限恢复。

## Task 11：让 Linux 平台测试进入 CI 并同步验收文档

- 状态：待实施
- Review：待进行
- 依赖：Task 1–10 已提交。
- 目标：现有 Linux mock/unit 测试不再只存在于源码中，仓库能力文档不再声称原生 Wayland 未实现。
- 主要文件：
  - `.github/workflows/ci.yml`
  - `docs/conformance.md`
  - 必要的测试配置
- 验收条件：
  - Ubuntu CI 实际执行 `go test ./internal/platform/linux`；
  - 不需要显示/GPU 的测试不会依赖本机 Darwin 交叉编译代替运行；
  - `docs/conformance.md` 区分“原生 Wayland 已实现”和“真实 Wayland 矩阵未验收”；
  - CI 配置和文档经独立 review 后提交。

## 后续但不属于本任务列表的实施项

以下项目继续保留在目标架构中，不与本轮确定性缺陷修复混交：libwayland prepare/read 全流程、完整显式 surface/configure 状态机、通用桌面服务重连抽象、托盘 keep-alive 产品决策，以及真实 Linux/compositor/GPU 分层验收环境建设。
