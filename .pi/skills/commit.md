---
name: commit
description: Handles git commit operations. Use when user explicitly asks to commit code changes.
---

## Rules

### Principles
- Only commit when user explicitly requests it. Never auto-commit.
- When user says "commit all", stage and commit all modified files together in one commit.
- Do NOT push after commit. Leave that to the user.

### Requirements

#### title
- Use English
- Follow GitHub conventional commit prefixes:
  - `feat:`, `fix:`, `docs:`, `style:`, `refactor:`, `perf:`, `test:`, `chore:`, etc.


#### message body
- Mix Chinese and English
- Technical terms in English (e.g., `PR`, `branch`, `commit`, `merge`)

