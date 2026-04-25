# KERNEL-SCENARIOS.md — Phase 0 walk-through

Seven scenarios from `AnyType-VPS/GROOVE-BACKEND-PLAN.md` §5 walked against `KERNEL-SPEC.md`. Each scenario either (a) confirms the spec, or (b) surfaces a concrete revision. Spec freezes only after all seven resolve cleanly.

Cast (used throughout):
- **Alice** — founder/admin. User `usr_A`, root keypair `(ALICE_R0_PRIV, ALICE_R0_PUB)`, encryption keypair `(ALICE_E_PRIV, ALICE_E_PUB)`.
- **Bob** — member (sometimes invitee). User `usr_B`. Devices `dev_B1` (laptop), `dev_B2` (phone).
- **Carol** — admin or member, depending on scenario. User `usr_C`.
- **Workspace** `ws_X` created by Alice (genesis op_hash = `C0`).

---

## Scenario 1 — Device-less user joins from a new laptop

**Setup.** Alice's user record exists (`usr_A`, root_seq=0). She gets a new laptop. New device `dev_B1` (sic — using B1 mnemonic as "laptop1" to avoid clashing with `dev_A`'s implicit existence on her old machine). The new laptop has a fresh Ed25519 device keypair but no `DeviceCert` yet.

**Flow trace.**

1. New laptop generates `(DEV_B1_PRIV, DEV_B1_PUB)` and the `Device` record `D_B1`.
2. New laptop must obtain a `DeviceCert` signed by `ALICE_R0_PRIV`. Two paths:
   - **(a)** Root key lives on the new laptop too (e.g. restored from backup) → laptop signs its own cert. Trivial.
   - **(b)** Root key lives on an existing trusted device → that device signs `Cert_B1` referencing `dev_B1`, hands it back via QR code, libp2p stream, or any side channel.
3. Alice now has `Cert_B1` on the new laptop. She wants to use the laptop in workspace `ws_X`.
4. The laptop publishes its first op into `ws_X`. It references `cert_ref = hash(Cert_B1)`.

**⚠️ Gap surfaced.** Step 4's validation (§5 step 2) does `log_prefix.find_cert_by_hash(op.cert_ref)`. But `Cert_B1` has never been written into `ws_X`'s log — it lives only on Alice's two devices and any side-channel transport. **Other peers cannot resolve `cert_ref`.**

**Spec revision required.** Add a `Cert` op kind (publishes a `DeviceCert` into a workspace log so peers can resolve `cert_ref`):

```json
{
  "kind": "Cert",
  "workspace_id": "ws_<hex>",
  "parents": ["<op_hash>"],
  "device_id": "<the device the cert authorizes>",
  "user_id": "usr_<hex>",
  "cert_ref": "<self-reference: hash of the cert payload below>",
  "payload": {
    "cert": { /* full DeviceCert from §2.3 */ }
  },
  "device_sig": "<signed by the new device — proves possession of device priv>",
  "op_hash": "<...>"
}
```

Authority for `Cert`: any user already in the workspace can publish a `Cert` for their own additional devices (they just attest "this is also me"). The validity check for `Cert` itself uses the **embedded cert** (a chicken-and-egg break): step 2 of `validate` resolves `cert_ref` against `op.payload.cert` when `op.kind == "Cert"`. Subsequent ops by `dev_B1` reference `cert_ref = hash(Cert_B1_op)` which now resolves via the log.

**Verdict.** Spec needs the `Cert` op kind added (revision #1 below). After that, scenario passes. ✓

---

## Scenario 2 — Offline invite, accept, reconciliation

**Setup.** Alice (admin), Bob (not yet a member), Carol (member). All start in sync at op `H` (some prior op in `ws_X`).

**Flow trace.**

1. Alice goes offline. She authors `Invite_B`:
   - parents=[H], kind=Invite, payload.invitee=usr_B, role=member, workspace_key_wrap=sealedbox(K0, ALICE_E_PRIV → BOB_E_PUB), key_epoch=0.
   - Validates: Alice is admin at H. ✓
   - Stored locally. Not yet gossiped.
2. Alice meets Bob in person, hands him `Invite_B` (USB / QR / direct libp2p).
3. Bob authors `Accept_B`:
   - parents=[hash(Invite_B)], payload.invite_op_hash=hash(Invite_B).
   - Validates: signer is `usr_B`, matches `Invite_B.payload.invitee`. ✓
   - Bob now has `{H, Invite_B, Accept_B}`. Carol still has `{H}` plus any ops Carol authored locally.
4. Alice and Carol come back online. Bob too. GossipSub reconciles.
5. Carol receives `Invite_B` first. Validate: `Invite_B.parents = [H]` ⊆ Carol's local log. Authority check: signer Alice was admin at H. ✓. Apply.
6. Carol receives `Accept_B`. Validate: `Accept_B.parents = [hash(Invite_B)]` ⊆ Carol's local log (just added). Matching `Invite_B` exists. ✓. Apply. Bob is now in Carol's membership snapshot.
7. Bob's `cert_ref` resolves against scenario 1's revision (Bob published a `Cert` op as part of joining, embedded with `Invite_B` or sent as a prior op).

**Verdict.** Spec handles this cleanly given revision #1 from scenario 1. ✓

---

## Scenario 3 — Concurrent admin removes of the same member

**Setup.** Alice and Carol are both admins. Both observe state at op `H`. Bob is a member.

**Flow trace.**

1. Concurrently:
   - Alice authors `Remove_A`: parents=[H], payload.removed_user=usr_B.
   - Carol authors `Remove_C`: parents=[H], payload.removed_user=usr_B.
2. Both ops are gossiped. Each peer receives both eventually.
3. Validate `Remove_A`: signer Alice is admin at H. ✓
4. Validate `Remove_C`: signer Carol is admin at H. ✓
5. Both kept in the DAG (concurrent — neither is in the other's `parents`).
6. Replay derived state by replaying both ops in **lex order of `op_hash`** (§5.1). Effect:
   - First applied Remove → Bob → removed.
   - Second applied Remove → Bob → already removed → no-op.
7. Idempotent. Both peers converge to identical state.

**Verdict.** Pure case. Spec handles it. ✓

**⚠️ Edge case surfaced.** What if both removes set `force_key_rotation: true`? Each Alice and Carol immediately follow with their own `RotateWorkspaceKey` op:
- `Rotate_A`: payload.new_epoch=1, wrap_for_user excludes Bob, signed by Alice.
- `Rotate_C`: payload.new_epoch=1, wrap_for_user excludes Bob, signed by Carol.

Both ops generate **different** workspace keys but both label themselves `epoch=1`. After replay, peers can't tell which key is "the" epoch-1 key. **Spec defect.**

**Spec revision required.** §7.2 must change `key_epoch` from a sequential int to a **content-addressed identifier**: `key_epoch = "ke_" + first_16_hex(op_hash_of_rotation_op)` (genesis epoch is `"ke_genesis"`). Then `Rotate_A` produces `ke_aaaa...`, `Rotate_C` produces `ke_cccc...`, both kept, both have wrapped keys, peers learn both. New ops sealed by an admin pick one of the available current keys (lex-lowest) and tag the envelope; peers try whichever epoch the envelope names.

Document the convergence rule: "current epoch = lex-lowest content-addressed epoch among the rotations whose `previous_epoch` chain reaches the genesis and whose causal frontier does not have a successor rotation."

**Verdict (after revision).** ✓

---

## Scenario 4 — Removed Bob's pre-removal op concurrent with `Remove`

**Setup.** Alice (admin), Bob (member), Carol (member). All sync at `H`. Bob then goes offline.

**Flow trace.**

1. Bob (offline) authors `Op_B` at parents=[H]: an ObjectOp editing some object. Stored locally.
2. Concurrently online: Alice authors `Remove_B` at parents=[H]: payload.removed_user=usr_B. Gossiped to Carol.
3. Bob comes online. Both `Op_B` and `Remove_B` propagate.
4. Validate `Op_B`:
   - cert_ref resolves. ✓
   - Bob's role at `Op_B.parents` = [H]: Bob is a `member`. ✓
   - Signature valid. ✓
   - **Accept `Op_B`.** Bob's edit, made when he was a member, persists.
5. Validate `Remove_B`: Alice admin at [H]. ✓ Accept.
6. Bob (now seeing `Remove_B`) authors `Op_B2` at parents=[hash(Op_B), hash(Remove_B)]: another edit.
   - Validate step 7: role(usr_B, prefix at [Op_B, Remove_B]) = `removed`. **Reject.**
7. Convergent state: `Op_B` applied; `Op_B2` rejected; Bob no longer a member.

**Verdict.** Spec handles it correctly. The "valid at the op's causal frontier" rule does the right thing both ways. ✓

**Note.** §4.4 `Remove` does not revoke device certs (cert revocation is `Revocation`, §2.5). Bob's cert is still cryptographically valid; Bob just isn't a member anymore. Future ops fail the **role check** in step 7, not the **cert check** in step 6. This is intentional: it lets Bob be re-invited later without issuing a new cert.

---

## Scenario 5 — Root key rotation; old certs verifiable for past ops, not new ones

**Setup.** Alice has root_seq=0, root_pub=R0. Several `DeviceCert`s reference `user_root_seq=0`. She rotates.

**Flow trace.**

1. Alice authors `KeyRotation_A`: from_seq=0, to_seq=1, new_root_pub=R1, old_root_sig=Sig_R0(rotation).
2. Validate signature of rotation using cached R0. ✓
3. Update Alice's `User` record: root_seq=1, root_pub=R1.
4. Old certs (reference user_root_seq=0): peers cache R0 and continue to verify them when validating **historical** ops that referenced them.
5. Past ops (ops in the DAG before `KeyRotation_A`) signed by old certs: still valid. Replay still works.
6. Future ops (after `KeyRotation_A` in causal order) signed by **old** certs: should be rejected.

**⚠️ Gap surfaced.** §5 `validate` pseudocode does not currently enforce step 6. Step 5 verifies the cert was signed by `user_at(op.user_id, cert.user_root_seq)` — but it accepts any cert from any seq. There is no check that the cert's seq matches the user's **current** seq at `op.parents`.

Nothing in §5 step 5 fails for an old cert: we look up the user at `cert.user_root_seq` (= 0), find R0, verify the cert sig — passes. The op then signs with the old device key, which still works.

**Spec revision required.** Add step **5b** to §5 `validate`:

> 5b. Compute `current_seq := log_prefix.user_seq_at(op.user_id, op.parents)`.
>     If `cert.user_root_seq < current_seq` AND `cert.not_after_seq < current_seq` (or `cert.not_after_seq == null`),
>     **reject**: "cert from old root-key generation, not valid for ops after rotation."
>
> Exception: ops whose causal position is **before** the rotation (i.e. `op.parents` does not transitively include `KeyRotation_A`) use the seq that was current at that prefix. Implementation: walk `op.parents` to find the latest `KeyRotation_*` op for `op.user_id` causally preceding it; that's the seq the cert must match (or be issued under).

This makes "old certs work for old ops, new certs for new ops" explicit and enforced.

**Verdict (after revision).** ✓

---

## Scenario 6 — Workspace key rotation on `Remove`

**Setup.** Alice (admin), Bob (member), Carol (member) in `ws_X` with workspace key K0 (epoch `ke_genesis`).

**Flow trace.**

1. Alice authors `Remove_B(force_key_rotation=true)`. Sealed under K0.
2. Alice immediately authors `Rotate`: parents=[hash(Remove_B)], payload.new_epoch=ke_<hash(Rotate)>, payload.previous_epoch=ke_genesis, payload.wrap_for_user={usr_A: ..., usr_C: ...}. **Bob excluded.** Sealed under K0 (the rotation op announces a new key but is itself sent under the old key so all current members can read it).
3. Bob, still on the gossip mesh, receives both ops. Bob has K0, can decrypt them. Bob sees: he's removed; new epoch announced; he is not in the wrap. Bob retains K0 in his store (per §7.2 — old keys are retained indefinitely so replay still works).
4. Future ops in `ws_X` sealed under K1. Bob receives ciphertext on the mesh but cannot decrypt — no K1.
5. Carol receives both ops, decrypts with K0, unwraps K1 with her X25519 priv (sealed-box). Carol stores K1 under `wskey/ke_<hash(Rotate)>`. New ops decrypt with K1.

**Verdict.** Works given revision #2 (content-addressed epochs from scenario 3). ✓

**Subtlety.** Bob can still see ciphertext of post-removal ops on the gossip mesh (he's still on the topic). The kernel does not eject him from the topic — that's a transport-layer concern. This is fine: he can't decrypt. But it does leak op-rate metadata to him until his peers stop relaying on his behalf or he is GossipSub-banned. Documented in §9 "fingerprint reveals partial op identity" — same trade-off, deferred.

---

## Scenario 7 — Full peer wipe and replay → byte-identical state

**Setup.** Carol's machine has a Badger workspace store at `<dataDir>/workspaces/ws_X/` with caches (`members/*`, `objects/*`, `dag/*`) and ops (`op/*`). Carol's identity store at `<dataDir>/identity/` has her device priv key, X25519 priv, and seq counter.

**Flow trace.**

1. **Wipe `<dataDir>/workspaces/ws_X/`.** Carol's identity is **not** wiped — that would mean losing her device altogether (covered by recovery, not by replay).
2. Restart. Carol's node has identity but no workspace data.
3. Connect to peers. Subscribe to `ws_X` topic. Peers gossip the full op set (or Carol fetches via libp2p stream).
4. For each op received (in any order):
   - Decrypt envelope using `wskey/<key_epoch>` if Carol has it; otherwise fetch from a `RotateWorkspaceKey` op's `wrap_for_user[usr_C]` and unwrap with her X25519 priv.
   - Validate per §5.
   - Store at `op/<op_hash>`. Ops are content-addressed; receipt order doesn't matter.
5. After all ops received, rebuild caches:
   - DAG: walk `op/*`, populate `dag/parents/*` and `dag/children/*`.
   - Membership snapshot: replay membership ops in causal order with lex-tiebreak. Populate `members/*`.
   - Object state: feed object ops into Automerge per object_id. Populate `objects/<oid>/state`.
6. Compare to a peer (Alice). For every key in `op/*`, both peers have identical bytes (content-addressed). For every cache key, identical bytes given identical replay rules.

**⚠️ Gap surfaced.** §0 (the invariant) says "every op must be replayable from an empty node and produce identical state." It's silent on whether *identity* is part of "empty." Without clarification, "wipe everything" includes the identity store, which loses the X25519 priv key, which means workspace keys are unrecoverable, which means scenario 7 trivially fails.

**Spec revision required.** Clarify §0 (or add a §0.1):

> The replay invariant assumes the device's **identity store** is intact. "Replay from empty" means the workspace store is wiped and rebuilt; the identity store (`<dataDir>/identity/`) is the device's persistent secret bag. Loss of the identity store is **device loss**, recovered separately via backup/restore, not by replay.

**Second gap.** When validating an op whose `cert_ref` is a `Cert` op embedded in the log, we need that `Cert` op already in `op/*` before validation can succeed. Solution: validation is **not** strict on receipt order — store ops first, validate during/after the rebuild pass. Step 4 above is correct as written (store first); step 5 does the validation. Document this explicitly:

> **Two-pass replay:** during sync, ops are stored at `op/<op_hash>` without validation (content-addressing is the integrity check). A second pass walks the DAG in causal order and validates each op against the partial replayed state. Caches are populated only by the validation pass.

**Verdict (after revision).** ✓

---

## Spec revisions summary

The walk-through surfaced **four concrete revisions**. None invalidate §2 decisions; all tighten the spec.

1. **Add `Cert` op kind** (§4 new subsection, e.g. §4.7). Publishes a `DeviceCert` into a workspace log so `cert_ref` is resolvable. Self-signed by the device the cert authorizes; validation uses the embedded cert payload. Without this, scenario 1 cannot complete.

2. **Workspace key epochs are content-addressed**, not sequential ints (§7.1, §7.2, §4.6). Rename `key_epoch: 0` to `key_epoch: "ke_<16-hex-of-rotation-hash>"` (genesis = `"ke_genesis"`). Add convergence rule for concurrent rotations. Without this, scenario 3+6 with concurrent rotations corrupts the workspace key namespace.

3. **Authority check enforces current root-key seq** (§5 step 5b). Old certs verify only ops causally before the corresponding `KeyRotation`. Without this, scenario 5 lets compromised pre-rotation keys author new ops forever.

4. **Identity store is out of scope of "replay from empty"** (§0 or new §0.1). Replay rebuilds the workspace store; the identity store is the device's persistent secret bag. Lost identity = device loss, recovered by backup, not by replay. Also: replay is **two-pass** — store first, validate during DAG walk. Without this, scenario 7 has an undefined behavior surface.

## Other findings (informational, no revision needed)

- Scenario 4's "remove doesn't revoke cert" is intentional and correct. Re-inviting a removed user does not require a new cert.
- Scenario 6's "Bob still sees ciphertext on the mesh" is a known transport-layer trade-off, already noted in §9 of the spec. Not a kernel concern.
- All seven scenarios converge to identical state across peers given the four revisions above.

---

## Status

- [x] All seven scenarios walked.
- [ ] Apply revisions 1–4 to `KERNEL-SPEC.md`.
- [ ] Re-walk scenarios 1, 3, 5, 7 against the revised spec.
- [ ] Pin library versions (open items in §10 of the spec).
- [ ] **Then** unfreeze Phase 1 and start writing `internal/identity/`.
