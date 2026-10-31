## Agent skills

### Issue tracker

本地 markdown 跟踪：`.scratch/<feature>/issues/`。见 `docs/agents/issue-tracker.md`。

### Triage labels

默认五标签：needs-triage / needs-info / ready-for-agent / ready-for-human / wontfix。见 `docs/agents/triage-labels.md`。

### Domain docs

单一上下文：根 `GLOSSARY.md` + `docs/adr/`。见 `docs/agents/domain.md`。
## Doc guardrail

文档修改后，从仓库根运行 docs\tools\check-docs.ps1（当前子目录运行 ..\docs\tools\check-docs.ps1）；退出码 0 才继续，失败先修。通过后按需 git 提交。

