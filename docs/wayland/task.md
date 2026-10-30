# Wayland 待实施任务

- 修订日期：2026-10-30
- 当前基线：`7111746`
- 目标架构：[01-architecture-overview.md](01-architecture-overview.md)
- 当前审查：[02-implementation-architecture-review.md](02-implementation-architecture-review.md)

本清单只保留尚未完成的事项。已经落地的修复、已经接入主循环的输入调度，以及已经进入 CI 的原生 Linux 平台测试，不再保留为待实施任务。

## 执行规则

1. 按 P0 → P1 → P2 → P3 实施；同一优先级内按任务编号推进。
2. 每个代码任务必须包含实现和对应回归测试；重构任务不得用删除旧断言代替行为验证。
3. 每个任务完成后进行独立只读 review；发现阻断问题必须先修复，并对修复结果重新 review，PASS 后才提交。
4. 代码任务至少执行 gofmt、`git diff --check`、Linux 交叉 vet、Linux 测试包交叉编译，以及当前环境可运行的测试。
5. Ubuntu CI 已实际执行 `go test ./internal/platform/linux`；新增测试应进入该包或其他会被原生执行的包，不能只验证交叉编译。
6. 静态检查、mock/unit 测试、headless compositor、Vulkan validation 和真实桌面验收分别记录，不得互相替代。
7. 每个任务提交前必须同步 `01`、`02` 和 `task.md`：记录基线提交与当前任务 diff、实现状态和验证证据，移除已完成 findings/待办且不重排历史任务编号，保留仍未完成的差距。Task 4 是阶段一致性检查，不替代逐任务同步。

## P0：修正当前正确性与公开契约

### Task 1：分离 compositor configure 与 scale 派生几何

- 对应 finding：F1
- 目标：scale 变化不得覆盖尚未由 `xdg_surface.configure` 闭合的 compositor 最新几何。
- 主要文件：
  - `internal/platform/linux/window_wayland.go`
  - `internal/platform/linux/wayland_scale.go`
  - `internal/platform/linux/wayland_window_test.go`
  - `internal/platform/linux/wayland_scale_test.go`
- 实施要点：
  - 区分 compositor configure 快照、scale 派生尺寸和已经提交的设备尺寸；
  - 不依赖 `resizeDeferred` 推断是否存在未闭合的 toplevel configure；
  - 明确 configure 代际，连续 configure 只提交最新有效状态；
  - scale 只改变坐标换算，不得把旧已提交几何写回 compositor pending 快照。
- 验收条件：
  - 400×300 → toplevel 800×600 → scale 变化 → xdg_surface configure 后，最终使用 800×600 对应的逻辑与新设备尺寸；
  - 上述顺序在 `resizeDeferred=false` 和 `true` 时均保持最新几何；
  - `OnResize` 收到正确尺寸，不错误重复或丢失；ACK 对应正确 serial；
  - 连续 configure/scale 交错时，旧 savedLogical 不覆盖 compositor 最新值。

### Task 2：让 Wayland `Frame()` 满足任意 goroutine 契约

- 对应 finding：F2
- 目标：`Frame()` 与 resize/scale/close 并发时无 Go 数据竞争，也不读取混搭的几何代际。
- 主要文件：
  - `internal/platform/linux/window_wayland.go`
  - `internal/platform/linux/wayland_scale.go`
  - 相关并发测试
- 实施要点：
  - 主线程发布不可变几何快照，或用明确同步保护完整读取和写入；
  - 快照必须包含一致的 width、height、margin 和 scale，不能只将其中一个字段改为 atomic；
  - 后台 `Frame()` 不直接执行 Wayland proxy 或 Vulkan 操作；
  - 保留 Wayland 无法报告全局屏幕位置时返回 `false` 的能力降级。
- 验收条件：
  - 并发 `Frame()` 与连续 resize/scale 在 race detector 下无数据竞争；
  - 每次读取得到同一代际的完整几何；
  - closed 或无法报告位置的行为符合公共接口契约。

## P1：补齐动态 output 生命周期与文档现状

### Task 3：保留 application 级 registry 并处理 output 热插拔

- 对应 finding：F3
- 目标：初次 discovery 后仍能接收真实 `global/global_remove`，动态 output 增删进入现有 scale/output 跟踪。
- 主要文件：
  - `internal/platform/linux/wayland_registry.go`
  - `internal/platform/linux/wayland_app.go`
  - `internal/platform/linux/wayland_scale.go`
  - 相关 registry/scale 测试
- 实施要点：
  - 将 registry proxy 的所有权纳入 application/globals 生命周期；
  - 在统一 teardown 中销毁 registry，保证 proxy 和 pin 恰好释放一次；
  - 新 output 建立绑定、缓存和 listener；移除 output 清理全局映射及窗口 activeOutputs；
  - output 移除后重新选择仍然有效的 scale 来源，不访问已销毁 proxy。
- 验收条件：
  - discovery 完成后到达的新 global 能被绑定并参与 scale 回退；
  - global_remove 后相关窗口不再引用旧 output，并能按剩余 output 重新计算；
  - 应用关闭与初始化失败路径均无重复销毁、遗留 listener 或回调访问已释放状态。

### Task 4：同步目标架构文档的当前状态

- 目标：使所有架构、审查和任务文档与任务完成时的 HEAD 一致。
- 主要文件：
  - `docs/wayland/01-architecture-overview.md`
  - `docs/wayland/02-implementation-architecture-review.md`
  - `docs/wayland/task.md`
  - 必要时同步 `docs/conformance.md`
- 实施要点：
  - 更新审查基准和各任务实施状态；
  - 根据 Tasks 1–3 的实际完成结果更新 configure/scale、Frame 并发和 output 生命周期状态，不预设这些问题在文档同步时仍未解决；
  - 已解决项从当前 findings 和待办中移除，未解决项保留当前证据与缺口；
  - 迁移顺序先处理已确认的正确性与公开契约，再进行后端探测、prepare/read 和通用抽象；
  - 区分源码存在、单元测试执行、真实 compositor/GPU 验收三个证据等级。
- 验收条件：
  - 已解决问题不作为当前 findings 或待实施任务重复出现；
  - 三份 Wayland 文档使用同一审查基准，当前状态、剩余问题和优先级一致。

## P2：继续目标架构收敛

### Task 5：收敛后端探测副作用与错误链

- 目标：后端自动选择保留下层诊断，区分无显示、配置错误和图形设备失败；探测阶段不留下全局副作用。
- 主要文件：
  - `internal/platform/linux/backend_select.go`
  - `internal/platform/linux/backend_select_test.go`
  - `internal/platform/linux/wayland_app.go`
  - 相关应用初始化与清理测试
- 实施要点：
  - 区分环境探测、后端初始化和后续窗口/图形设备创建，不让探测启动窗口、后台服务或遗留全局 hook；
  - 自动后备保留 Wayland 初始化失败的诊断，无可用 X11 时不得把已有但初始化失败的 Wayland 配置误报为无显示；
  - 强制模式继续保留下层错误，显示连接与 Vulkan/设备错误保持可区分；
  - 初始化失败和自动后备路径释放已创建的连接、FD 和全局状态。
- 验收条件：
  - Wayland 环境存在但初始化失败且无 DISPLAY 时，返回可识别的下层原因，而不是无条件 ErrNoGraphics；
  - 自动后备到 X11 时诊断仍可获取，强制 Wayland 不静默降级；
  - 探测失败不留下 watch hook、desktop watcher 或其他后台资源；
  - 无显示、未知 override、显示连接失败和图形设备失败均有独立错误测试。

### Task 6：补齐 libwayland prepare/read 与统一 event-source 契约

- 目标：在不改变现有命令队列、flush 背压、FD 异常处理和 deadline 行为的前提下，完成标准读取流程。
- 主要文件：
  - `internal/platform/linux/wayland_loop.go`
  - `internal/platform/linux/wayland_client.go`
  - `internal/platform/linux/wayland_app.go`
  - 相关 loop/service 测试
- 实施要点：
  - `prepare_read` 成功返回 0 后严格配对 `read_events` 或 `cancel_read`；
  - poll 前排空 pending；`prepare_read` 返回 -1 且 `errno == EAGAIN` 时继续 dispatch，其他失败进入统一错误路径；
  - 保留 flush EINTR/EAGAIN、POLLOUT 恢复和持续 poll 错误退出；
  - event-source 明确 FD、Interest、Dispatch(revents) 和关闭所有权，不使用平行切片隐式关联作为最终契约；
  - 保留主循环 IBus/repeat deadline，不重新引入独立 ticker goroutine。
- 验收条件：
  - readable、timeout、EINTR、EAGAIN、ERR/HUP/NVAL 和 cancel 路径均有行为测试；
  - eventfd 唤醒、Quit、服务 FD 清理及最近 deadline 不回归；
  - prepare/read 任一步骤失败都进入统一错误与清理路径。

### Task 7：集中 surface/configure 状态与提交事务

- 目标：用显式状态和统一事务替代继续扩张的布尔组合，明确 serial、ACK、几何、frame callback 和 present/commit 的对应关系。
- 主要文件：
  - `internal/platform/linux/window_wayland.go`
  - `internal/platform/linux/wayland_frame.go`
  - `internal/platform/linux/wayland_scale.go`
  - 相关协议 fake 测试
- 实施要点：
  - 明确 Created、AwaitInitialConfigure、Mapped、Unmapped、Closed 等状态及合法迁移；
  - configure 从 toplevel 事件开始到对应 serial ACK 为止都有唯一几何 owner；
  - open、redraw、input、resize、scale 和 remap 复用同一提交协调边界；
  - 初始 configure 可异步等待，不把长期架构建立在阻塞 roundtrip 上；
  - 保持 clean scene、强制呈现、失败清理和回调背压的既有正确行为。
- 验收条件：
  - 所有实际 buffer commit 都能追溯到合法 surface 状态和对应 configure 事务；
  - 不出现无 commit 的孤立 framePending，也不出现等待 remap configure 时提前 attach buffer；
  - 连续 configure、Hide/Show、close、apply/present 失败时状态可验证且无重复 ACK/销毁；
  - 重跑 Frame 并发 race 测试，重构后的几何快照不得重新引入数据竞争；
  - 现有呈现与生命周期回归测试继续通过。

## P3：建立真实验证层次

### Task 8：补 compositor、Vulkan 和真实桌面验收

- 目标：验证 mock/unit 无法证明的协议时序、WSI 可见结果和实际体验。
- 实施层次：
  1. headless compositor：初始 configure、事件交错、Hide/Show、resize、scale、output 热插拔和断开；
  2. Vulkan validation/lavapipe：swapchain 重建、同步、失败清理和新交换链呈现；
  3. 真实 GNOME/KDE/Sway：分数/整数缩放、多屏、IBus、拖放和 AT-SPI；
  4. 性能：空闲唤醒、resize 延迟、frame 背压和 GPU wait。
- 验收条件：
  - 每项记录运行环境、命令、结果和未覆盖范围；
  - 将上述真实验收证据及仍然存在的限制同步到 `01` 和 `02`，并完成三份文档的最终一致性检查；
  - 不用交叉编译、vet 或 fake 测试代替真实环境结论；
  - 可见闪烁、几何漂移和并发问题分别有时序或实机证据；
  - 同一基线下，已解决项不再作为当前缺口或待办出现。

## 后续但未排入当前任务序列

- 抽取桌面服务公共连接/重连生命周期，保留各后端 FD 与显示连接适配。
- 明确托盘项和显式后台常驻的 keep-alive 产品契约后再实施，避免普通 watcher 意外延长应用生命周期。
