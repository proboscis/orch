---
type: issue
id: state-model-refactor
title: Separate issue resolution from run lifecycle states
status: open
---

# Separate issue resolution from run lifecycle states

Currently both issues and runs use 'resolved' state which is semantically confusing. Issues should have resolution states (open/resolved/closed), while runs should have operational lifecycle states (running/blocked/idle/waiting-user/no-agent/completed/cancelled).
