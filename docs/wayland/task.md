# Wayland 未完成任务

- 更新日期：2026-10-30
- 基线：`2e3b8ce`（`docs/linux-render-refresh-flicker`）
- 开发契约：[01-architecture-overview.md](01-architecture-overview.md)
- 当前 findings：[02-implementation-architecture-review.md](02-implementation-architecture-review.md)

本清单只保留未完成事项。P1 的 Task 11 与 P3 的 Task 8 已完成并从本清单移除；Task 8 在受限环境中无法执行的项目（GNOME/Sway、真实 ibus-daemon 会话、真实 portal/session bus 重启、device loss 注入、运行时 output 热插拔）作为限制记录在 [02-implementation-architecture-review.md](02-implementation-architecture-review.md) §4.5，不声称已验证。本机有 Fcitx5 5.1.12 提供 `org.freedesktop.IBus` 会话总线接口，但 viva 默认不连接它，见 §4.6。

## 执行规则

1. 按 P1 → P2 → P3 执行；同优先级按任务编号推进。
2. 代码任务必须包含实现和行为回归测试，不能用删除旧断言代替验证。
3. 每个任务完成后进行独立只读 review，阻断问题修复并复核；连续两次 PASS 后再提交并推送。
4. 代码任务至少执行 gofmt、`git diff --check`、Linux 交叉 vet、Linux 测试包交叉编译和当前环境可运行的相关测试。
5. 同步更新三份文档的基线、findings 和验证证据；完成项从当前清单删除，不保留修复流水账。
6. 原生单元测试、race、headless compositor、Vulkan validation 和真实桌面结果分别记录，不能互相替代。

## P1

- **Task 14：Wayland IME context 生命周期（F6）。** 窗口销毁/移除时调用 `router.ime.close()`，与 X11 的关闭路径一致；回归测试覆盖关闭窗口后守护进程收到 `Context.Close`，以及关闭后新窗口文本输入仍正常。失焦通知的去重与能力移除路径已在 Task 13 的收尾修复中完成并有测试。
- **Task 15：Vulkan SUBOPTIMAL 收敛（F5）。** 重建后再次呈现得到的 `VK_SUBOPTIMAL_KHR` 不得作为下一帧重建的依据；`OUT_OF_DATE` 的恢复路径保持不变。回归测试覆盖：fake 驱动持续返回 SUBOPTIMAL 时 `deviceWaitIdle`/swapchain 重建只发生一次；持续 `OUT_OF_DATE` 仍按现有有界恢复重建。

## 尚未排期

- 受限环境验收：GNOME、Sway/wlroots、真实 ibus-daemon/窗口级 IME 注入、拖放、AT-SPI、真实 session bus/portal 重启、device loss 注入和运行时 output 热插拔。Fcitx5 的 IBus 兼容接口可能通过 `IBUS_ADDRESS=<session bus>` 接入，但尚未在真实窗口验证，不能替代 ibus-daemon 或 native text-input 验收。需要解锁的真实会话或可控测试机，完成前不得以 fake 或 headless 结果替代。
- 桌面服务公共连接/重连抽象：待真实服务重连验收后实施，保留各后端 FD 与显示连接适配。
- 托盘和显式后台常驻：先明确 keep-alive 产品契约，避免普通 watcher 意外延长应用生命周期。
