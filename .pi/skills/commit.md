---
name: commit
description: Git commit with Conventional Commits prefix. Use "commit all" to stage and commit all changes.
---

# Git Commit

## Rules
- 只有用户明确命令commit的时候才执行。LLM不要自己决定执行commit
- Every commit MUST have a Conventional Commits prefix: `feat:`, `fix:`, `docs:`, `style:`, `refactor:`, `perf:`, `test:`, `chore:`, `ci:`, `revert:`, etc.
- Title in **English**, body is up to LLM
