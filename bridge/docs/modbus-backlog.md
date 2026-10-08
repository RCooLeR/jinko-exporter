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

The current 97-metric core comprises 86 shared canonical metrics and eleven
Modbus-local fields: the register-500 run state, the register-551 power
switch state, six raw warning/fault words, and three external-CT currents.
The bounded CT and internal-power promotions record their evidence limits in
validation Stages 26-27; outstanding live checks remain MB-006/MB-007 below.
When Modbus establishes the
primary failover surface, ordinary cloud-only fields are intentionally omitted
after fallback rather than invented or emitted with competing labels. Shelly
`grid_load` enrichment is independent of this Modbus core.

Stage 31 validates the six raw metadata strings introduced in Stage 30 and
enables their four cached reads by default. It also promotes five independent
BMS mirrors (three new keys, two alias replacements). Counts are 32 first /
26 subsequent, or 28 first / 26 subsequent with metadata explicitly disabled.
The BMS voltage scale follows the HV table and matching target/cloud readings;
the LV table's different scale is not claimed to agree. The unresolved fields
within that block remain ignored, not converted into guessed values.

Open equipment-dependent items do not widen the supported contract and do not
become release blockers merely because the required equipment is unavailable.
They remain documented exclusions, and the current decoder must keep rejecting
out-of-contract states so priority fallback can run. MB-001 is an explicit
acceptance-evidence debt rather than an unbounded decoder: its current
map/schema-backed status handling is narrow, read-only, and fail-closed. Any
implementation that claims one of the open domains is blocked until that item's
validation gates are complete, except for the explicitly bounded Stage-26 CT
and Stage-27 internal-power acceptance, and the exact Stage-31 HV BMS subset.
These exceptions do not admit wider power values or the unresolved BMS fields.

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

### MB-003 TODO: Wider UPS/Backup-Load Power Domain

**Status:** phase/total publication is implemented in Stage 28; nonzero high
words remain unsupported.

Two compatible maps define unsigned low-first pairs 640/696, 641/697,
642/698, and 643/699 at `1 W/count`. A complete supervised capture measured
`34/72/295 W`, total `401 W`, with all four high words zero. The nearby
identity-matched Solarman sample measured `34/71/292 W`, total `397 W`;
the exact four-minute-32-second offset is recorded in Stage 28.

Production reuses the existing `640 x 7` block and immediately reads
`696 x 4`, replacing the redundant `643 x 1` scalar request. It emits distinct
canonical `C_P_L1..3` and preserves the existing `UPS_P` identity. Every high
word must remain zero; each low word retains its unsigned `0..65535 W`
domain, including the previous total's full range. Total is its own measured
pair, not a phase sum or an alias for `E_Puse_t1`.

This is a bounded subset of the documented U32 format, not validation of
nonzero high words or an atomic acquisition guarantee. Any nonzero high word
rejects the complete Modbus snapshot for configured fallback. Wider-domain
support requires independent nonzero-high correlation and an appropriate
low/high acquisition-coherence strategy; do not increase load or change
inverter settings merely to manufacture such a sample.

### MB-004 TODO: Lifetime PV Production (`Et_ge0`)

**Status:** mapping not established.

The 2026-10-07 follow-up found candidate low/high words 534/535 at
`0.1 kWh/count` in both inspected maps (printed page 30). The low-word row
explicitly says **reserved**. A counter name is not evidence that this firmware
implements it: neither word was queried or promoted. Fresh cloud
`Et_ge0=21610.50 kWh` does not resolve that reservation.

Locate a lifetime-PV counter whose read-only address, width, word order, and
scale agree across two compatible maps. Verify it against cloud `Et_ge0` over
multiple observations, including monotonic behavior and the distinction from
grid-side generation, import/export, and daily-PV counters. Do not promote a
counter merely because its value is numerically close to the cloud total.

### MB-005 TODO: Output Apparent Power and Power Factor

**Status:** blocked on a materially non-unity-power-factor sample.

The follow-up distinguishes three surfaces. External-grid apparent-power
candidate 620/709 is documented at `1 VA/count`; it is not load apparent power
or inverter output apparent power. Register 621 is labelled grid-connected PF
with scale 1000, but the HV map's access cell contains both `R` and `R/W`:
whether this is measured PF or a configuration/setpoint value is unresolved.
It is excluded from the prepared evidence read, and is not an alias for `P_F1`.
Load apparent candidates 654/660 and internal apparent candidates 608/704 are
marked reserved/undefined and remain excluded. Maps label output pair 637/695
as apparent power but use W, whereas canonical `O_P` uses kVA; retain this unit
ambiguity until independently resolved.

Registers 637/695 are compatible-map candidates for joined output apparent
power and are deliberately ignored today. Prove their word order, scale, and
relationship to canonical `O_P` (`kVA`) while apparent and active power differ
materially. Independently establish the meaning and scale of cloud `P_F1`; do
not derive it from rounded active/apparent values or reuse a load-side power
factor. Keep load apparent power and inverter output apparent power separate;
canonical load apparent total `E_Suse_t1` needs its own proven source.

### MB-006 TODO: Remaining External CT Acceptance Evidence

**Status:** bounded publication implemented; exact export/current correlation
and wider-power acquisition remain open, not a claim of full live validation.

The owner confirmed the native CTs are installed, supplied an import cloud
sample, and explicitly accepted signed CT publication on 2026-10-07. Stage 26
records the successful two-request raw capture and its ten-minute cloud offset.
Two compatible maps define currents 613-615 as `S16 * 0.01 A` and powers
616-619/705-708 as low-first signed 32-bit watts. Production preserves that sign
inside the conservative `-32767..32767 W` per-value coherence envelope, with no
phase-sum equality requirement. The power keys are `CT1_P_E`, `CT2_P_E`,
`CT3_P_E`, and `CT_T_E`; currents use distinct Modbus-local keys. Internal
currents 610-612 still supply `G_C_L1..3` and are never relabelled as CT.

Retain a naturally occurring export capture with contemporaneous cloud/display
CT values, plus an independently labelled CT-current comparison. User
confirmation and synthetic signed test vectors do not replace these live
checks. Do not infer a universal phase-sum invariant from the positive sample.
The current wire-domain range is not a CT capacity rating. Power above the
coherence envelope needs a separately reviewed coherent acquisition strategy
and target evidence; do not widen a constant or cap it by inverter rated power.

### MB-007 TODO: Internal and Reactive Power Surfaces

**Status:** internal active-power publication implemented after positive target
correlation; exact negative-flow capture remains acceptance-evidence debt.
Reactive-power mapping and discriminating conditions remain missing.

A newer V105.1 map alone lists phase-reactive entries at 710-715, with low/high
labels explicit only for phase A (710/711). Pairing 712/713 and 714/715 follows
the table layout but remains an inference. Access/sign/unit columns are blank;
the two compatible maps do not supply a matching contract. No read or
publication is approved from that table alone. There is also no established
direct register for `G16`, and
no documented inverter-phase reactive mapping. Do not invent a total or reuse
active/apparent registers.

The owner's `18:43:51Z` cloud sample reports inverter reactive powers
`6666/1813/9782 Var` alongside phase voltages `236.9/231.9/240.6 V` and currents
`-0.2/-0.2/0.6 A`. Their magnitudes are inconsistent with those voltage/current
readings, even allowing rounding. This is a reference-data anomaly, not valid
calibration evidence; matching those cloud numbers would not establish a
correct decoder.

Reference: [V105.1, printed page 40](https://github.com/user-attachments/files/16798469/MODBUS.RTU.V105.1-20231006.pdf).

Stage 27 records the supervised fixed FC03 `604 x 4`, then `700 x 4` read.
V104-2 printed pages 32/37 and the SG01-HP3-AM2 HV map printed pages 33/38 agree
on read-only signed low-first `1 W/count` pairs 604/700 through 607/703. High
words start at **700**, not 701; reserved apparent-power candidates 608/704
remain excluded. Production preserves the four independent `GS_*` values with
canonical sign extension and the `-32767..32767 W` coherence envelope.

Local powers `98/86/148 W`, total `332 W`, closely matched the owner's fresh
`17:31:27Z` cloud `98/84/148 W`, total `330 W`, during a reported stable load.
The sample is positive with all-zero high words, not proof of a negative raw
pair or wide power domain. Retain a naturally occurring negative-flow capture
with the corresponding cloud/display values; wider power needs coherent
acquisition evidence. Do not infer a universal phase-sum equality or an alias
between internal, external CT, and selected grid power.

Cloud keys `GS_A`, `GS_B`, `GS_C`, and `GS_T` represent internal power, not the
selected grid phase/total powers. Reactive keys `G16`, `A_RP_PG`,
`B_RP_PG`, `C_RP_PG`, `A_RP_INV`, `B_RP_INV`, and `C_RP_INV` likewise require
their own documented register pairs, signs, scales, and totals. Validation needs
a sample with material reactive power; zeros or near-unity power factor cannot
distinguish aliases.

### MB-008 TODO: Extended BMS and Second-Battery Data

**Status:** five BMS mirrors promoted in Stage 31; current encoding, zero-only
fields and the other BMS/second-battery surfaces remain open.

The 2026-10-07 audit identified inverter-side BMS mirrors 210-218 and prepared
a fixed raw-only `210 x 9` diagnostic. These are explicitly **R/W** registers;
the proposed exception permits only FC03 reads, never a write or a production
promotion by itself. Both maps agree on their meanings, but the HV map specifies
`0.1 V` for 210/211/215 and `0.1 A` for 216, versus `0.01 V` and `1 A` in the
LV tables. The third inspected map repeats the LV scales. Current signedness
is unspecified in the mirror rows. SOC 214 is percent, temperature 217 is
`(raw-1000)/10 C`, and limits 212/213/218 use A.

The earlier 2026-10-07 cloud sample has BMS voltage `598.3 V`, current `-0.1 A`, temperature
`23 C`, SOC `100%`, charge voltage `648 V`, and discharge current limit `22 A`.
These are comparison candidates, not a local register capture. In particular,
a future raw `FFFF` current alone must be distinguished from an unsupported
sentinel before claiming a full signed-current contract.

Register 219 has an unspecified unit; 224-226 omit detailed access/format
information. Register 227 conflicts critically: battery hardware version in
the LV table, LCD-upgrade test in the HV table. The separate 10000+ read-only
bank describes battery device type `0x0700`, not this inverter's `0x0006`;
its accessibility through this inverter is unproven. Extended pack layouts and
battery2 mirrors also conflict. None of these ranges was queried. Lithium
alarm/fault words 220/221 need their own raw-word contract and must not be
equated with inverter warning/fault words.

The 2026-10-08 supervised capture at `06:01:07Z` and identity-matched cloud
sample collected at `06:02:38Z` agree on all nine observed mirror values under
the HV scales. The accepted subset is R210 `BMS_C_V=648 V`, R213
`BMS_D_C_L=22 A`, R214 `BMS_SOC=100%`, R215 `BMS_B_V1=615.2 V`, and R217
`BMST=22 C`. The SOC/temperature values replace the former R588/R586 aliases;
inverter `B_left_cap1` and `B_T1` still use their own registers. R213's
nonnegative U16 whole-amp domain is a wire representation, not a battery rating
or a claim that every extreme has been live-observed.

R216 was `0001`, matching cloud `+0.1 A`. Negative current is a normal valid
battery operating condition (also confirmed by the owner), but this capture
does not prove its register encoding or any unsupported sentinel. R216 remains
unpublished; it is neither forced positive nor made a rejection gate. R211,
R212 and R218 were zero only and likewise remain unpublished pending nonzero
correlation. Changes to these four ignored words do not invalidate the verified
subset. The one exact `210 x 9` FC03 request is performed each fetch; protocol
failures or invalid published-field domains reject the complete snapshot.

Do not alias `BMS_B_V1` to `B_V1` or `BMS_B_C1` to `B_C1`: the
[retained cloud fixture](../testdata/jinko_detail_response.json) contains
different simultaneous values for each pair. The remaining BMS current,
charge/discharge limits, SOH, capacity, cycle/state data, cell diagnostics, and
all `bms2` fields still need their own evidence. Jinko
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

**Status:** six raw strings target-validated and enabled by default in Stage 31;
all other status interpretations remain open.

Both compatible maps (printed pages 4-5) agree on read-only protocol-version
word 2, assistant/bootloader bytes in word 11, firmware words 14-18, and battery
input code 24. The fixed raw-only diagnostic for `2 x 1`, `11 x 1`, `14 x 5`,
`24 x 1` succeeded on the target at `2026-10-08T06:01:25Z`. It skips
conflicting words 12/13 and unrelated adjacent configuration. Firmware must be
represented as lossless text metadata, not decimal float values such as `104`
for cloud version `0104`. A source string or metadata field is not automatically
a canonical numeric sensor.

Stage 30 implemented those exact four cached reads behind a default-off flag;
Stage 31 validates them and changes the default to `MODBUS_METADATA_ENABLED=true`.
An explicit false remains an opt-out. MQTT/HA diagnostic discovery
and the six-key Prometheus info allowlist have explicit format, source-fallback,
availability and independent-Shelly tests. No cloud-version aliases or battery
count are inferred. Protocol failures, cancellation and unsupported R24 codes
leave no partially accepted profile/metadata cache. Sanitized complete target
responses are retained as parser/decoder regressions. This evidence confirms
the raw strings on the 12 kW target, not all sibling models or cloud-composite
aliases. R24 remains a raw code rather than an inferred battery count.

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
