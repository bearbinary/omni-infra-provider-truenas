# TrueNAS Upstream Bug: `RecursionError` in `role_manager.roles_for_role()` breaks auth when a custom privilege contains overlapping meta-roles

**Ticket**: [JHNF-729](https://ixsystems.atlassian.net/browse/JHNF-729)
**Status**: Filed — awaiting upstream triage
**Filed**: 2026-04-14
**Filing path**: [TrueNAS Jira](https://ixsystems.atlassian.net/) or [TrueNAS Community Forum — Software Development](https://forums.truenas.com/c/developer/27)

**Severity**: High — bricks authentication for any user in the affected privilege; recovery requires editing the privilege via `midclt` from another admin account (the affected user's own UI may also stall because the privilege page tries to re-evaluate the bad privilege).

**Component**: `middlewared/role.py`, `middlewared/plugins/account_/privilege.py`

---

## Summary

`RoleManager.roles_for_role()` in `middlewared/role.py:362-363` transitively expands a role's included roles with **no cycle detection**. If a custom privilege is saved with a combination of roles whose inclusion graph contains a cycle (e.g., a meta-role and one of its transitively-included children both listed), `compose_privilege()` enters infinite recursion and every subsequent `auth.login_*` call for any user bound to that privilege fails with:

```
error: -32001 "Method call error"
errno: 22 (EINVAL)
reason: maximum recursion depth exceeded
trace.class: RecursionError
```

Restarting `middlewared` does **not** fix it because the bad privilege config is persisted in the TrueNAS config database — middlewared reloads it on startup and the next auth attempt blows up again.

## Environment

- TrueNAS SCALE **25.10.1** (Goldeye)
- Python 3.11
- Verified on a bare-metal install with fresh config

## Steps to reproduce

1. **Create a dedicated user** with no password:
   - **Credentials → Local Users → Add**
   - Username: `test-recursion`
   - Password Disabled: yes
   - Create New Primary Group: yes
   - Save.

2. **Create an API key for this user** (Credentials → API Keys → Add → select `test-recursion`). Save the key somewhere — you'll need it to confirm the failure.

3. **Create a custom privilege** bound to the user's group with a role list that contains meta roles and their children together:

   Via `midclt`:

   ```bash
   sudo midclt call privilege.create '{
     "name": "test-recursion-privilege",
     "local_groups": [<test-recursion user group id>],
     "roles": [
       "FULL_ADMIN",
       "READONLY_ADMIN",
       "FILESYSTEM_FULL_CONTROL",
       "FILESYSTEM_DATA_READ",
       "FILESYSTEM_DATA_WRITE",
       "FILESYSTEM_ATTRS_READ",
       "FILESYSTEM_ATTRS_WRITE",
       "VM_READ",
       "VM_WRITE",
       "DATASET_READ",
       "DATASET_WRITE",
       "DATASET_DELETE"
     ],
     "web_shell": false
   }'
   ```

   (Adding `FULL_ADMIN` alongside 140+ other roles via the UI "Add all" button reproduces this equivalently; the minimal trigger is having at least one meta-role AND one of its children listed in the same privilege.)

4. **Attempt to authenticate** with the API key:

   ```bash
   curl -skX POST https://<truenas-host>/api/current \
     -H 'Content-Type: application/json' \
     -d '{"jsonrpc":"2.0","id":1,"method":"auth.login_with_api_key","params":["<the key>"]}'
   ```

   Or equivalently via the WebSocket client — any auth attempt reproduces it.

## Expected behaviour

`auth.login_with_api_key` returns a success response (or a clear authorization error). Login should not fail because of an internal recursion in the role resolver.

## Actual behaviour

The call returns HTTP 200 with a JSON-RPC error payload:

```json
{
  "jsonrpc": "2.0",
  "error": {
    "code": -32001,
    "message": "Method call error",
    "data": {
      "error": 22,
      "errname": "EINVAL",
      "reason": "maximum recursion depth exceeded",
      "trace": {
        "class": "RecursionError"
      }
    }
  },
  "id": 1
}
```

`middlewared.log` shows a stack trace that recurses through `roles_for_role` hundreds of times:

```
File "/usr/lib/python3/dist-packages/middlewared/plugins/auth.py", line 1188, in login_with_api_key
  resp = await self.login_ex(app, { ... })
File "/usr/lib/python3/dist-packages/middlewared/plugins/auth.py", line 895, in login_ex
  resp = await self.get_login_user(...)
File "/usr/lib/python3/dist-packages/middlewared/plugins/auth.py", line 1148, in get_login_user
  resp = await self.middleware.call(...)
File "/usr/lib/python3/dist-packages/middlewared/plugins/auth_/authenticate.py", line 20, in authenticate_plain
  user_token = await self.authenticate_user(pam_resp['user_info'])
File "/usr/lib/python3/dist-packages/middlewared/plugins/auth_/authenticate.py", line 125, in authenticate_user
  'privilege': await self.middleware.call('privilege.compose_privilege', privileges),
File "/usr/lib/python3/dist-packages/middlewared/plugins/account_/privilege.py", line 386, in compose_privilege
  compose['roles'] |= self.middleware.role_manager.roles_for_role(role, enabled_stig)
File "/usr/lib/python3/dist-packages/middlewared/role.py", line 362, in roles_for_role
  return set.union({role}, *[
File "/usr/lib/python3/dist-packages/middlewared/role.py", line 363, in <listcomp>
  self.roles_for_role(included_role, enabled_stig)
File "/usr/lib/python3/dist-packages/middlewared/role.py", line 362, in roles_for_role
  return set.union({role}, *[
… (recurses ~1000 times until Python's default recursion limit is hit)
RecursionError: maximum recursion depth exceeded
```

## Root cause (apparent from the stack)

`middlewared/role.py:362-363`:

```python
def roles_for_role(self, role, enabled_stig):
    return set.union({role}, *[
        self.roles_for_role(included_role, enabled_stig)
        for included_role in ...
    ])
```

The function has no `visited` set tracking roles already being expanded on the current call stack. If `role A` includes `role B` and `role B` (transitively) includes `role A`, or if a role list contains a meta-role and any of its transitive children, the recursion does not terminate.

Given roles are currently defined as a finite graph in the TrueNAS source, this should be straightforward to fix with a standard visited-set guard:

```python
def roles_for_role(self, role, enabled_stig, _visited=None):
    if _visited is None:
        _visited = set()
    if role in _visited:
        return set()
    _visited.add(role)
    return set.union({role}, *[
        self.roles_for_role(included_role, enabled_stig, _visited)
        for included_role in ...
    ])
```

## Recovery (if you hit this)

The affected privilege is persisted to disk — middleware restart doesn't help. Edit the privilege to remove the offending roles, from **another admin account** that is not bound to the broken privilege:

```bash
# Find the privilege
sudo midclt call privilege.query '[["name","=","<your-privilege-name>"]]' | jq '.[] | {id, roles}'

# Replace roles with a clean, small list (no meta-roles alongside children)
sudo midclt call privilege.update <id> '{"roles": ["READONLY_ADMIN", "VM_READ", "VM_WRITE", "DATASET_READ", "DATASET_WRITE"]}'
```

## Impact

Any site that creates custom privileges with role lists generated from the full role catalog (e.g., a naive "include everything under X namespace" script, or the UI's "add all" pattern if it ever gets added) will brick authentication for their users with no actionable error message.

## Workarounds (user-side)

- Never include `FULL_ADMIN` as a role in a custom privilege — use `builtin_administrators` group membership instead if you need admin privileges.
- Never mix a meta-role (`READONLY_ADMIN`, `FILESYSTEM_FULL_CONTROL`, `REPLICATION_ADMIN`, `SHARING_ADMIN`) with any of its transitively-included child roles in the same privilege.
- Keep custom privilege role lists small and flat (leaf roles only).

## Proposed fix

Add cycle detection to `roles_for_role()` via a visited-set parameter (snippet above). Optionally also validate on `privilege.create` / `privilege.update` to reject role lists whose graph contains a cycle — failing loudly at save time is kinder than failing silently at login time.
