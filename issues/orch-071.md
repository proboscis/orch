---
type: issue
id: orch-071
title: orch monitor crash in file store scanning (scanIssues)
status: open
---

# orch monitor crash in file store scanning (scanIssues)

The orch monitor randomly crashes with a stack trace pointing to 'scanIssues' and 'filepath.Walk'.

Stack trace indicates an issue in 'internal/store/file/file.go:89' (parseIssueFile) or related file operations.

Symptoms:
- Random crashes.
- Stack trace involving 'sync.(*WaitGroup).Wait', 'bubbletea', and 'syscall.syscall' inside 'filepath.Walk'.

Possible causes:
- File descriptor exhaustion?
- Race condition in file store access?
- Concurrency issue with Bubble Tea command loop?

Stack trace excerpt:
/opt/homebrew/Cellar/go/1.25.3/libexec/src/runtime/sema.go:114 +0x38
sync.(*WaitGroup).Wait(0x1400058a3e0)
github.com/charmbracelet/bubbletea.(*Program).execBatchMsg(0x14000000dc0, {0x14000204fa0, 0x2, 0x0?})
...
goroutine 83 [runnable]:
syscall.syscall(...)
...
github.com/s22625/orch/internal/store/file.(*FileStore).parseIssueFile(...)

