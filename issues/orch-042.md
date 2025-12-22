---
type: issue
id: orch-042
title: Remove ask/answer feature from spec and implementation
status: resolved
---

# Remove ask/answer feature from spec and implementation

## Description

Remove the 'ask answer' feature from both the specification and implementation. The purpose of this feature is unclear and it adds unnecessary complexity.

## Rationale

- The feature's purpose is not well understood
- Removing unused/unclear features reduces maintenance burden
- Simplifies the codebase

## Tasks

- [ ] Identify all references to ask/answer in spec files
- [ ] Identify all ask/answer implementation code
- [ ] Remove from specification documents
- [ ] Remove from implementation
- [ ] Update any related tests
- [ ] Ensure no regressions in other functionality

## Acceptance Criteria

- [ ] All ask/answer related code removed
- [ ] All ask/answer references removed from specs
- [ ] Tests pass after removal
- [ ] No orphaned code or references remain
