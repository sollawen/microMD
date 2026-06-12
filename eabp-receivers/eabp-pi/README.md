# EABP Receiver for pi

EABP (Embedded Agent Bridge Protocol) receiver extension for pi.

## 安装

```bash
pi install /path/to/eabp-receivers/eabp-pi
```

## 功能

通过 Unix socket 接收 microNeo 发来的上下文通知，根据报文内容走两条递送路径：

### 带 message 报文
- 格式化上下文引用（`@<path> :line<focus>`）+ 用户消息
- 调用 `pi.sendUserMessage(text, { deliverAs: "steer" })` 触发 LLM 对话
- streaming 时自动排队，等当前回合工具链结束后插入

### 纯上下文报文（无 message）
- 格式化上下文引用
- 调用 `ctx.ui.setEditorText(text)` 填入输入框
- 用户手动发送后才进入 LLM（不自动触发）

## 协议版本

`eabp-1`

## 相关文档

- 协议详情：`docs/agent-comm/D2-通信协议方案.md`
- 实施计划：`docs/agent-comm/D5-pi接收端实施计划.md`