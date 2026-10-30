# Wayland 未完成任务

- 更新日期：2026-10-30
- 基线：`4e77606`
- 开发契约：[01-architecture-overview.md](01-architecture-overview.md)
- 当前 findings：[02-implementation-architecture-review.md](02-implementation-architecture-review.md)

本清单只保留未完成事项。Task 8 沿用原编号；后续任务从 Task 9 递增，不重排现有任务。Task 11 只保留未完成的 Weston 复跑，其余原生验收证据见 [02-implementation-architecture-review.md](02-implementation-architecture-review.md)。

## 执行规则

1. 按 P1 → P2 → P3 执行；同优先级按任务编号推进。
2. 代码任务必须包含实现和行为回归测试，不能用删除旧断言代替验证。
3. 每个任务完成后进行独立只读 review，阻断问题修复并复核；连续两次 PASS 后再提交并推送。
4. 代码任务至少执行 gofmt、`git diff --check`、Linux 交叉 vet、Linux 测试包交叉编译和当前环境可运行的相关测试。
5. 同步更新三份文档的基线、findings 和验证证据；完成项从当前清单删除，不保留修复流水账。
6. 原生单元测试、race、headless compositor、Vulkan validation 和真实桌面结果分别记录，不能互相替代。

## P1：建立最小真实门禁

### Task 11：建立最小真实验证入口并完成原生验收

**已完成：** `TestWaylandHeadlessIntegration` 入口、`VIVA_WAYLAND_COMPOSITOR` 选择、原生 race 与 KWin 虚拟后端下的集成验收（首次呈现、swapchain 重建、失败清理）、lavapipe 与 validation 运行，以及首次原生执行暴露的两个测试替身缺陷修复。环境和结果见 [02-implementation-architecture-review.md](02-implementation-architecture-review.md) 第 4 节。

**未完成：** 在 Weston 下复跑同一入口。本机没有 Weston，且无网络/无 sudo 安装；该复跑完成前任务保留在清单中，不能用 KWin 结果替代 Weston 结论。

## P3：完成完整验收矩阵

### Task 8：补全 compositor、Vulkan、真实桌面和性能验收

**目标：** 在 Task 11 最小门禁之上完成环境覆盖；保留原任务编号。本机限制：真实会话锁定、没有 Weston/GNOME/Sway、IBus 未运行；已完成的驱动和会话片段见 [02-implementation-architecture-review.md](02-implementation-architecture-review.md) 第 5 节。

- 扩展 headless compositor 的 scale、output 热插拔和复杂事件交错。
- 在更多驱动上执行 Vulkan validation，覆盖 device/surface loss 和有界恢复。
- 真实 GNOME、KDE、Sway/wlroots 验证分数/整数缩放、多屏、IBus、拖放和 AT-SPI。
- 受控终止并恢复 session bus/portal，验证重连后的设置更新、退避行为和单一 FD source；覆盖 Task 12 的真实服务恢复路径。
- 测量空闲唤醒、resize 延迟、frame 背压和 GPU wait。

**验收：** 每项记录环境、命令、结果和限制；可见闪烁、几何漂移、并发和性能分别提供对应证据，不以单元测试替代实机结论。

## 尚未排期

- 桌面服务公共连接/重连抽象：待真实服务重连验收后实施，保留各后端 FD 与显示连接适配。
- 托盘和显式后台常驻：先明确 keep-alive 产品契约，避免普通 watcher 意外延长应用生命周期。
