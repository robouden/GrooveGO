# KERNEL-SPEC.md — Phase 0 Specification

**Status:** draft. Gates Phase 1 code. Frozen after the seven Phase-0 scenarios in `AnyType-VPS/GROOVE-BACKEND-PLAN.md` §5 pass on paper.

> ## §0 — The invariant
>
> **Every op must be replayable from an empty node and produce identical state.**
>
> No exception. No wall-clock in merge logic. No hidden state outside the op log. No op whose effect depends on the order of receipt — only on the causal order encoded in the op itself. Deterministic iteration over sets, sorted by stable IDs.
>
> Every PR includes a replay test for the ops it touches. A failing replay is a blocker.

---

## §1 — Conventions

### 1.1 Canonical JSON
- Encoding: **RFC 8785 / JSON Canonicalization Scheme (JCS).** Stdlib `encoding/json` is **not** sufficient. All sign/verify and hash paths route through a JCS canonicalizer.
- **No floats.** Numeric fields are signed 64-bit integers (`int64`). Decimal quantities are encoded as strings. This eliminates the JCS number-canonicalization edge cases entirely.
- **No wall-clock timestamps in fields used for merge.** Display timestamps are allowed in non-load-bearing metadata fields named `*_display_ts`; these must be ignored by all merge logic.
- Keys: snake_case, ASCII.
- Strings: UTF-8 NFC (Unicode normalization form C). The canonicalizer enforces NFC before serialization.

### 1.2 Hashes
- Algorithm: **SHA-256** (stdlib `crypto/sha256`).
- Encoding: lowercase hex (64 chars). Never raw bytes in JSON.
- `hash(x) := hex(sha256(canonicalize(x)))`.
- An "op hash" is computed over the op envelope **with the `device_sig` and `op_hash` fields removed** (so the hash is over the signed payload, not the sig itself).

### 1.3 Signatures
- Algorithm: **Ed25519** (stdlib `crypto/ed25519`). 64-byte sigs.
- Encoding: lowercase hex (128 chars).
- A signature always covers `canonicalize(payload_without_sig)`.

### 1.4 Symmetric encryption (workspace ops)
- Algorithm: **ChaCha20-Poly1305** AEAD (`golang.org/x/crypto/chacha20poly1305`).
- Key: 256 bits (32 bytes).
- Nonce: 96 bits (12 bytes), random per op. Stored alongside ciphertext.
- AAD: empty. The whole op is encrypted; routing metadata in the outer envelope is in the clear.

### 1.5 Asymmetric key wrap (workspace key → user key)
- Algorithm: **X25519 sealed box** (`golang.org/x/crypto/nacl/box`'s sealed-box construction; libsodium-compatible).
- Recipient: a user's X25519 public key (derived from a separately-managed encryption keypair, not the Ed25519 signing key — see §2.1).

### 1.6 IDs
| ID kind | Format | Source |
|---|---|---|
| `user_id` | `usr_<32-hex-chars>` | first 16 bytes of `sha256(user_root_pub)` |
| `device_id` | `dev_<32-hex-chars>` | first 16 bytes of `sha256(device_pub)` |
| `workspace_id` | `ws_<64-hex-chars>` | `hash(genesis_op)` (full 32 bytes) |
| `object_id` | UUIDv7, lowercase, with dashes | client-generated, time-ordered |
| `op_hash` | 64-hex-chars | `hash(op_payload)` |

UUIDv7 is allowed for `object_id` because the time prefix is *not* used for merge ordering — only for natural sort order in UI listings. Causal order comes from the op DAG.

---

## §2 — Identity (per Plan §2.0.1: device-first internally, user-first externally)

A user owns one or more devices. Each device has its own signing key and is authorized to act for the user via a `DeviceCert` signed by the user's root key. Verifiers always check both `device_sig(op)` and `cert(device) ← user_sig`.

### 2.1 `User` record
A user has **two** keypairs:
- **Root signing key** (Ed25519) — signs `DeviceCert`s and `KeyRotation` records. Used rarely. May live on offline storage or a hardware token.
- **Encryption key** (X25519) — receives wrapped workspace keys. Used often.

```json
{
  "kind": "User",
  "user_id": "usr_8f3c1a92b7e5d4c0a1b2c3d4e5f60718",
  "root_pub":     "<32-byte ed25519 pub, hex>",
  "encryption_pub":"<32-byte x25519 pub, hex>",
  "created_at_seq": 0,
  "label": "Rob"
}
```

`label` is a human display name. It is **never** used for identity, equality, or merge — only for UI.

`created_at_seq` is a monotonic per-user counter (starts at 0, incremented by `KeyRotation`). Used to detect stale certs.

### 2.2 `Device` record
```json
{
  "kind": "Device",
  "device_id": "dev_2a3b4c5d6e7f8091a2b3c4d5e6f70819",
  "device_pub": "<32-byte ed25519 pub, hex>",
  "platform": "linux",
  "label": "rob-laptop"
}
```

`device_pub` signs every kernel op published by this device.

### 2.3 `DeviceCert` record
A user authorizes a device to act on their behalf.

```json
{
  "kind": "DeviceCert",
  "user_id": "usr_8f3c1a92b7e5d4c0a1b2c3d4e5f60718",
  "device_id": "dev_2a3b4c5d6e7f8091a2b3c4d5e6f70819",
  "device_pub": "<echoed for fast verification>",
  "user_root_seq": 0,
  "capabilities": ["sign_ops", "wrap_keys"],
  "not_before_seq": 0,
  "not_after_seq": null,
  "user_root_sig": "<64-byte ed25519 sig over this record minus user_root_sig, hex>"
}
```

- `user_root_seq` pins which root-key generation issued the cert. After a `KeyRotation`, certs from older generations remain *verifiable* (so historical ops still validate) but no longer *valid for new ops*.
- `not_after_seq = null` means open-ended.
- Capabilities: `sign_ops` (membership and object ops); `wrap_keys` (can issue workspace-key wraps for new members on behalf of the user).

### 2.4 `KeyRotation` record
Replaces the user's root or encryption key.

```json
{
  "kind": "KeyRotation",
  "user_id": "usr_8f3c1a92b7e5d4c0a1b2c3d4e5f60718",
  "from_seq": 0,
  "to_seq": 1,
  "new_root_pub": "<new ed25519 pub, hex>",
  "new_encryption_pub": "<new x25519 pub, hex>",
  "reason": "scheduled_rotation",
  "old_root_sig": "<sig by previous root key, hex>"
}
```

The old root key signs the rotation record, proving continuity. After rotation, all devices must obtain new `DeviceCert`s referencing `to_seq`. Pre-rotation ops remain verifiable using the cached old root pub.

### 2.5 `Revocation` record
Revokes a `DeviceCert` (compromise, decommission, lost device).

```json
{
  "kind": "Revocation",
  "user_id": "usr_8f3c1a92b7e5d4c0a1b2c3d4e5f60718",
  "revokes_cert_hash": "<op_hash of the DeviceCert, hex>",
  "reason": "lost_phone",
  "user_root_seq": 0,
  "user_root_sig": "<ed25519 sig, hex>"
}
```

After a `Revocation` propagates, ops signed by that device **with timestamps after the revocation's causal frontier** are rejected. Ops signed *before* the revocation remain valid (otherwise we'd retroactively invalidate history).

### 2.6 At-rest storage
Per Plan §2.0.4. The device's Ed25519 priv key, the user's X25519 priv (held on the device that has it), and any cached workspace keys live in a Badger sub-store at `<dataDir>/identity/`, encrypted via Badger's `WithEncryptionKey` using a device-local key derived from OS keychain (Linux: secretservice / macOS: keychain / Windows: credential vault) or passphrase fallback.

---

## §3 — Workspace genesis

A workspace is created by issuing a single `Create` op. The hash of that op becomes the `workspace_id`.

```json
{
  "kind": "Create",
  "version": 1,
  "label": "Engineering",
  "founder_user_id": "usr_8f3c1a92b7e5d4c0a1b2c3d4e5f60718",
  "founder_device_id": "dev_2a3b4c5d6e7f8091a2b3c4d5e6f70819",
  "founder_cert_ref": "<op_hash of the founder's DeviceCert, hex>",
  "initial_workspace_key_wrap": {
    "epoch": 0,
    "wrap_for_user": {
      "usr_8f3c1a92b7e5d4c0a1b2c3d4e5f60718": "<sealed-box-wrapped key, hex>"
    }
  },
  "parents": [],
  "device_sig": "<ed25519 sig over canonical(op) without device_sig and op_hash, hex>",
  "op_hash": "<sha256 of canonical(op) without device_sig and op_hash, hex>"
}
```

After this op, `workspace_id = "ws_" + op_hash`. The founder is implicitly an `admin`.

`parents: []` — the genesis op has no causal predecessor.

---

## §4 — Membership ops

All membership ops share the **op envelope** (§4.1). The body in `payload` differs by `kind`.

### 4.1 Op envelope (membership and objects share this shape)

```json
{
  "kind": "Invite | Accept | Remove | ChangeRole | RotateWorkspaceKey | ObjectOp",
  "workspace_id": "ws_<hex>",
  "parents": ["<op_hash>", "..."],
  "device_id": "dev_<hex>",
  "user_id":   "usr_<hex>",
  "cert_ref":  "<op_hash of DeviceCert>",
  "payload":   { "...kind-specific..." },
  "device_sig": "<ed25519 sig, hex>",
  "op_hash":    "<sha256, hex>"
}
```

- `parents` is the set of op hashes this op directly succeeds (a Merkle DAG). Concurrent ops have disjoint parent paths to a common ancestor.
- The membership log is one DAG per workspace. Object ops live in the same DAG (parents may include either kind), so causal order is workspace-global.

### 4.2 `Invite`
Authority: signer must be `admin` at the op's causal frontier.

```json
{
  "kind": "Invite",
  "payload": {
    "invitee_user_id": "usr_<hex>",
    "invitee_encryption_pub": "<x25519 pub, hex>",
    "role": "member",
    "workspace_key_wrap": "<sealed-box of current workspace key for invitee, hex>",
    "key_epoch": 0,
    "expires_at_seq": null,
    "note": "optional human reason"
  }
}
```

- `role` is one of `admin`, `member`.
- `workspace_key_wrap` lets the invitee decrypt ops as soon as they accept; without it, accept-then-fetch-keys is a separate round-trip.

### 4.3 `Accept`
Authority: signer's `user_id` must equal the `Invite.payload.invitee_user_id` of an unmatched `Invite` in the causal ancestry, AND the `cert_ref` must resolve to a `DeviceCert` whose `user_id` matches.

```json
{
  "kind": "Accept",
  "payload": {
    "invite_op_hash": "<op_hash of the Invite op, hex>"
  }
}
```

The minimal payload — the binding info is all in the referenced `Invite`.

### 4.4 `Remove`
Authority: signer must be `admin` at the causal frontier. **Last-admin self-removal is rejected.** Two concurrent `Remove`s of the same user converge to one effect (user is removed once).

```json
{
  "kind": "Remove",
  "payload": {
    "removed_user_id": "usr_<hex>",
    "reason": "left_team",
    "force_key_rotation": true
  }
}
```

`force_key_rotation: true` triggers an automatic `RotateWorkspaceKey` op published by the same admin immediately after this op (mechanism, not separate authority).

### 4.5 `ChangeRole`
Authority: admin. Cannot demote oneself if doing so would leave zero admins.

```json
{
  "kind": "ChangeRole",
  "payload": {
    "target_user_id": "usr_<hex>",
    "new_role": "admin"
  }
}
```

### 4.6 `RotateWorkspaceKey`
Authority: admin.

```json
{
  "kind": "RotateWorkspaceKey",
  "payload": {
    "new_epoch": 1,
    "previous_epoch": 0,
    "wrap_for_user": {
      "usr_<hex>": "<sealed-box of new key, hex>",
      "usr_<hex>": "<sealed-box of new key, hex>"
    },
    "reason": "member_removed"
  }
}
```

After this op:
- All new ops are encrypted with key epoch `new_epoch`.
- Old ops remain readable using the cached `previous_epoch` key.
- Members with no entry in `wrap_for_user` cannot decrypt new ops (= effective revocation of read access).

---

## §5 — Authority check (pure function)

Pseudocode. Implemented in `internal/membership/authority.go`. **No I/O, no clock, no randomness.**

```text
func validate(op, log_prefix) -> Valid | Invalid(reason):

  1. Verify op_hash == sha256(canonicalize(op_without_sig_and_hash))
     else Invalid("op_hash mismatch")

  2. Resolve cert := log_prefix.find_cert_by_hash(op.cert_ref)
     else Invalid("cert not found")

  3. Verify cert.user_id == op.user_id
     and cert.device_id == op.device_id
     else Invalid("cert/op identity mismatch")

  4. Verify ed25519.Verify(cert.device_pub, canonicalize(op_without_sig_and_hash), op.device_sig)
     else Invalid("device sig invalid")

  5. user := log_prefix.user_at(op.user_id, cert.user_root_seq)
     Verify ed25519.Verify(user.root_pub, canonicalize(cert_without_user_root_sig), cert.user_root_sig)
     else Invalid("cert not signed by user root")

  6. Verify cert is not revoked at op.parents:
       for each parent in op.parents (transitively):
         if exists Revocation revoking op.cert_ref before this point: Invalid("cert revoked")

  7. Switch on op.kind:
       Create:        Valid (if op.parents == [] and matches workspace_id)
       Invite:        require role(op.user_id, log_prefix at op.parents) == admin
       Accept:        require log_prefix has matching unaccepted Invite for op.user_id
       Remove:        require role(op.user_id, ...) == admin
                      AND not (op.payload.removed_user_id == op.user_id
                               AND admin_count(log_prefix excluding self) == 0)
       ChangeRole:    require role(op.user_id, ...) == admin
                      AND not (demoting self leaves 0 admins)
       RotateKey:     require role(op.user_id, ...) == admin
       ObjectOp:      require role(op.user_id, ...) in {admin, member}
                      AND object-level rule (TBD per object type)

  8. Return Valid.
```

`role(uid, prefix)` and `admin_count(prefix)` are pure functions that fold the membership ops in `prefix` into a state map. Both are derived from the log; never read from a cache without recomputing.

### 5.1 Conflict resolution & tie-breaks
- Two valid concurrent ops are both kept. The derived state is computed by replaying both in **lexicographic op_hash order**. (Idempotent ops produce the same state regardless of order; for non-idempotent ops, the deterministic order ensures convergence.)
- **No wall-clock breaks ties.** Ever.

---

## §6 — Object op envelope

```json
{
  "kind": "ObjectOp",
  "workspace_id": "ws_<hex>",
  "parents": ["<op_hash>", "..."],
  "device_id": "dev_<hex>",
  "user_id":   "usr_<hex>",
  "cert_ref":  "<op_hash of DeviceCert>",
  "payload": {
    "object_id":      "<uuid7>",
    "object_type":    "block | text | list | poll | <future>",
    "schema_version": 1,
    "automerge_change": "<base64 of automerge change blob>",
    "owner_workspace_id": "ws_<hex>"
  },
  "device_sig": "<hex>",
  "op_hash":    "<hex>"
}
```

- `automerge_change` is the only field whose internal layout is not specified by us — Automerge owns it. Determinism requires we pin the `automerge-go` version in `go.mod` and treat its bytes as opaque.
- `owner_workspace_id` is normally equal to `workspace_id`. If not, this is a cross-workspace reference (deferred per Plan §2.0.3 — initially we reject ops where they differ).
- `object_type` is a string registered in the workspace's schema registry (itself a workspace object — bootstrapped with built-in types).

---

## §7 — Encryption framing

### 7.1 On-the-wire envelope (what GossipSub carries)

```json
{
  "ws":         "ws_<hex>",
  "key_epoch":  0,
  "nonce":      "<12-byte nonce, hex>",
  "ciphertext": "<chacha20-poly1305 ciphertext including auth tag, hex>",
  "fingerprint":"<first 8 bytes of op_hash, hex>"
}
```

- `ws` and `key_epoch` are in the clear so peers can route and pick the right decryption key without trial decryption.
- `fingerprint` is in the clear so peers can dedup before decrypting (a partial op_hash is enough; recomputing the full hash after decryption confirms integrity — the AEAD tag does the heavy lifting).
- The plaintext sealed by ChaCha20-Poly1305 is `canonicalize(full_op)` — i.e. the entire signed op (envelope + payload + sig + op_hash).

### 7.2 Workspace key lifecycle
- Generated at `Create` with a fresh 32-byte random key (epoch 0).
- Rotated by `RotateWorkspaceKey` ops; each new epoch's key is wrapped to every current member's encryption pub via X25519 sealed box and stored in the rotation op's payload.
- Old keys are **retained indefinitely** by every member who had access at the time. Removing them would break replay of old ops.
- A member who never had a key for epoch N (joined later) cannot decrypt ops sealed under epoch N. This is by design: pre-membership history is not retroactively shared.

### 7.3 Per-object wrapping (optional, Phase 3)
For sensitive subsets without a separate workspace:

```json
{
  "kind": "ObjectKeyWrap",
  "payload": {
    "object_id": "<uuid7>",
    "object_key_ciphertext": "<chacha20-poly1305 ciphertext of the per-object key under the workspace key, hex>",
    "object_key_nonce": "<12-byte nonce, hex>",
    "allowed_user_ids": ["usr_<hex>", "..."]
  }
}
```

`allowed_user_ids` is enforced by the application layer (the kernel just stores the wrap). True per-object access control requires re-wrapping per allowed user; this is deferred until we have a use case.

### 7.4 Blobs (files)
- Files >64 KB are content-addressed: `blob_hash = sha256(plaintext_bytes)`.
- Blob is encrypted with a per-blob random key, AEAD as in §1.4. The per-blob key is wrapped to the workspace key (single wrap, not per-user).
- The op carrying the file reference contains `{blob_hash, blob_key_ciphertext, blob_key_nonce, mime, size}`. Plaintext bytes never appear in any op.
- Fetched on demand via libp2p stream protocol `/groove/files/1.0.0` (Phase 3).

---

## §8 — Storage layout (Badger)

All keys are bytes; values are JSON-serialized records (canonical JSON). Two stores per node:

### 8.1 Identity store: `<dataDir>/identity/`
Encrypted at rest with the device-local key.

| Key prefix | Value |
|---|---|
| `device/priv` | Ed25519 priv key (raw 32B) |
| `user/<user_id>/encryption_priv` | X25519 priv key |
| `user/<user_id>/seq` | Current root-key seq (uvarint) |
| `cert_cache/<op_hash>` | Cached `DeviceCert` (for fast verification) |

### 8.2 Workspace store: `<dataDir>/workspaces/<ws_id>/`
Encrypted at rest with the device-local key. Per-workspace.

| Key prefix | Value |
|---|---|
| `op/<op_hash>` | The full op envelope (canonical JSON) — the source of truth |
| `wskey/<epoch>` | The workspace key for that epoch (bytes) |
| `members/<user_id>` | Cached current `{role, joined_at_op_hash}` (rebuildable from `op/*`) |
| `dag/parents/<op_hash>` | Parent op hashes (cached for traversal) |
| `dag/children/<op_hash>` | Child op hashes (rebuildable) |
| `objects/<object_id>/state` | Cached Automerge document state (rebuildable from object ops) |
| `blobs/<blob_hash>` | Encrypted blob bytes |

**Rule:** anything labeled "cached" or "rebuildable" must produce identical state from the `op/*` store alone. The replay test (Phase 4) verifies this by deleting all caches and rebuilding.

`op/<op_hash>` is the immutable, content-addressed source. Two peers with the same set of op hashes have, by definition, identical kernel state.

---

## §9 — What is **not** specified here

These are **explicitly deferred** to keep §0 (the invariant) cheap to enforce. Each must land in this spec before its phase opens:

- **Object schema language.** What does "schema_version: 1" mean exactly for built-in types? Defined per type in Phase 4.
- **Cross-workspace object references.** Read-only links per Plan §2.0.3, deferred past Phase 6.
- **Search and indexing.** All caches above are exact; full-text search is application-layer.
- **Privacy semantics for fingerprints.** §7.1's 8-byte fingerprint reveals partial op identity; this is intentional (it enables dedup by non-members on the gossip mesh) but should be revisited if metadata leakage matters.

---

## §10 — Open items still to confirm before code

- [ ] Pick the JCS canonicalizer dependency: `github.com/gibson042/canonicaljson-go` (recommended) vs. roll our own (~150 LOC).
- [ ] Confirm `crypto/sha256` over BLAKE3. SHA-256 chosen for stdlib-only; revisit if hashing becomes a perf bottleneck (very unlikely at our scale).
- [ ] Confirm UUIDv7 library: `github.com/google/uuid` v1.6+ supports it natively. Pin a version.
- [ ] Pin `automerge-go` version. Treat upgrades as breaking-change events that require a new schema_version.
- [ ] Walk through the seven Phase-0 scenarios from `AnyType-VPS/GROOVE-BACKEND-PLAN.md` §5 against this spec. Any failure → spec revision before code.

---

## §11 — Bibliography (well-trodden territory we are reusing)

- RFC 8785 — JSON Canonicalization Scheme (JCS).
- RFC 8949 §4.2 — CBOR deterministic encoding (rejected in favor of JCS, kept here for reference if we ever change wire format).
- Matrix room state resolution v2 — signed-event-DAG + authority rules pattern.
- Keybase sigchains — user/device identity with rotation and revocation.
- Automerge — opaque CRDT engine for object content.
- libsodium sealed boxes — X25519 key wrap pattern (`golang.org/x/crypto/nacl/box`).

If you find yourself inventing something here, check these first. The pattern almost certainly already has a name.
