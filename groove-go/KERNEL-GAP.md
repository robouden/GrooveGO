# KERNEL-GAP.md — Phase 0 Audit

What `internal/*` currently provides versus what the kernel must provide before any layer can safely sit on top of it. Reference: `AnyType-VPS/GROOVE-BACKEND-PLAN.md` §2 (locked decisions) and §3 (the four requirements).

Audited at commit `HEAD`, 2026-04-25. Code totals: 1,322 lines across 8 `.go` files; `internal/sync/` and `pkg/protocol/` are empty.

---

## Module inventory

| Module | Lines | Purpose today | Kernel role (target) |
|---|---|---|---|
| `internal/node/` | 204 | libp2p host, mDNS, DHT bootstrap, hole-punch, AutoRelay | Transport substrate (keep) |
| `internal/transport/` | 171 | GossipSub topic per workspace, JSON `Message` (chat + base64 file), plaintext on wire | Op gossip (rewrite — typed, sealed, signed) |
| `internal/store/` | 118 | Badger KV; key = `msg/<ws>/<unix-nano>/<from>` | Op log + state cache (rewrite — encrypted, vector-clock keyed) |
| `internal/workspace/` | 104 | `Manager` joins topics by name; per-channel `(transport.Workspace, store.Store, presence.Tracker)` | Workspace lifecycle (rewrite — content-addressed ID, membership log) |
| `internal/presence/` | 153 | Heartbeats on `presence-<ws>`, wall-clock `LastSeen`, 15s offline threshold | Liveness only (keep, but ops must not depend on it) |
| `internal/web/` | 364 | HTTP/WebSocket UI shell | UI projection (out of scope for kernel) |
| `internal/apps/` | 208 | Codeberg-webhook adapter | Application layer (out of scope) |
| `internal/sync/` | **0** | — | CRDT engine **(missing)** |
| `pkg/protocol/` | **0** | — | Wire types **(missing)** |

---

## Requirement 1 — Identity is not optional (§2.0.1: device-first internally, user-first externally)

**What exists.** `node.go:33` generates a fresh Ed25519 keypair via `crypto.GenerateEd25519Key`. The libp2p host's `peer.ID` is the public-key hash. That is it.

**What is missing.**
- No persistence. The key is regenerated on every process start (`node.New` always calls `GenerateEd25519Key`); restart = new peer ID, lost identity.
- No `User` type. There is only a libp2p peer (which is closer to a "device"). No way to express "Alice on her laptop" vs. "Alice on her phone".
- No `DeviceCert` linking a device to a user. No way for a peer to verify that ops from device-X are authorized by user-Alice.
- No `KeyRotation` op, no `Revocation` op. No way to invalidate a compromised key without invalidating all past ops.
- No keystore at rest. The libp2p priv key is held only in memory.

**What is wrong.**
- Treating `peer.ID` as the security identity conflates transport (pluggable, can change) with authorization (must be stable). When NAT traversal swaps relays or AutoRelay rotates, peer addresses change but identity must not — that distinction does not exist yet.
- Restart-creates-new-identity actively breaks every other requirement: there is nothing to bind membership, ops, or workspaces to over time.

**Gap size:** ~95%. Only the cryptographic primitive (Ed25519 signing) is in place.

---

## Requirement 2 — Membership must be first-class (§2.0.2: signed event log primary, snapshot is a cache)

**What exists.** `workspace/manager.go:44` `Join(name)` joins a GossipSub topic named `workspace-<name>`. `transport.JoinWorkspace` opens the topic and subscribes. That is the entire membership model.

**What is missing.**
- No `Invite` / `Accept` / `Remove` / `ChangeRole` / `RotateWorkspaceKey` ops. No op log at all — let alone a *signed* one.
- No authority rules. Any peer that learns the topic name and is on the same GossipSub mesh is "in". No admin / member distinction.
- No history. There is no record of who joined, when, or by whom. `manager.active` is a `map[string]*entry` keyed by name, in-memory only.
- No content-addressed workspace ID. A workspace is identified by a free-form string (`name string`); two peers using the same name on disjoint networks would silently produce two different workspaces that merge incorrectly if they ever connect.

**What is wrong.**
- The current model is "anyone who knows the topic name has full read+write." That is not membership — it is a capability URL. There is no way to share anything safely.
- Workspace identity = string is not stable: rename the workspace and history is orphaned; create two with the same name and they collide.

**Gap size:** ~100%. There is no membership system to extend; this layer must be built from scratch.

---

## Requirement 3 — Data must be scoped to shared contexts (§2.0.3 + §2.0.4 encryption)

**What exists.**
- *Per-workspace topic:* `transport.JoinWorkspace` uses topic `"workspace-" + name` (`pubsub.go:61`); presence uses `"presence-" + name` (`tracker.go:46`). This is partial alignment with "per-workspace GossipSub topic `/groove/ws/<wsID>/ops/1.0.0`" from §5 Phase 3.
- *Per-channel store:* `Manager` opens a Badger DB at `<storeDir>/<name>` so on-disk data is also scoped (`manager.go:52`).

**What is missing.**
- *No object concept.* `Message` (`pubsub.go:23`) is a chat-or-file blob. There is no `ObjectID`, no `Version`, no `Op` envelope. We cannot scope replication to objects when objects do not exist.
- *No encryption — anywhere.* The wire format is JSON marshaled directly to GossipSub (`pubsub.go:106`). The Badger store is unencrypted (`store.go:38`, `badger.DefaultOptions(dir)` with no `WithEncryptionKey`). Files saved to `<storeDir>/<name>/files/` are plaintext on disk (`store.go:103`).
- *No workspace key.* No symmetric key is generated, stored, or rotated. There is no key wrap, sealed-box, or per-member envelope.
- *No replication boundary policy.* Files are inlined as base64 in chat messages (`pubsub.go:100`) up to 10 MB, broadcast to every peer. There is no content-addressed blob store, no on-demand fetch, no delta sync.

**What is wrong.**
- Topic name = string equals workspace ID. Anyone who guesses or learns the name has full access. With a content-addressed workspace ID, the ID itself is unguessable and binds to a specific genesis op.
- File transport via 10 MB base64 in JSON is gossip-amplification: every peer in the topic receives every byte, even if they already have the file. No deduplication by content hash.
- Plaintext on the wire violates the "encryption aligned with membership" property §2.0.4 promotes to first-class. Removing a member changes nothing about what they can read off the network; we must move ciphertext now.

**Gap size:** ~90%. The topic-per-workspace shape is right; everything inside the topic must change.

---

## Requirement 4 — Deterministic merge semantics (§2.0.5: replay invariant)

**What exists.** Nothing relevant. `internal/sync/` is empty.

**What is missing.**
- No CRDT. No Automerge integration. No `internal/objects/` package.
- No vector clocks. No causal-order tracking at all.
- No `ObjectID` (UUIDv7), no `Version`, no `Ref`, no `Op` envelope.
- No schema registry for object types.
- No replay test suite. No way to assert "wipe peer, re-sync, byte-identical state."

**What is wrong (active violations of §2.0.5).**
- *Wall-clock keyed storage.* `store.Save` keys messages by `msg/<workspace>/<unix-nano>/<from>` (`store.go:58`). Two peers with skewed clocks producing the same logical message land at different keys; replay order is wall-clock, not causal. The replay invariant ("every op replayable from empty produces identical state") cannot hold with this scheme.
- *Wall-clock timestamps in payloads.* `transport.Message.Timestamp = time.Now().UTC()` (`pubsub.go:80`). If any future merge logic uses `Timestamp`, it is non-deterministic across peers.
- *Order of receipt leaks into state.* `History()` returns messages in Badger key order (`store.go:79`), which is the receipt-time order. A peer that receives messages in a different order produces a different `History()` result.
- *Self-message filter is by ReceivedFrom.* `pubsub.go:133` skips messages where `ReceivedFrom == w.self`, but that is a transport-layer property, not a causal one. A relayed self-op would be processed; a forwarded other-peer op originating from self would be dropped.

**Gap size:** 100%, *and* the existing store and message format actively block the determinism invariant. Phase 4's replay test suite cannot be written against the current code without first removing wall-clock dependencies.

---

## Cross-cutting: encryption model (§2.0.4)

Counted under Requirement 3 above. Summary: **zero plaintext-to-ciphertext separation anywhere**. All mitigations are deferred to Phases 2–3 of the build.

---

## Punch list (priority order, mapped to plan phases)

The numbering matches `GROOVE-BACKEND-PLAN.md` §5.

### P1 — Identity kernel (Phase 1)
1. Create `internal/identity/`. Define `User`, `Device`, `DeviceCert`, `KeyRotation`, `Revocation` records (formats frozen in `KERNEL-SPEC.md`, not yet written).
2. Persist Ed25519 device key to disk in `<dataDir>/identity/`. Encrypt the private key blob with a passphrase / OS-keychain-derived key (§2.0.4 at-rest).
3. Stop calling `crypto.GenerateEd25519Key` in `node.New`; load the existing device key, generate only on first run.
4. Add `device-sig(op)` + `DeviceCert` reference to every kernel op. Verifiers must check both.

### P2 — Membership kernel (Phase 2)
5. Create `internal/membership/`. Implement signed event log + derived snapshot cache. Ops: `Invite`/`Accept`/`Remove`/`ChangeRole`/`RotateWorkspaceKey`.
6. Replace `workspace.Manager`'s string-name keying with a content-addressed `wsID = hash(genesis_op)`. Keep a human-readable name in metadata, never as identity.
7. Implement authority check as a pure function `(log_prefix, op) → valid|invalid`. Last-admin self-removal blocked.
8. Replace topic name `"workspace-" + name` with `/groove/ws/<wsID>/ops/1.0.0`.

### P3 — Scoped replication & encryption (Phase 3)
9. Generate a workspace symmetric key at `Invite`-genesis. Wrap to each member's user key (X25519 sealed box). Persist wrapped copies in the membership log.
10. Seal every op with the workspace key before publishing on GossipSub. Non-members see ciphertext only.
11. Wire `RotateWorkspaceKey` op to actually rotate: on `Remove`, emit a new key, wrap to remaining members, retain old key for replay of pre-rotation ops.
12. Remove file inlining. Add a content-addressed blob protocol (`/groove/files/1.0.0`) with on-demand fetch by hash. Encrypt blobs with the workspace key (or a per-object subkey) before storing.
13. Encrypt the Badger store at rest (`badger.DefaultOptions(dir).WithEncryptionKey(deviceKey)`).

### P4 — Deterministic merge (Phase 4)
14. Create `internal/objects/` and `pkg/protocol/`. Define `ObjectID` (UUIDv7), `Version` (vector clock), `Op` envelope, `Ref`.
15. Replace `store.Save` keying. Drop `unix-nano`. Key by `(wsID, objectID, version_hash)` so the same op produces the same key on every peer.
16. Remove all wall-clock dependencies from anything in the merge path. `transport.Message.Timestamp` is allowed only as a UI hint, never as input to ordering or merge.
17. Pull `automerge-go` into `internal/sync/`. Wire CRDT for object content. Hand-roll deterministic CRDT for object metadata (type, relations).
18. Add the replay test harness: for every kernel op kind, wipe a peer, re-sync from peers, assert byte-identical derived state. This is the gate for Phase 4 acceptance.

### Cleanup (any phase)
19. Stop duplicating `Message` between `transport/` and `store/`. The kernel op envelope in `pkg/protocol/` becomes the single source.
20. Delete or quarantine `internal/web/` and `internal/apps/` from the kernel — they are projections, not kernel code. Move under a `cmd/` or top-level `app/` once the kernel is real.

---

## What we keep verbatim

- `internal/node/` (libp2p host, mDNS, DHT, hole-punch, AutoRelay) — transport, not kernel. No changes needed.
- `internal/presence/` — liveness signal only. Already separated from the op channel via its own topic. Continues to use wall-clock for `LastSeen` because nothing in the kernel may depend on presence state.

---

## Decision: ready for `KERNEL-SPEC.md`?

**Not yet.** Two items must be settled in spec before code:

- **CRDT choice for membership log.** Plan recommends hand-rolled (small, deterministic, auditable). Confirm that decision in `KERNEL-SPEC.md` before defining op formats.
- **Object op envelope encoding.** Plan says protobuf via `pkg/protocol/`. Current codebase has zero protobuf footprint and uses JSON throughout. Confirm protobuf vs. canonical JSON before fixing wire layouts; protobuf is recommended for determinism but adds tooling.

Once both are decided, `KERNEL-SPEC.md` can freeze the record formats and the seven scenario walk-throughs can begin.
