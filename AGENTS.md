## Agent skills

### Issue tracker

本地 markdown 跟踪：`.scratch/<feature>/issues/`。见 `docs/agents/issue-tracker.md`。

### Triage labels

默认五标签：needs-triage / needs-info / ready-for-agent / ready-for-human / wontfix。见 `docs/agents/triage-labels.md`。

### Domain docs

单一上下文：根 `GLOSSARY.md` + `docs/adr/`。见 `docs/agents/domain.md`。
## Doc guardrail

文档（.md/.proto/.yaml/配置）修改后，先跑 docs\tools\check-docs.ps1（退出码 0 才算通过，失败先修再继续）；通过后按需要 git 提交变更。守则以 CODING_STANDARDS.md（若存在）与三份服务方案规则为准，本块只做触发指针。

