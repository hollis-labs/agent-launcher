---
# The abstract floor. Comments are ordinary here and must not confuse the scan.
id: base
abstract: true
name: Fixture Base
provider: claude
spec:
  # An indented key that shares a name with a header key: it must be ignored,
  # because this scan does not enter nested mappings.
  name: not-the-profile-name
  slots:
    - name: standing
      source: { kind: static_file }
---

Body prose for base.
