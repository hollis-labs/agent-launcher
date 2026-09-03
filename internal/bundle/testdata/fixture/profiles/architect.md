---
id: architect
extends: base
name: Architect
description: Decides structure and boundaries, including what they rule out.
provider: claude
spec:
  slots:
    - name: role
      source: { kind: static_file, static_file: { path: ~/templates/roles/architect.md } }
  skills: [adr]
---
