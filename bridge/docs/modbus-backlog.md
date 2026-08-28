# Local Modbus Validation Backlog

This document is the authoritative list of **open** local-Modbus validation
work. The [validation ledger](./modbus-validation.md) records evidence already
accepted into `jks-6-20h-ei-readonly-v1`; this backlog records everything that
must not be inferred from that evidence.

An item can remain blocked indefinitely when the corresponding hardware or
operating condition is unavailable. Missing support is safer than publishing a
plausible-looking value under the wrong canonical key. Nothing in this list is
an instruction to scan registers or change inverter settings.

## Validation Gates

A new numeric field or a wider value domain is eligible for production only
after all applicable gates pass:

1. At least two compatible protocol maps agree on the exact register meaning,
   read-only status, width, word order, sign, and scale.
2. A fixed, smallest-possible FC03 request set is represented by offline
   request, response, checksum, CRC, parser, and failure-path tests.
3. With the normal bridge stopped, a supervised one-shot performs every request
   in the reviewed set exactly once, on one connection, without a retry,
   reconnect, range scan, or write function.
4. The complete raw response is bracketed closely in time against the inverter
   display or vendor cloud while the target condition is present. Identity and
   credential fields are redacted before evidence is retained.
5. Live evidence proves address, scale, sign, word order, phase/total semantics,
   and any encoded invariant. Zero-only evidence never proves a nonzero domain.
6. The decoder fails closed on unsupported values and returns no partial
   snapshot. Tests lock exact request order/count, accepted boundaries, rejected
   boundaries, metric identity, and priority-source fallback.
7. The collected-data contract, validation ledger, request counts, and metric
   counts are updated together with the implementation.

Do not use FC06/FC10, logger HTTP configuration endpoints, arbitrary register
reads, broad adjacent ranges, discovery scans, concurrent pollers, or guessed
aliases. Do not publish an unavailable measurement as zero. The existing
generator zeros are the deliberate exception: they are emitted only while all
of the corresponding live-observed raw words remain exactly zero, and any
nonzero word rejects that Modbus snapshot.

## Current Safe Boundary

The production profile is locked to the JKS-6/8/10/12/15/20H-EI family shape:
device type `0x0006`, two MPPTs, three phases, and one of the allowlisted
6/8/10/12/15/20 kW ratings. Only the 12 kW target has been fully live-correlated.
Each fetch uses one fresh connection and fixed FC03 reads under one absolute
deadline. It has no write or arbitrary-register path.

The current 80-metric core comprises 72 shared canonical metrics and eight
Modbus-local diagnostics: the register-500 run state, the register-551 power
switch state, and six raw warning/fault words. When Modbus establishes the
primary failover surface, ordinary cloud-only fields are intentionally omitted
after fallback rather than invented or emitted with competing labels. Shelly
`grid_load` enrichment is independent of this Modbus core.

Open equipment-dependent items do not widen the supported contract and do not
become release blockers merely because the required equipment is unavailable.
They remain documented exclusions, and the current decoder must keep rejecting
out-of-contract states so priority fallback can run. MB-001 is an explicit
acceptance-evidence debt rather than an unbounded decoder: its current
map/schema-backed status handling is narrow, read-only, and fail-closed. Any
implementation that claims one of the open domains is blocked until that item's
validation gates are complete.

## Open Work

### MB-001 TODO: Exact Raw/Relay-State Bracket for Registers 551-552

**Status:** repeated complete target fetches prove transport and decoder
acceptance; exact raw-word evidence and a relay-state bracket remain pending.

Two compatible read-only maps and the canonical cloud schema support the
current narrow promotion. Repeated production snapshots with the exact 80-core
Modbus surface could only succeed after the fixed `551 x 2` response passed all
framing and decoder gates, so the earlier first-read debt is closed. A
supervised raw capture must still retain both complete words and compare
register 552 with the contemporaneous AC relay state. It must include the
complete raw register 551, not only the low nibble that normal metric output
exposes, so any upper bits are visible. Named interpretation of register-552
relay and dry-contact bits needs separate observed state transitions; the
current raw-U16 contract does not claim that every documented bit has been
live-proven. The read must not toggle a relay or power state.

### MB-002 TODO: Exact Negative Inverter-Output Capture

**Status:** current signed decoder is safe; exact negative magnitude correlation
is pending.

The production log proved that register 691 can be `0xFFFF`, but did not retain
registers 633-636 or high words 692-694. Capture both existing blocks during
verified negative output, preserve all four low/high pairs, and compare
`INV_O_P_L1..3` and `INV_O_P_T` with a closely timed cloud/display sample. This
evidence may confirm the current `-32767..32767 W` domain; it must not
automatically widen that coherence envelope.

### MB-003 TODO: Full UPS/Backup-Load Power

**Status:** blocked on a discriminating load sample and complete pair evidence.

Production currently reads register 643 separately and emits only its unsigned
U16 value as canonical `UPS_P`. Within the `640 x 7` block, registers 640-643
are not decoded as phase/total power; only voltage registers 644-646 are used.
Compatible-map candidates pair low words 640-643 with high words 696-699, but
the sign convention, phase order, relationship between register 643 and the
joined total, and relationship to cloud `C_P_L1..3`/`UPS_P` must all be proven.
Validation needs a nontrivial, preferably unbalanced backup-load sample plus
zero and boundary tests. Until then, the separate U16 `UPS_P` is the entire
supported UPS-power contract.

### MB-004 TODO: Lifetime PV Production (`Et_ge0`)

**Status:** mapping not established.

Locate a lifetime-PV counter whose read-only address, width, word order, and
scale agree across two compatible maps. Verify it against cloud `Et_ge0` over
multiple observations, including monotonic behavior and the distinction from
grid-side generation, import/export, and daily-PV counters. Do not promote a
counter merely because its value is numerically close to the cloud total.

### MB-005 TODO: Output Apparent Power and Power Factor

**Status:** blocked on a materially non-unity-power-factor sample.

Registers 637/695 are compatible-map candidates for joined output apparent
power and are deliberately ignored today. Prove their word order, scale, and
relationship to canonical `O_P` (`kVA`) while apparent and active power differ
materially. Independently establish the meaning and scale of cloud `P_F1`; do
not derive it from rounded active/apparent values or reuse a load-side power
factor. Keep load apparent power and inverter output apparent power separate;
canonical load apparent total `E_Suse_t1` needs its own proven source.

### MB-006 TODO: External CT Current and Power

**Status:** blocked on an installed/configured external CT and directional flow.

Compatible maps identify current candidates at registers 613-615 and active
power low words at 616-619 with high words at 705-708. Validate the CT mode,
phase ordering, current scale, signed import/export convention, low-first
pairing, total semantics, and behavior during both flow directions. Only then
map proven fields to `CT1_P_E`, `CT2_P_E`, `CT3_P_E`, and `CT_T_E`. The
internal grid-current metrics at registers 610-612 are a different measurement
surface and must not be relabelled as external CT.

### MB-007 TODO: Internal and Reactive Power Surfaces

**Status:** mapping and discriminating live conditions missing.

Cloud keys `GS_A`, `GS_B`, `GS_C`, and `GS_T` represent internal power, not the
already supported grid phase/total powers. Reactive keys `G16`, `A_RP_PG`,
`B_RP_PG`, `C_RP_PG`, `A_RP_INV`, `B_RP_INV`, and `C_RP_INV` likewise require
their own documented register pairs, signs, scales, and totals. Validation needs
a sample with material reactive power; zeros or near-unity power factor cannot
distinguish aliases.

### MB-008 TODO: Extended BMS and Second-Battery Data

**Status:** partially blocked on BMS protocol evidence; battery 2 requires
matching hardware.

The safe aliases today are `BMS_SOC` from the already validated SOC register
588 and `BMST` from the already validated temperature register 586. Do not
alias `BMS_B_V1` to `B_V1` or `BMS_B_C1` to `B_C1`: the
[retained cloud fixture](../testdata/jinko_detail_response.json) contains
different simultaneous values for each pair. BMS voltage/current,
charge/discharge limits, SOH, capacity, cycle/state data, cell diagnostics, and
all `bms2` fields need addresses and live BMS correlation of their own. Jinko
lithium-BMS alarm/fault keys are also not aliases for inverter-wide raw warning
words 553-558.

### MB-009 TODO: Wider and Signed-Total Direct-Load Coherence

**Status:** signed phases are production-safe inside `-32767..32767 W`; wider
phases and any signed-total contract remain blocked on raw target evidence and
an acquisition-coherence design.

Phase pairs 650/656 through 652/658 accept canonical sign extension and joined
values inside `-32767..32767 W`; production observation of `R656=0xFFFF`
established the sign-word shape but did not preserve an exact paired negative
magnitude. Dedicated total 653/659 remains in its verified zero-high
`0..65535 W` subset. Capture complete raw low/high words plus a nearby
cloud/display value before claiming an exact negative magnitude or enabling a
signed total. Preserve all four independent pairs; do not add a phase-sum
equality gate because a live positive sample already disproved that invariant.

The non-negative total range is not a complete torn-sign guard if the actual
total can cross below zero. Full coherence while retaining values above
`32767 W` requires a target-validated acquisition strategy, such as one
contiguous block that returns both halves coherently or fixed high/low/high
bracketing. Do not narrow the total merely to the inverter rating: the
pass-through current rating is a separate limit.

### MB-010 TODO: Nonzero Generator Telemetry

**Status:** blocked on a generator-equipped, correctly configured system.

Registers 133, 536-539, and 661-671 are currently an exact zero-only contract.
A nonzero capture must establish port mode, runtime and energy widths/scales,
voltage scales, signed power pairs, phase/total behavior, and the relationship
between `EG_P_CT1` and `GEN_P_T`. Any nonzero word continues to reject Modbus
and invoke configured cloud fallback until a complete mode-specific contract is
validated.

### MB-011 TODO: Smart Load/AUX Port

**Status:** blocked on Smart Load hardware/configuration.

Smart Load can share a configurable GEN/AUX physical port on compatible
inverters, so generator addresses cannot be reused under Smart Load labels
without evidence. With Smart Load deliberately enabled and safely exercised,
establish the register-133 mode value, identify which electrical and energy
blocks change, correlate them with the display/cloud, and implement a separate
mode-gated decoder. A nonzero mode must never silently pass through the current
generator-zero decoder.

### MB-012 TODO: Microinverter / AC-Coupled Solar

**Status:** blocked on compatible AC-coupled equipment and mode configuration.

Cloud keys `AC_S_A`, `AC_S_B`, and `AC_S_C` are zero in the
[retained fixture](../testdata/jinko_detail_response.json) and therefore supply
labels only. Establish the port mode, phase sign, total and energy semantics,
and whether AC-coupled production is already included in any load, generator,
grid, or total-solar value. The decoder must prevent double counting and remain
distinct from Smart Load and generator modes.

### MB-013 TODO: Model-Family Matrix

**Status:** 12 kW target live-verified; other accepted family ratings require
live correlation.

Repeat the fixed profile and representative telemetry validation on 6, 8, 10,
15, and 20 kW JKS family members. The accepted rating gate comes from the
official family specification, but does not prove that every firmware exposes
identical telemetry. Do not infer a telemetry limit from a 40 A or 80 A
pass-through specification.

The 25 kW two-MPPT sibling is outside `jks-6-20h-ei-readonly-v1`. The 29.9/30
kW BM3 three-MPPT variants are structurally different. Either needs a separate
named profile with independent gates and evidence; broadening the current
allowlist is not sufficient.

### MB-014 TODO: Additional MPPT Channels

**Status:** separate hardware/profile required.

The production profile requires exactly two MPPTs and zero PV3/PV4 power words.
Do not emit `DP3`/`DP4` or related voltage/current metrics as zeros. A three- or
four-MPPT device needs its own capability gate, exact register map, channel
scales, per-channel bounds, and aggregate invariant.

### MB-015 TODO: DC-Link `HV_V` and `BN_V`

**Status:** no safe Modbus address.

Jinko labels `HV_V` as HV voltage and `BN_V` as Bus-N voltage. In the retained
[cloud fixture](../testdata/jinko_detail_response.json) they are `743 V` and
`371 V`, so `BN_V` appears to be the DC bus neutral/midpoint potential, roughly
half of the full DC-link `HV_V`; it is not battery, PV, or grid voltage. That
relationship is explanatory evidence, not a mapping. Candidate addresses
601/602 conflict across inspected maps, and register 33 belongs to configuration
metadata rather than live voltage. Require two-map agreement and a live/cloud
bracket before adding either key.

### MB-016 TODO: Warning/Fault Bit Meanings

**Status:** raw U16 words are production-safe; per-bit decoding is pending.

Registers 553-558 intentionally expose six separate lossless raw words. Decode
individual flags only when a compatible firmware table documents the exact bit
and a known nonzero event confirms it. Preserve the raw words even if decoded
convenience metrics are later added. Never merge the four fault words into a
single `float64` mask, because the combined integer cannot preserve every bit
exactly.

The optional correlation worker now captures the next non-zero six-word vector,
queries both configured cloud sources, and records only sanitized comparable
metrics plus source alert points. Use that evidence together with the inverter
display and firmware table to close this TODO. Until then, the bridge may alert
on non-zero raw words but must not attach bit names, acknowledge a fault, or
perform any automatic control action.

### MB-017 TODO: Remaining Status and Metadata Keys

**Status:** intentionally excluded unless an exact contract is found.

Do not synthesize cloud `ST_PG1` or `INV_MOD1` from register 500, register 551,
or the device-type gate: their code systems and meanings differ. Firmware and
protocol versions, battery type/state, relay/contact interpretations, and
similar metadata need exact source contracts. Absence is preferable to a
numeric alias that only happens to match one fixture.

### MB-018 TODO: Wider Grid-Power Coherence Domain

**Status:** current `-32767..32767 W` domain is fail-closed; wider values need
an acquisition-coherence design and target evidence.

The 2026-08-19 production event proved that separate low/high grid-power reads
can straddle a sign transition. Do not restore the former `±65535 W` envelope
by changing only a numeric constant: phase-sum-preserving torn pairs can pass
that range. A wider contract requires a fixed, no-retry pairing strategy such
as high/low/high sign-word bracketing with identical outer high blocks, or a
single target-validated contiguous read that supplies both halves coherently.
Either design must retain one connection, fixed request order/count, canonical
sign extension, exact signed phase sum, no partial snapshot, and priority
fallback. It also needs boundary values above `32767 W`, both zero-crossing
directions, protocol golden tests, and redacted target evidence before the
production domain can widen.

## Definition of Done for a Backlog Item

An item is complete only when the smallest safe implementation, golden frames,
decoder boundary tests, end-to-end one-connection/fallback tests, redacted live
evidence, current metric/request counts, and user-facing documentation land
together. If an observed value contradicts the candidate map, record the
rejection in the validation ledger and leave the production surface unchanged.
