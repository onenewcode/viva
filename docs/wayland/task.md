# Wayland 未完成任务

- 更新日期：2026-10-30
- 基线：`4a2b96d` + Task 11 当前 diff
- 开发契约：[01-architecture-overview.md](01-architecture-overview.md)
- 当前 findings：[02-implementation-architecture-review.md](02-implementation-architecture-review.md)

本清单只保留未完成事项。Task 8 沿用原编号；后续任务从 Task 9 递增，不重排现有任务。

## 执行规则

1. 按 P1 → P2 → P3 执行；同优先级按任务编号推进。
2. 代码任务必须包含实现和行为回归测试，不能用删除旧断言代替验证。
3. 每个任务完成后进行独立只读 review，阻断问题修复并复核通过后再提交。
4. 代码任务至少执行 gofmt、`git diff --check`、Linux 交叉 vet、Linux 测试包交叉编译和当前环境可运行的相关测试。
5. 同步更新三份文档的基线、findings 和验证证据；完成项从当前清单删除，不保留修复流水账。
6. 原生单元测试、race、headless compositor、Vulkan validation 和真实桌面结果分别记录，不能互相替代。

## P1：建立最小真实门禁

### Task 11：建立最小真实验证入口并完成原生验收

**目标：** 提供可重复的原生验证入口，并在有 Linux/compositor/Vulkan 环境时完成验收；不要求在本机启动 VM，也不修改 CI。

已实现入口：

- 原生 Linux 可执行 `go test -race ./internal/platform/linux`，覆盖系统 API 关闭并发和几何快照。
- `TestWaylandHeadlessIntegration` 可在 Weston 下验证初始映射、Hide/Show、maximize/restore 几何变化和 compositor 断开；依赖迟到由同包原生单元测试覆盖。
- 设置 `VIVA_WAYLAND_INTEGRATION=1` 后启用集成测试，由运行环境选择 lavapipe 和 Vulkan validation；默认环境跳过，不影响 fake 单元测试。

**当前验收状态：**

- 本机仅完成 gofmt、`git diff --check`、Linux 交叉 vet 和测试包交叉编译，没有执行原生集成测试。
- CI 配置保持不变，不把测试入口的存在记为真实验证通过。

**完成条件（尚未满足）：**

- 在原生 Linux 执行 race 和集成测试，记录 Weston、驱动、validation 环境、命令和结果。
- 验证 lavapipe 首次呈现、swapchain 重建和失败清理；失败项修复并复核后才能关闭 Task 11。

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
