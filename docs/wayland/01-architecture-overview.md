# Viva Linux/Wayland 架构与开发契约

- 更新日期：2026-10-30
- 审查基线：`cb6958b` + Task 11 当前 diff
- 用途：约束后续开发；不记录已完成的修复历史
- 当前复核：[02-implementation-architecture-review.md](02-implementation-architecture-review.md)
- 未完成任务：[task.md](task.md)

## 1. 架构取舍

Viva 采用 **Wayland 原生优先、X11 后备、PureGo 动态绑定、共享 Vulkan 核心**。该主干保留，不因当前问题推倒重写。

- 强制选择 Wayland 或 X11 时，失败不得静默切换后端。
- 自动探测可从 Wayland 后备到 X11，但必须保留下层诊断；显示连接错误不得与 Vulkan/设备错误混为一谈。
- `Run` 所在 OS 线程拥有 Wayland display/proxy、窗口协议状态、Vulkan 窗口操作和服务 FD 分发。
- 后台 goroutine 只提交不可变命令或数据；允许跨 goroutine 的只读 API 使用不可变快照或明确同步。
- 场景、文字和输入路由尽量共享；FD 注册、显示连接和后端生命周期仍由各适配层负责。

## 2. 必须保持的实现契约

### 2.1 事件循环与线程所有权

所有入口都必须遵守单线程所有权，包括窗口方法、Waker、光标、剪贴板、托盘和其他系统集成，不能只在主循环内部成立。

- 跨线程操作通过命令队列和 `eventfd` 唤醒；先入队再唤醒，命令在主线程锁外执行。
- 命令执行前重新验证窗口或对象代际；关闭后的命令不得访问 proxy、renderer 或已卸载库。
- 同步系统 API 若支持后台调用，应使用主线程快速路径或结果通道，且能处理关闭并避免主线程自等待死锁。
- 若某 API 明确只支持 UI 线程，公共契约和实现断言必须一致，不能用局部互斥锁冒充完整生命周期同步。

libwayland 读取流程保持以下顺序：

```text
drain commands -> dispatch pending -> prepare read
-> flush -> poll -> read or cancel -> dispatch ready sources -> dispatch pending
```

约束：

- `prepare_read` 成功后必须配对 `read_events` 或 `cancel_read`。
- `prepare_read` 仅在 `EAGAIN` 时通过 `dispatch_pending` 排空后重试。
- `flush` 对 `EINTR` 重试，对 `EAGAIN` 监听 `POLLOUT`，恢复后取消监听。
- auxiliary source 使用 `FD/Interest/Dispatch(revents)`；`POLLERR/HUP/NVAL` 必须进入一次性注销和 owner 清理路径。
- 键盘 repeat 和 IME deadline 由最近超时驱动，不创建平行轮询 goroutine。

### 2.2 Surface、configure 与 present

表面生命周期保持 `Created / AwaitInitialConfigure / Mapped / Unmapped / Closed` 的显式状态。

- 创建 role object 后先做无 buffer 的初始 commit，异步等待 configure；不得用阻塞 roundtrip 等待窗口映射。
- Hide 使用 nil buffer + commit；重新 Show 必须再次 initial empty commit，等待新 configure 后才能附加用于映射的 buffer。
- configure serial 由同一事务持有，ACK 与对应 buffer commit 配对，不能由多条路径分别猜测 ACK 时机。
- scene dirty 只表示内容变化，不等于 surface 无需提交。initial、remap、configure、scale 和 swapchain rebuild 都必须强制呈现。
- clean scene 只有在不存在强制原因时才能跳过 present。
- frame callback 只与真实 commit 配对；失败、Hide 和 Close 不得留下孤立的 `framePending`。
- frame callback 用于绘制背压，不得成为 Quit、输入、Show 或业务退出的前置条件。

### 2.3 Vulkan WSI 与资源所有权

- `VK_ERROR_OUT_OF_DATE_KHR` 后重建并重新呈现，不能把未完成帧当作成功。
- `VK_SUBOPTIMAL_KHR` 可以完成当前帧，但新交换链也必须成功呈现一次；恢复必须有界。
- `oldSwapchain` 在创建调用后即使失败也进入退休语义，应用负责恰好销毁一次。
- surface、swapchain 和依赖资源的销毁顺序集中管理，局部错误分支与 deferred cleanup 不得重复拥有同一句柄。
- acquire/submit/present 错误进入统一恢复或应用错误通道，保留首个根因。

### 2.4 缩放、输入与 registry

缩放来源优先级保持：

1. fractional-scale + viewport，buffer scale 为 1；
2. `wl_surface.preferred_buffer_scale`；
3. surface 覆盖输出的最大 `wl_output.scale`。

scale 变化必须同步更新设备尺寸、viewport/window geometry、input region、文字比例和 swapchain，并强制呈现。`Frame` 读者只能看到完整几何代际。

registry 在 application 生命周期内持续存在。除 output 热插拔外，seat 或 data-device-manager 任一依赖迟到都必须触发 data device 的幂等初始化；重复 registry/capabilities 事件不得重复创建 proxy 或重置 offer。

### 2.5 服务故障与应用退出

- 不可恢复的 compositor、poll 或 Vulkan 错误不得表现为正常 `nil` 退出。
- 可降级的桌面服务要明确区分永久 unsupported 与临时 disconnected；临时故障采用有界退避重试，不能永久禁用，也不能每次 poll 忙重连。
- FD 的注册、注销和关闭由同一 owner 管理，重连不得遗留重复 source。
- 托盘/后台常驻尚无完整 keep-alive 产品契约；普通 watcher 不应意外延长应用生命周期，`Quit` 始终可跨线程唤醒。

## 3. 当前开发缺口

| 优先级 | 缺口 | 任务 |
| :--- | :--- | :--- |
| P1 | Weston 下的集成入口复跑未执行；KWin 虚拟后端的原生验收已完成 | Task 11 |
| P3 | 完整 compositor、真实桌面/服务重连、驱动和性能矩阵未完成 | Task 8 |

Task 11 的原生验收结果（KWin 替代环境、lavapipe、validation、失败清理和遗留的 X11 偶发拖放失败）记录在 [02-implementation-architecture-review.md](02-implementation-architecture-review.md)。Task 8 的编号沿用现有清单，不因后插入任务而重排。

## 4. 验证要求

验证结论必须分层记录：

1. Linux 原生单元测试与 race；
2. headless compositor 协议时序；
3. Vulkan validation/lavapipe 的呈现和失败清理；
4. GNOME、KDE、Sway/wlroots 的真实桌面与驱动矩阵；
5. 空闲唤醒、resize 延迟、frame 背压和 GPU wait 性能。

交叉编译、vet 和 fake 测试不能替代对应的真实验证。当前 CI 虽配置了原生 Linux 单元测试任务，但具体提交的 CI 结果仍需单独确认。Task 11 已在本机完成第 1–3 层的最小验收，其余环境仍按本节分层记录。

## 5. 推荐顺序

Task 11 只剩 Weston 下的复跑，其余最小门禁已在本机完成；当前按 **Task 8** 推进。桌面服务公共抽象应在真实重连行为验收后实施；keep-alive 先明确产品契约再实施。
