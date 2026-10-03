---
name: git-commit
description: Write a clear, conventional git commit message for staged changes.
---

# Writing a commit message

1. Run `git diff --cached --stat` to see what is staged.
2. Summarize the change in one imperative line (<= 72 chars), prefixed with a
   type: `feat`, `fix`, `docs`, `refactor`, `perf`, `test`, or `chore`.
3. If the change is non-trivial, add a body explaining what and why.
4. Return the message in a fenced code block so it can be copied.

Keep it factual: describe what changed, not how hard it was.
