---
type: issue
id: orch-043
title: Fix Japanese text breaking ASCII layout in monitor issue dashboard
status: resolved
---

# Fix Japanese text breaking ASCII layout in monitor issue dashboard

## Problem

When scrolling through issues in orch monitor's issue dashboard, Japanese text causes the ASCII layout to break. The table/layout rendering doesn't properly handle the width of Japanese characters (which are typically double-width in terminal display).

## Expected Behavior

The issue dashboard should maintain proper column alignment and layout regardless of whether issue titles or content contain Japanese (or other CJK) characters.

## Likely Cause

Japanese characters are displayed as double-width in terminals, but the code may be counting them as single-width characters when calculating column widths and layout.

## Suggested Fix

Use a library or function that properly calculates the display width of strings containing multi-byte/double-width characters (e.g., using runewidth or similar approach in Go).
