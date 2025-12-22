---
type: issue
id: ISSUE-PLC-142
title: Add Truncating __repr__ to SingleAnnotationLetteringResult
status: resolved
assignee: codex
---

# Add Truncating __repr__ to SingleAnnotationLetteringResult

## Problem

`SingleAnnotationLetteringResult` (and its nested types) produce overwhelming output when logged or printed. The default dataclass repr includes massive data like:
- Base64-encoded images
- Polygon coordinate lists
- Other large nested structures

This makes debugging and logging difficult as the output floods the console.

## Location

- `src/placement/letter_func_replay/single_lettering_testing.py:80`

## Proposed Solution

Add custom `__repr__` methods that truncate large data:
1. Truncate base64 strings (e.g., show first 20 chars + `...[N chars]`)
2. Summarize polygon data (e.g., `Polygon(N points)`)
3. Keep the repr informative but concise

Consider adding this to the root `SingleAnnotationLetteringResult` class and any nested types that contain large data (`AnnotationSample`, `LetteringInput`, `LetteringOutput`).

## Example

```python
# Before (overwhelming)
SingleAnnotationLetteringResult(annotation=AnnotationSample(image_base64='iVBORw0KGgoAAAANSUhEUgAAA...', polygon=[[0.1, 0.2], [0.3, 0.4], ...hundreds more...]), ...)

# After (readable)
SingleAnnotationLetteringResult(annotation=AnnotationSample(image_base64='iVBORw0KG...[50000 chars]', polygon=Polygon(150 points)), ...)
```
