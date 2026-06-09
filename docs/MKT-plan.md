# microNeo Marketing Plan

> Last updated: 2026-06-08

---

## 核心卖点（所有渠道统一）

**一句话：**
> The only terminal Markdown editor that renders and edits in the same window.

**扩展版：**
> Every Markdown editor splits your screen — source left, preview right.
> Terminal screens aren't wide to begin with. Splitting makes it worse.
> microNeo renders and edits in the same window. Click to edit. See the result instantly.
> Nothing else does this.

---

## 执行进度

### ✅ Day 1 — 2026-06-08（周日）

| 任务 | 状态 | 链接/说明 |
|------|------|-----------|
| 竞品调研（HN 数据、GitHub stars） | ✅ 完成 | 见附录 A |
| 确定核心卖点：同窗口渲染 + 编辑 | ✅ 完成 | 全网无竞品，验证过 |
| awesome-tuis PR | ✅ 已提交 | [PR #713](https://github.com/rothgar/awesome-tuis/pull/713)（19.3k ⭐） |
| awesome-cli-apps PR | ✅ 已提交 | [PR #1142](https://github.com/agarrharr/awesome-cli-apps/pull/1142)（19.7k ⭐） |
| awesome-modern-cli PR | ✅ 已提交 | [PR #20](https://github.com/thegdsks/awesome-modern-cli/pull/20)（358 ⭐） |
| awesome-markdown PR | ✅ 已提交 | [PR #124](https://github.com/BubuAnabelas/awesome-markdown/pull/124)（931 ⭐） |
| 中国独立开发者列表 PR | ✅ 已提交 | [PR #975](https://github.com/1c7/chinese-independent-developer/pull/975)（48.8k ⭐ 程序员版面） |
| HelloGitHub 投稿 | ✅ 已提交 | [Issue #3335](https://github.com/521xueweihan/HelloGitHub/issues/3335)（160k ⭐ 月刊） |
| 安装 gh (GitHub CLI) | ✅ 完成 | `gh auth login` as sollawen |
| 推广调研报告 | ✅ 完成 | `docs/推广调研报告.md` |

### 📋 Day 2 — 2026-06-09（周一）白天准备

| 任务 | 优先级 | 说明 |
|------|--------|------|
| **优化 README** | P0 | 去掉 "AI era" 说教，压缩 Why microNeo，首屏一句话讲清 |
| **检查 demo 截图** | P0 | 确认 microneo-demo2.png 效果够震撼，不够就重截 |
| **起草 HN Show HN 帖子** | P0 | 标题 + 正文，写好存档 |
| **起草 Reddit 帖子** | P1 | r/commandline 版 |
| **起草 V2EX 帖子** | P1 | 分享创造节点 |

### 📋 Day 2 — 2026-06-09（周一）晚上发布

| 任务 | 时间 | 说明 |
|------|------|------|
| **HN Show HN 发帖** | 北京时间 20:00-21:00 | 美东早 8-9 点，最佳时段 |
| **Reddit r/commandline** | 同上 | 趁 HN 热度同步发 |

### 📋 后续（Day 3+）

| 任务 | 说明 |
|------|------|
| V2EX 发帖 | 分享创造节点 |
| 掘金文章 | Go + 终端工具受众 |
| Homebrew formula 提交 | 提升发现性 |
| dev.to 文章 | "I forked Micro to make..." 个人故事角度 |
| awesome 列表跟进 | 检查 4 个 PR 是否被 merge |

---

## HN 发帖方案

### 标题

```
Show HN: microNeo – The only terminal Markdown editor that renders in-place
```

### 正文（草稿，Day 2 定稿）

```
I love Micro, and I love Glow. But I kept switching between them — 
edit in Micro, preview in Glow, back to Micro, back to Glow...

So I forked Micro and added a Markdown rendering layer. 
Open any .md file and you see it rendered beautifully. 
Click anywhere to edit the source. No split panes, no context switching.

[截图]

One-line install: curl -fsSL https://raw.githubusercontent.com/sollawen/microNeo/master/install.sh | sh

Works great as $EDITOR for Claude Code, Yazi, etc.
```

### HN 发帖规则提醒

- 标题以 `Show HN:` 开头
- 正文不能太长，300 字以内
- **全天回复评论**——HN 的评论互动直接影响排名
- 不要自己顶帖，不要叫朋友顶帖（HN 会检测并惩罚）
- 态度谦虚，接受批评

---

## README 优化要点（Day 2 执行）

1. **去掉 "AI era"** — HN 用户反感 AI 炒作，用实际痛点切入
2. **压缩 Why microNeo** — 5 个小节 → 2-3 句话 + 对比表格
3. **首屏一句话说明** — 先说产品是什么，再讲为什么
4. **截图放最前面** — 视觉冲击力 > 文字

---

## 推广渠道优先级

| 优先级 | 渠道 | 预期效果 | 状态 |
|--------|------|----------|------|
| 🔴 P0 | awesome-tuis | 长期每天 10-50 浏览 | ✅ PR 已提交 |
| 🔴 P0 | awesome-cli-apps | 同上 | ✅ PR 已提交 |
| 🔴 P0 | HN Show HN | 0-300+ stars | 📋 Day 2 |
| 🟡 P1 | Reddit r/commandline | 50-200 浏览 | 📋 Day 2 |
| 🟡 P1 | V2EX | 50-200 浏览 | 📋 Day 3 |
| 🟢 P2 | dev.to 文章 | ❤️ 10-50 | 📋 Week 2 |
| 🟢 P2 | 掘金文章 | 100-500 浏览 | 📋 Week 2 |
| 🟢 P2 | Homebrew formula | 发现性提升 | 📋 Week 2 |

---

## 附录 A：竞品数据

### GitHub

| 项目 | Stars | 定位 |
|------|-------|------|
| Yazi | 39.2k | 终端文件管理器 |
| Glow | 25.7k | Markdown 阅读器 |
| Micro | 25.3k | 终端编辑器 |
| frogmouth | 3.2k | Markdown 浏览器 |
| markln | 56 | Markdown 编辑器（Python，分屏） |

### Hacker News

| 项目 | Points | 成功要素 |
|------|--------|----------|
| Micro | 349 | 简洁标题 + 成熟产品 |
| Micro (第二次) | 324 | 社区已有认知 |
| Glow | 283 | 一句话说清 + 差异点 |
| Doxx | 261 | 借势 Glow + 痛点驱动 |
| Fresh | 187 | 挑战现状叙事 |
| C-edit | 168 | 复古情怀 |
| Ki Editor | 138 | 一个明确卖点 |

### microNeo 独特定位

| | microNeo | Micro | Glow | frogmouth | markln |
|--|:---------:|:-----:|:----:|:---------:|:------:|
| Markdown 渲染 | ✅ | ❌ | ✅ | ✅ | ✅ |
| 可编辑 | ✅ | ✅ | ❌ | ❌ | ✅ |
| 同一窗口 | ✅ | — | — | — | ❌ 分屏 |
| 语言 | Go | Go | Go | Python | Python |
| 单二进制 | ✅ | ✅ | ✅ | ❌ | ❌ |

> **microNeo = Micro 的编辑能力 + Glow 的渲染能力，合二为一。**
> 同窗口渲染 + 可编辑，全网无竞品。

---

## 附录 B：awesome PR 记录

| 仓库 | Stars | 维护者更新频率 | PR | 加的位置 | Merge 概率 |
|------|-------|----------------|-----|---------|-----------|
| rothgar/awesome-tuis | 19.3k | 活跃（最近 merge 6/3） | [#713](https://github.com/rothgar/awesome-tuis/pull/713) | Editors | 高 |
| **agarrharr/awesome-cli-apps** | 19.7k | 很活跃（6/4~6/7 天天 merge） | [#1142](https://github.com/agarrharr/awesome-cli-apps/pull/1142) | Text Editors + Markdown | 中高 |
| thegdsks/awesome-modern-cli | 358 | 月更（最近 merge 5/6） | [#20](https://github.com/thegdsks/awesome-modern-cli/pull/20) | Text Editors | 高 |
| **1c7/chinese-independent-developer** | **48.8k** | **很活跃（6/5~6/8 天天 merge）** | [**#975**](https://github.com/1c7/chinese-independent-developer/pull/975) | 程序员版面 | 高 |
| **521xueweihan/HelloGitHub** | **160k** | 月刊发布，审核周期 1-4 周 | [**Issue #3335**](https://github.com/521xueweihan/HelloGitHub/issues/3335) | Go 类目投稿 | 中高（需审核） |
