# Linux/Wayland 当前实现与架构复核

- 修订日期：2026-10-30
- 审查基准：`2c48fee` + Task 7 当前 diff
- 适用分支：`docs/linux-render-refresh-flicker`
- 目标架构：[01-architecture-overview.md](01-architecture-overview.md)
- 待实施任务：[task.md](task.md)
- 范围：Wayland 后端选择、事件循环、surface/configure/呈现、输入调度、缩放、共享 Vulkan WSI、测试与验收边界
- 类型：只读源码审查；本文只保留当前仍未解决的问题和目标差距

> 证据口径：“源码确认”表示控制流中存在对应路径，不表示已在真实 Linux/compositor/GPU 上复现可见症状。Linux 测试交叉编译和 vet 不等于原生测试、race detector 或真实环境验收。已经完成的修复不再作为 findings 保留。

## 1. 结论

**宏观架构合理，不需要推倒重写；当前主路径实现与目标设计基本一致。Tasks 1–3 已处理本轮三个具体实现问题，Task 5 已收敛后端探测副作用与错误链，Task 6 已落地标准读取流程和 auxiliary event-source，Task 7 已集中 surface/configure 状态与提交事务，剩余主要为真实验证和未排期的桌面服务抽象。**

Task 3 将 registry 保留为 application/globals 级对象，使 discovery 后的 output 增删能够进入现有跟踪路径；真实热插拔仍需后续实机验收。Task 5 将 Wayland 的全局运行时状态和 desktop watcher 延后到 `Run`，自动后备记录 Wayland 初始化原因，无 X11 时保留下层错误链。Task 6 为 prepare 成功后的每条路径配对 read/cancel，并将服务 FD 与 callback 合并为带 Interest/Dispatch 契约的 source。Task 7 用显式 surface 状态和 configure 事务取代分散字段，初始 configure 改为异步等待，open/redraw/input/resize/scale/remap 共用提交边界。

这些问题不需要改变 Wayland-first、PureGo、共享 Vulkan、eventfd 命令队列或 frame-callback 背压等主干设计。后续结构性工作主要是桌面服务公共生命周期抽象，不再重复已完成的 configure 状态机迁移。

## 2. 当前 findings

本轮没有保留的实现 findings。F1–F3 对应的实现修改均已在 Tasks 1–3 中落地，验证证据和未执行项目见第 5 节；后续 review 若发现回归，应以当前源码重新建立 finding，而不是恢复历史文本。

## 3. 架构合理性判断

### 3.1 后端与共享边界：合理

Wayland-first、强制后端不静默降级、X11 后备，以及共享场景、输入路由和 Vulkan 核心的边界可以继续保留。PureGo 动态绑定也不需要因当前问题改变。Task 5 已保留自动后备诊断和无 X11 时的下层错误链，并将全局 hook、活动应用指针及 desktop watcher 收敛到 `Run`/`close` 生命周期。

### 3.2 事件循环：标准读取流程已落地，真实时序待验收

eventfd 命令队列、主线程执行、FD 异常 readiness、flush POLLOUT 和最近 deadline 调度保持原有职责划分。Task 6 已在 poll 前排空 pending，按 EAGAIN 语义重试 prepare，并为成功 prepare 严格配对 read/cancel；服务 FD 不再通过平行切片关联 callback。Task 2 的 `Frame` 仍只读主线程发布的不可变几何快照。真实 compositor 下的协议时序仍需后续验收。

### 3.3 Surface/configure/呈现：显式状态与集中事务已落地

scene update 与 present/commit 分离、clean/forced present、frame callback 配对以及 frame-pending 时延迟提交，都是正确主干。Task 1 区分了 toplevel 待闭合、serial 待 ACK 和 hidden/frame-pending 延迟的几何所有权，Task 7 又将这些协议字段集中到显式 surface 状态和 configure 事务中，并让首次 configure 异步进入同一提交边界。真实 compositor 下的连续 configure 和 remap 时序仍需验收。

### 3.4 Vulkan WSI：恢复边界合理

有界恢复、swapchain 重建后的再次呈现以及资源集中销毁符合目标设计。当前 findings 不要求调整 Vulkan 主干；后续重点是 validation/lavapipe 和真实驱动验收。

### 3.5 缩放与多输出：主路径合理，真实热插拔待验收

fractional-scale + viewport、整数 preferred scale 和 output scale 的优先级合理。Tasks 1–2 覆盖了几何交错与跨线程快照，Task 3 又将 registry 生命周期延长至 globals teardown，使晚期 output 增删能够驱动 scale 回退；真实热插拔结果仍列入后续验收。

## 4. 尚未完成的目标架构能力

以下是目标差距，不与第 2 节的确定性实现问题混为一谈：

| 能力 | 当前缺口 |
| :--- | :--- |
| 桌面服务公共生命周期 | 协议核心部分共享，连接和重连状态机仍有后端分叉 |
| 托盘/后台常驻 | 完整 keep-alive 产品契约尚未实现 |
| 原生 Linux race 验证 | Task 2 并发快照测试已交叉编译，尚未在 Linux/race detector 下执行 |
| 真实环境验收 | headless compositor、Vulkan validation/lavapipe、真实桌面与性能矩阵未完成 |

`01-architecture-overview.md` 已同步当前基线。后续实施任务完成后，应再次更新三份 Wayland 文档的审查基准和状态，避免已解决问题重新成为待办。

## 5. 本地验证记录

审查环境为 macOS，未提供真实 Linux/Wayland/Vulkan 运行环境。以下命令成功：

```sh
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
  go test -c -o /tmp/viva-linux-platform-task7.test ./internal/platform/linux

GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
  go vet ./internal/platform/... ./app
```

当前 macOS 平台的 `go test ./internal/platform/...` 有 302 个测试通过，`go test -race ./internal/platform/...` 有 300 个测试通过，但均不覆盖 Linux 后端。全仓 `go test ./...` 为 5537 个测试通过、23 个跳过，唯一失败仍为既有 `app/TestDesktopThemeDetectionAndStyling`（KDE/GNOME 样式识别得到 0），未将该命令记为全部通过。

这些结果只证明相关 Linux 测试可交叉编译、静态检查通过。Task 7 的显式状态迁移、异步初始 configure、configure 事务、Hide/Show、关闭和失败清理测试均包含在上述 Linux 测试包中，但没有原生执行；Task 2 的 Frame 并发快照测试也未在 Linux/race detector 下重跑。本次没有执行 Linux 测试二进制、Linux race detector、compositor、Vulkan WSI/validation 或真实桌面/性能验收；原生 Linux race 验证已列入 Task 8。

## 6. 建议优先级

1. **P3：补真实验证层次**：原生 Linux race、headless compositor、validation/lavapipe、真实 GNOME/KDE/Sway 和性能矩阵。
2. **未排期：桌面服务公共生命周期**：减少 X11/Wayland 重连分叉，并先明确 keep-alive 产品契约。

最终判断：**保留现有架构主干；Tasks 1–3、Tasks 5–7 的实现及 Task 4 文档同步已反映在当前基线，但不能称为已完成原生 Linux race、真实 compositor、热插拔或 Vulkan/桌面矩阵验收。**
