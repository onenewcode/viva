# Wayland 未完成任务

- 更新日期：2026-10-30
- 基线：`5b1cd1e`
- 开发契约：[01-architecture-overview.md](01-architecture-overview.md)
- 当前 findings：[02-implementation-architecture-review.md](02-implementation-architecture-review.md)

本清单只保留未完成事项。Task 8 沿用原编号；后续任务从 Task 9 递增，不重排现有任务。

## 执行规则

1. 按 P1 → P2 → P3 执行；同优先级按任务编号推进。
2. 代码任务必须包含实现和行为回归测试，不能用删除旧断言代替验证。
3. 每个任务完成后进行只读 review，阻断问题修复并复核通过后再提交。
4. 代码任务至少执行 gofmt、`git diff --check`、Linux 交叉 vet、Linux 测试包交叉编译和当前环境可运行的相关测试。
5. 同步更新三份文档的基线、findings 和验证证据；完成项从当前清单删除，不保留修复流水账。
6. 原生单元测试、race、headless compositor、Vulkan validation 和真实桌面结果分别记录，不能互相替代。

## P1：收敛实现并建立最小真实门禁

### Task 9：统一系统集成的线程所有权

**目标：** 所有 Wayland proxy 和 event-source 操作都遵守主循环生命周期，消除后台系统集成调用与关闭路径的竞争。

- 覆盖光标、剪贴板读写，并审计托盘及其他直接 proxy/FD 注册入口。
- 后台调用只提交不可变命令；同步结果通过主线程快速路径或结果通道返回。
- 主线程调用不得自等待死锁；关闭后命令应被拒绝，不能访问已销毁 proxy 或已卸载库。
- 若确有 API 必须限定 UI 线程，需同步明确公共契约和实现断言，不得隐式依赖调用习惯。

**验收：**

- 源码中不存在后台路径直接操作 Wayland proxy 或并发重建 poll FD 的入口。
- 增加后台光标/剪贴板调用与最后窗口关闭、应用退出并发的测试。
- 覆盖主线程同步调用、关闭后调用和多窗口场景；原生 race 无报告，相关单元测试通过。

### Task 10：补齐 data-device 依赖迟到的生命周期

**目标：** 初始 discovery 缺少 seat 或 data-device-manager 时，后续依赖到齐仍能使用剪贴板和拖放。

- seat 或 manager 任一可用性变化时触发幂等的 `setupDataDevice`，不能只依赖新的 capabilities 事件。
- 重复 registry/capabilities 事件不得重复创建 data device、重置 offer 或丢失现有剪贴板状态。
- 明确 teardown 与后续重新初始化的所有权，不引入重复 listener。

**验收：**

- 覆盖初始无 seat、初始无 manager、两个依赖乱序到达、重复事件、剪贴板/拖放及关闭清理。
- data device 创建恰好一次，proxy 和 listener 无泄漏或重复销毁。

### Task 11：建立合并前最小真实验证门禁

**目标：** 用最小真实环境集验证 fake/交叉编译无法证明的线程、协议和 WSI 行为。

- 原生 Linux 执行 `go test -race ./internal/platform/linux`，重点覆盖 Task 9 的关闭并发和现有几何快照测试。
- headless compositor 覆盖初始 configure、Hide/Show、连续 resize、晚期 seat 和断开。
- Vulkan validation/lavapipe 覆盖首次呈现、swapchain 重建后再次呈现和失败清理。

**验收：**

- 记录运行环境、命令、结果和未覆盖范围；上述最小集合全部实际执行。
- Task 9–10 的新测试进入会被原生执行的测试包。
- 不用交叉编译、vet 或 fake 测试代替此门禁；失败项先修复再进入后续任务。

## P2：恢复桌面服务生命周期

### Task 12：实现 desktop watcher 有界重试

**目标：** 临时 D-Bus/portal 故障不再通过 `desktopFailed` 或进程级 `portalGone` 永久禁用桌面设置监听。

- 同时处理连接/接收失败和初始 `readPortalValues` 失败，区分永久 unsupported 与临时 disconnected；`NameHasNoOwner`、`NoReply` 等服务重启状态不得形成永久锁存。
- 使用有界退避，observer 重新注册或新窗口可触发立即重试，避免每次 poll 忙重连。
- 重连前后保持单一 FD owner，注销旧 source 后再关闭，避免重复注册和 FD 复用误分发。
- 修改共享 `desktopState.portalGone` 时同步审查并回归 X11，不能只验证 Wayland。

**验收：**

- 覆盖连接失败、接收失败、瞬时 portal-read 失败后恢复、observer 重新注册、新窗口触发和永久 unavailable；Wayland 与 X11 共享路径均有回归测试。
- 重试间隔有上限且空闲时不忙唤醒；恢复后只有一个有效 FD source。
- 相关原生单元测试通过，并在 Task 8 中保留真实服务重连验收，不以“未覆盖”结束真实验证。

## P3：完成完整验收矩阵

### Task 8：补全 compositor、Vulkan、真实桌面和性能验收

**目标：** 在 Task 11 最小门禁之上完成环境覆盖；保留原任务编号。

- 扩展 headless compositor 的 scale、output 热插拔和复杂事件交错。
- 在更多驱动上执行 Vulkan validation，覆盖 device/surface loss 和有界恢复。
- 真实 GNOME、KDE、Sway/wlroots 验证分数/整数缩放、多屏、IBus、拖放和 AT-SPI。
- 受控终止并恢复 session bus/portal，验证重连后的设置更新、退避行为和单一 FD source；覆盖 Task 12 的真实服务恢复路径。
- 测量空闲唤醒、resize 延迟、frame 背压和 GPU wait。

**验收：** 每项记录环境、命令、结果和限制；可见闪烁、几何漂移、并发和性能分别提供对应证据，不以单元测试替代实机结论。

## 尚未排期

- 桌面服务公共连接/重连抽象：待 Task 12 行为稳定后实施，保留各后端 FD 与显示连接适配。
- 托盘和显式后台常驻：先明确 keep-alive 产品契约，避免普通 watcher 意外延长应用生命周期。
