# TrueNAS Upstream Bugs

Bug reports found during this project's investigation that should be filed
with iXsystems (TrueNAS developers) so they can be fixed upstream.

Each report is self-contained: filing path, severity, component, reproduction
steps, expected vs. actual behaviour, evidence, suspected cause, and proposed
fix. Written so you can paste the body of one directly into a TrueNAS Jira
ticket or a dev-forum post without editing.

| Ticket | File | Status | Filed | Short description |
|--------|------|--------|-------|-------------------|
| [JHNF-729](https://ixsystems.atlassian.net/browse/JHNF-729) | [`truenas-role-recursion.md`](truenas-role-recursion.md) | Filed — awaiting triage | 2026-04-14 | `RoleManager.roles_for_role()` has no cycle detection; saving a custom privilege with a meta-role alongside its children bricks authentication for every user in that privilege. Python `RecursionError`, recovery requires `midclt`. |
| [JHNF-730](https://ixsystems.atlassian.net/browse/JHNF-730) | [`truenas-upload-role-gap.md`](truenas-upload-role-gap.md) | Filed — awaiting triage | 2026-04-14 | `/_upload` HTTP endpoint ignores `FILESYSTEM_DATA_WRITE` role and returns 403 unless the user is in `builtin_administrators`. Inconsistent with the JSON-RPC `filesystem.put` method which the role is documented to cover. |

Both bugs were verified empirically on **TrueNAS SCALE 25.10.1** (Goldeye) against a live test system. Stack traces, role inventories, and A/B comparisons between a working (FULL_ADMIN) and failing (scoped roles) user are included in each report.
