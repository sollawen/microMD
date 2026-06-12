# EABP Receiver for pi

EABP (Embedded Agent Bridge Protocol) receiver extension for pi.

## 安装

```bash
pi install /path/to/eabp-receivers/pi
```

## 功能（M1 形态）

- 启动时在 Unix socket 上监听来自 microNeo 的上下文通知
- 按 `\n` 分帧解析 JSON 信封（EABP 协议）
- 收到 `context` 报文后通过 `ctx.ui.notify()`展示来源/文件/行号/选区/消息
- session 结束时自动清理 socket 和注册文件

## 协议版本

`eabp-1`

## 相关文档

- 协议详情：`docs/agent-comm/D2-通信协议方案.md`
- 原型验证计划：`docs/agent-comm/D3-原型验证计划.md`