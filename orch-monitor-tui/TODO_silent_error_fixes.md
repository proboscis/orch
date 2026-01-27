# Silent Error Handling Fixes for orch-monitor TUI

## Problem Statement
The TUI silently fails when daemon communication errors occur, showing empty tables instead of clear error messages. Users have no way to know what went wrong without checking log files.

## Goal
- Show user-facing notifications for ALL errors
- Log ALL errors to `.orch/monitor-tui.log`
- Include log file path in error messages so users know where to look

## Log File Location
```
{project_root}/.orch/monitor-tui.log
```

---

## Files to Fix (Priority Order)

### 1. proto_client.py - LOW-LEVEL CLIENT (HIGHEST PRIORITY)

The proto_client has methods that catch `ProtoDaemonError` and silently return None/False. These need to either:
- Re-raise the exception (let daemon_api.py handle it)
- Or add a callback/logging mechanism

| Line | Method | Current Behavior | Fix |
|------|--------|------------------|-----|
| 468-470 | `register_monitor()` | catches ProtoDaemonError, returns None | Re-raise or log |
| 477-480 | `unregister_monitor()` | catches ProtoDaemonError, returns False | Re-raise or log |
| 486-490 | `monitor_heartbeat()` | catches ProtoDaemonError, returns False | Re-raise or log |
| 504-510 | `get_diff_stats()` | catches ProtoDaemonError, returns None | Re-raise or log |
| 520-526 | `get_branch_state()` | catches ProtoDaemonError, returns "" | Re-raise or log |
| 535-541 | `get_diff()` | catches ProtoDaemonError, returns None | Re-raise or log |
| 552-559 | `capture_session()` | catches ProtoDaemonError, returns None | Re-raise or log |
| 584-588 | `resolve_issue()` | catches ProtoDaemonError, returns False | Re-raise or log |

**Decision**: These methods are called from daemon_api.py which properly handles exceptions. We should **remove the try/except and let exceptions propagate** for daemon_api.py to handle.

### 2. daemon_api.py - API WRAPPER (GOOD - mostly correct)

This layer properly converts exceptions to Result types. The error handling is already good. No changes needed here - the issue is proto_client.py swallowing errors before they reach daemon_api.py.

### 3. app.py - UI LAYER (MEDIUM PRIORITY)

The app layer needs to ensure ALL error Result types are shown to users.

| Line | Location | Current Behavior | Fix |
|------|----------|------------------|-----|
| 182-183 | `_log_error()` | Silent OSError catch | Add fallback notification? |
| 1437-1438 | `_update_elapsed_times()` | Silent Exception | OK - cosmetic, not critical |
| 1443-1444 | Same method | Silent KeyError | OK - cosmetic, not critical |
| 1512-1513 | `_fetch_runs()` | Silent ValueError for status | Log warning |
| 1613 | `_fetch_issue_content()` | Silent Exception | Need to check context |
| 1711 | `_do_stop_run()` | Silent Exception | Already has notify in context |
| 1744 | `_do_resolve_run()` | Silent Exception | Need to check context |
| 2046-2047 | `_fetch_runs()` IssuesDashboard | Silent ValueError | Log warning |
| 2333 | `_update_elapsed_times()` | Silent Exception | OK - cosmetic |
| 2339-2340 | Same method | Silent KeyError | OK - cosmetic |
| 2359-2360 | `_fetch_issues()` | Silent Exception | Need to check |
| 2464-2465 | `_do_close_issue()` | Silent ValueError | Log warning |
| 2500-2501 | `_do_new_run()` | Silent ValueError | Need to check |

**Key Fix**: Update `_log_error()` to return the log path, then include it in user notifications.

### 4. config.py - CONFIGURATION (MEDIUM PRIORITY)

| Line | Location | Current Behavior | Fix |
|------|----------|------------------|-----|
| 175-176 | `load()` | Silent Exception on config parse | Log and use defaults |
| 259 | `load_filters()` | Silent YAML/OSError | Log and use defaults |
| 277-278 | `save_filters()` | Silent OSError | Log warning |

### 5. widgets.py - UI WIDGETS (LOW PRIORITY)

| Line | Location | Current Behavior | Fix |
|------|----------|------------------|-----|
| 157 | `update_diff_stats()` | Silent KeyError/IndexError | OK - defensive |
| 171-172 | `_highlight_current_row()` | Silent RowDoesNotExist | OK - defensive |
| 592-593 | `_setup_render_cache()` | Silent Exception | Log warning |

### 6. multiplexer.py - TERMINAL MULTIPLEXER (LOW PRIORITY)

| Line | Location | Current Behavior | Fix |
|------|----------|------------------|-----|
| 269 | `capture_pane()` | TimeoutExpired → returns "" | OK - expected behavior |
| 284 | `send_keys()` | TimeoutExpired → returns False | OK - expected behavior |
| 364 | `_run_command()` | TimeoutExpired | OK - expected behavior |

### 7. models.py - DATA MODELS (LOW PRIORITY)

| Line | Location | Current Behavior | Fix |
|------|----------|------------------|-----|
| 101 | Status.from_string() | Silent ValueError/KeyError | OK - returns UNKNOWN |
| 181 | Agent parsing | Silent TypeError/ValueError | OK - returns None |

### 8. __main__.py - ENTRY POINT (MEDIUM PRIORITY)

| Line | Location | Current Behavior | Fix |
|------|----------|------------------|-----|
| 72-83 | Config loading | Silent Exception | Show error to user |
| 159 | Layout setup | Silent Exception | Show error to user |
| 220-222 | Subprocess | Timeout/FileNotFound | Show error to user |
| 456-482 | Various subprocess | Timeout handling | OK - expected |
| 716 | Multiplexer config | InvalidMultiplexerConfigError | Already handled |

---

## Implementation Plan

### Phase 1: Fix proto_client.py (Critical Path)
1. Remove silent try/except blocks in lines 468-588
2. Let ProtoDaemonError propagate to daemon_api.py
3. daemon_api.py already handles these correctly

### Phase 2: Enhance Error Notifications
1. Update `_log_error()` in app.py to return log path
2. Update error notifications to include: "See {log_path} for details"

### Phase 3: Fix app.py Silent Catches
1. Add logging to silent catches where appropriate
2. Keep defensive catches that are truly OK (cosmetic UI updates)

### Phase 4: Fix config.py and __main__.py
1. Log config loading errors
2. Show startup errors to user clearly

---

## Error Message Template

```python
# Standard error notification pattern
log_path = _log_error(operation, error, self.config.project_root)
self.notify(
    f"{operation} failed: {error}\nSee {log_path} for details",
    severity="error",
    timeout=10
)
```

---

## Testing Checklist

After implementing fixes, test these scenarios:
- [ ] Daemon not running → Should show clear error with log path
- [ ] Daemon crashes mid-operation → Should show timeout error
- [ ] Invalid config file → Should show parse error
- [ ] Network/socket issues → Should show connection error
- [ ] All errors logged to `.orch/monitor-tui.log`
