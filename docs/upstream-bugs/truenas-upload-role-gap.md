# TrueNAS Upstream Bug: `/_upload` HTTP endpoint does not honor `FILESYSTEM_DATA_WRITE` role — returns 403 unless user is in `builtin_administrators`

**Ticket**: [JHNF-730](https://ixsystems.atlassian.net/browse/JHNF-730)
**Status**: Filed — awaiting upstream triage
**Filed**: 2026-04-14
**Filing path**: [TrueNAS Jira](https://ixsystems.atlassian.net/) or [TrueNAS Community Forum — Software Development](https://forums.truenas.com/c/developer/27)

**Severity**: Medium — blocks any API-key-driven file upload for non-admin users, forcing upload-capable tools to require full admin privileges and defeating the point of the granular role system.

**Component**: `/_upload` HTTP endpoint (likely `middlewared/plugins/filesystem/upload.py` or similar; endpoint is distinct from the `filesystem.put` JSON-RPC method).

---

## Summary

The `filesystem.put` JSON-RPC method is documented to require the `FILESYSTEM_DATA_WRITE` role. It cannot be called directly over WebSocket JSON-RPC because it needs a pipe-based upload; the documented workaround is to POST to the `/_upload` HTTP endpoint with a multipart body.

**A user with `FILESYSTEM_DATA_WRITE` (and all other relevant filesystem/dataset roles) receives HTTP 403 Forbidden from `/_upload`.** The same user can successfully call every JSON-RPC filesystem method (`filesystem.stat`, `filesystem.listdir`, `pool.dataset.create`, `pool.dataset.update`, `pool.dataset.delete`), proving the filesystem write roles are present and functional — they just aren't honored at the HTTP upload layer.

Adding the user to the `builtin_administrators` group (which grants the `SYS_ADMIN` account attribute) immediately fixes the 403. Neither adding any other role nor combining existing roles restores access; the gate is the account attribute, not a role.

This breaks the stated user-facing promise of the role system: `FILESYSTEM_DATA_WRITE` should let a user write file data to a TrueNAS filesystem they have access to, whether they do it via JSON-RPC or HTTP upload.

## Environment

- TrueNAS SCALE **25.10.1** (Goldeye)
- Authentication: API key via `Authorization: Bearer <key>` header on the `/_upload` POST (the documented method used by TrueNAS middleware's own `filesystem.put` flow)

## Steps to reproduce

1. **Create a test user** without root admin privileges:

   ```bash
   # Create user (via UI or midclt)
   # - username: upload-test
   # - Password Disabled: yes
   # - Create New Primary Group: yes
   ```

2. **Grant the user the documented filesystem-write role set via a custom privilege:**

   ```bash
   sudo midclt call privilege.create '{
     "name": "upload-test-privilege",
     "local_groups": [<upload-test primary group id>],
     "roles": [
       "FILESYSTEM_ATTRS_READ",
       "FILESYSTEM_DATA_READ",
       "FILESYSTEM_DATA_WRITE",
       "DATASET_READ",
       "DATASET_WRITE",
       "POOL_READ"
     ],
     "web_shell": false
   }'
   ```

   Do **not** add the user to `builtin_administrators`.

3. **Create an API key for the user** (Credentials → API Keys → Add → select `upload-test`).

4. **Create a target dataset** to upload into:

   ```bash
   sudo midclt call pool.dataset.create '{"name": "<pool>/upload-test"}'
   ```

5. **Verify the API key can call `filesystem.stat` on the target path** (confirms `FILESYSTEM_ATTRS_READ` works):

   ```bash
   curl -skX POST https://<truenas-host>/api/current \
     -H 'Content-Type: application/json' \
     -d '{"jsonrpc":"2.0","id":1,"method":"auth.login_with_api_key","params":["<api key>"]}'
   # (then over the same WebSocket or via session-replay tool)
   # Call filesystem.stat with params: ["/mnt/<pool>/upload-test"]
   # Expected: returns stat dict. Confirmed to work.
   ```

6. **Attempt to upload a test file to `/_upload`** using the same API key:

   ```bash
   echo "hello" > /tmp/probe.txt

   curl -skv \
     -H "Authorization: Bearer <api key>" \
     -F 'data={"method":"filesystem.put","params":["/mnt/<pool>/upload-test/probe.txt",{"mode":493}]};type=application/json' \
     -F 'file=@/tmp/probe.txt' \
     https://<truenas-host>/_upload/
   ```

## Expected behaviour

The upload succeeds. A user with `FILESYSTEM_DATA_WRITE` on a dataset they can write to (per `DATASET_WRITE`) should be able to write a file via the HTTP upload endpoint, because `/_upload` is documented as the HTTP-equivalent path for `filesystem.put`.

## Actual behaviour

```
HTTP/1.1 403 Forbidden
```

Response body: `403: Forbidden`.

The exact same request with an API key whose owning user is in the `builtin_administrators` group succeeds (HTTP 200, file written, correct ownership). The scoped user's API key fails every time.

## Evidence the role system thinks the user is authorized

Before the upload attempt, the same user can successfully call, via the same API key on the same WebSocket session:

| JSON-RPC method | Authorized by role | Result |
|-----------------|--------------------|--------|
| `filesystem.stat` | `FILESYSTEM_ATTRS_READ` | ✓ 200 |
| `filesystem.listdir` | `FILESYSTEM_ATTRS_READ` | ✓ 200 |
| `pool.dataset.create` | `DATASET_WRITE` | ✓ 200 |
| `pool.dataset.update` | `DATASET_WRITE` | ✓ 200 |
| `pool.dataset.delete` | `DATASET_DELETE` | ✓ 200 |
| **`/_upload` (HTTP)** | `FILESYSTEM_DATA_WRITE`? | **✗ 403** |

`auth.me` for the scoped user shows:

```json
{
  "account_attributes": ["LOCAL", "API_KEY"],
  "privilege": {
    "roles": {
      "$set": [
        "FILESYSTEM_ATTRS_READ",
        "FILESYSTEM_DATA_READ",
        "FILESYSTEM_DATA_WRITE",
        "DATASET_READ",
        "DATASET_WRITE",
        "POOL_READ",
        ...
      ]
    }
  }
}
```

`auth.me` for a user in `builtin_administrators` (whose upload works) shows:

```json
{
  "account_attributes": ["LOCAL", "API_KEY", "SYS_ADMIN"],
  "privilege": { "roles": { "$set": [...all 141 roles...] } }
}
```

The **only** difference in effective authz state between the working and failing users is the `SYS_ADMIN` account attribute, which comes from `builtin_administrators` group membership.

## Suspected cause

The `/_upload` endpoint enforces `SYS_ADMIN` (account attribute) rather than consulting the role system to check for `FILESYSTEM_DATA_WRITE` (or whichever role should gate it). This is inconsistent with how the JSON-RPC `filesystem.put` method is gated and undocumented anywhere in the TrueNAS role reference.

## Impact

Any API-driven tool that uploads files to TrueNAS (backup tools, provisioning tools like omni-infra-provider-truenas, file sync daemons, etc.) is forced to require `builtin_administrators` membership for its service account — equivalent to full admin. This:

- Defeats the granular role system's promise of principle-of-least-privilege API access.
- Forces tool operators to choose between "use FULL_ADMIN" and "pre-populate files some other way".
- Makes the role catalog misleading: `FILESYSTEM_DATA_WRITE`'s description implies it covers writing file data, not a JSON-RPC-method subset.

## Reproduction test script

The `omni-infra-provider-truenas` project maintains a one-shot Go probe at [`scripts/verify-api-key-roles/main.go`](https://github.com/bearbinary/omni-infra-provider-truenas/blob/main/scripts/verify-api-key-roles/main.go) that exercises every JSON-RPC method + the `/_upload` endpoint against a supplied API key and produces a pass/fail matrix. The matrix clearly isolates the `/_upload` failure from every other filesystem operation for a scoped role set — useful as a regression test if this is fixed.

## Proposed fix

Either:

1. **Gate `/_upload` on a role check** that consults the same `FILESYSTEM_DATA_WRITE` + `DATASET_WRITE` roles as the underlying `filesystem.put` middleware call. This is the least surprising behaviour and makes the role system self-consistent.

2. **Or**: document explicitly that `/_upload` requires the `SYS_ADMIN` account attribute (i.e., `builtin_administrators` membership), cannot be granted via roles, and add a new role (e.g., `FILESYSTEM_UPLOAD`) that third parties can grant to non-admin users. Optionally scope it per-dataset to keep least-privilege intact.

Option 1 is preferred because the role inventory already has `FILESYSTEM_DATA_WRITE` and users reasonably expect it to cover file upload as well as JSON-RPC data writes.

## Workarounds (user-side, until fixed)

- **Add the service user to `builtin_administrators`.** This is what we document for omni-infra-provider-truenas as of v0.14.6, with the caveat that it grants full admin access.
- **Pre-populate files on the filesystem out-of-band** (SSH + `scp` / `rsync`) and skip `/_upload` entirely. Requires SSH access to the TrueNAS host and breaks the "API-only" deployment model.

## Related

- [Role recursion bug](./truenas-role-recursion.md) — separate bug found during the same investigation. Unrelated root cause; related investigation.
