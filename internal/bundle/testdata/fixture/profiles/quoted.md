---
id: 'quoted'
extends: "base"   # a trailing comment after a quoted value
name: "A name: with a colon"
description: plain value # trailing comment
notes: |
  A block scalar. The scan reports nothing for a key it cannot read as a
  scalar, and does not wander into the folded lines.
---
