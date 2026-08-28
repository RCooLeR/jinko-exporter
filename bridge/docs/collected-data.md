# Collected Data

The bridge normalizes source responses into one snapshot shape. Only numeric telemetry becomes Prometheus metrics and Home Assistant metric sensors. Non-numeric metadata is kept as snapshot metadata and, when MQTT is enabled, as diagnostic entities.

## Snapshot Shape

The `fetch` command prints the normalized snapshot:

```json
{
  "source": "jinko",
  "device_sn": "SYNTHETIC_INV_001",
  "parent_sn": "LOGGER123",
  "device_id": "100000001",
  "site_id": "200000001",
  "collected_at": "2026-05-06T12:00:00Z",
  "metrics": [
    {
      "group": "grid",
      "key": "PG_Pt1",
      "name": "Total Grid Power",
      "unit": "W",
      "value": 1234
    }
  ],
  "meta": {
    "base_url": "https://globalapi.solarmanpv.com"
  }
}
```

## Metric Fields

| Field | Meaning |
| --- | --- |
| `group` | Logical metric group such as `grid`, `battery`, `electric`, or `alert`. |
| `key` | Stable source or canonical key. Metrics recognized by the shared Jinko dictionary use the same key across Jinko, Solarman, and local Modbus. |
| `name` | Human-readable metric name. Home Assistant uses this as the default sensor name. |
| `unit` | Source unit, normalized for common Home Assistant units. |
| `value` | Numeric value as `float64`. Non-numeric source fields are skipped. |

## Source Behavior

### Jinko

The Jinko parser reads:

- `deviceSn`
- `parDeviceSn`
- `deviceId`
- `siteId`
- `collectionTime`
- numeric values from `paramCategoryList[].fieldList[]`

For each field, the bridge prefers `orgValue`, falls back to `value`, parses the result as a number, and skips the field when parsing fails.

Jinko metric keys are normalized this way:

- `storageName` is preferred as the metric `key`.
- If `storageName` is missing, the displayed field name is sanitized.
- Known Jinko keys and names are canonicalized into stable groups, names, and units.

### Solarman

The Solarman parser reads `dataList` from `/device/v1.0/currentData` and accepts common point fields:

- key: `key`, `dataKey`, `id`, or `sn`
- name: `name`, `dataName`, `title`, or `paramName`
- unit: `unit` or `dataUnit`
- value: `value` or `val`

Solarman groups are inferred from the key and name, but every point recognized by the shared Jinko metric dictionary is always canonicalized to that dictionary's key, group, name, and unit. Unrecognized Solarman-only points remain available in compatibility mode. `SOLARMAN_CANONICAL_JINKO_METRICS=true` is the legacy-named strict-surface switch: it filters those unknown points and keeps only metrics in the shared dictionary. If the option is not set, it defaults to `EXPORTER_METRICS_DROP_SOURCE_LABEL`.

For the two-MPPT PV surface, Jinko, known Solarman points, and local Modbus use the same canonical identities: `DP1`/`DP2` (`electric`, `DC Power PV1`/`DC Power PV2`, `W`), `DV1`/`DV2` (`electric`, `DC Voltage PV1`/`DC Voltage PV2`, `V`), `DC1`/`DC2` (`electric`, `DC Current PV1`/`DC Current PV2`, `A`), and `S_P_T` (`electric`, `Total Solar Power`, `W`). This shared key/group/name/unit contract is what lets one Home Assistant entity set and one Prometheus label set survive source failover.

For a source-independent priority deployment, set `EXPORTER_METRICS_DROP_SOURCE_LABEL=true`. Unless explicitly overridden, that setting also enables `EXPORTER_SOURCE_PROJECT_FAILOVER_METRICS` and strict shared-dictionary filtering through `SOLARMAN_CANONICAL_JINKO_METRICS`. After the primary surface has been learned, a matching fallback metric is published with the primary surface's group, key, name, and unit; fallback-only ordinary telemetry is omitted. Together, stable labels and removal of the `source` label prevent separate source-specific Prometheus series from appearing as duplicate lines in Grafana. Warning/alarm/fault metrics are not projected onto the primary telemetry surface and retain their source-local domains.

Home Assistant Discovery can persist the corresponding schema independently with `MQTT_DISCOVERY_STATE_FILE`. The configured first-priority source contributes a monotonic ordinary metric surface; a cold fallback cannot add fallback-only ordinary entities. Source-local warning/alarm/fault metrics and Shelly `grid_load` metrics form separate monotonic unions. Ordinary values missing from the current snapshot are explicitly `null`, and missing-safe templates make those entities `unknown` without treating absence as zero or producing template warnings. Alert entities instead combine global bridge availability with an exact manifest-derived source/key condition: a finite zero remains `0`/`OFF`, a non-zero value is active, and an inactive source, missing key, or non-finite value is `unavailable`. This persistence changes only retained MQTT entity ownership and state shaping; it does not change the normalized snapshot returned by `fetch` or the source-selection behavior used by Prometheus.

### Shelly `grid_load` Enrichment

Shelly Pro 3EM support is an optional enrichment source, not an inverter-source
candidate. After the configured Jinko, Solarman, or Modbus source returns a
complete snapshot, the bridge reads `EM.GetStatus` and `EMData.GetStatus` from
the configured Shelly and appends every available value below with group
`grid_load`. A Shelly error logs a warning and omits the complete enrichment for
that poll; it does not fail or replace the successful inverter snapshot.

| Surface | Keys | Maximum |
| --- | --- | ---: |
| Per-phase live values for `l1`, `l2`, and `l3` | `<phase>_voltage`, `<phase>_current`, `<phase>_power`, `<phase>_apparent_power`, `<phase>_power_factor`, `<phase>_frequency` | 18 |
| Whole-meter live values | `neutral_current`, `total_current`, `total_power`, `total_apparent_power` | 4 |
| Per-phase energy for `l1`, `l2`, and `l3` | `<phase>_energy_total`, `<phase>_returned_energy_total` | 6 |
| Whole-meter energy | `energy_total`, `returned_energy_total` | 2 |

The maximum surface is 30 metrics. Shelly RPC fields are optional, so a valid
response can contain fewer; unavailable values are omitted rather than filled
with zero. Power is reported in `W`, apparent power in `VA`, voltage in `V`,
current in `A`, frequency in `Hz`, power factor with an empty unit, and Shelly
active/returned energy is converted from `Wh` to `kWh`.

The final snapshot keeps the winning inverter source identity. Consequently,
the Prometheus `source` label on a `grid_load` metric, when enabled, is
`modbus`, `jinko`, or `solarman`—it is **not** `shelly_grid_load`. The
`group="grid_load"` label is the stable marker that the value originated from
the configured Shelly. With `EXPORTER_METRICS_DROP_SOURCE_LABEL=true`, the
source label is absent as expected. Shelly metrics are appended after priority
selection and are not canonical fallback projections.

### Local Modbus

The target-locked local profile is intentionally smaller than the cloud sources. It exposes only documented, reviewed read-only values through fixed FC03 requests and field-specific validation. Evidence differs by range: most production fields have target-live validation, while repeated complete target fetches prove transport and narrow decoder acceptance for registers 551-552 without yet retaining their exact raw words or a simultaneous relay-state bracket. The exact evidence status and accepted domain for every range are recorded in the [Modbus validation ledger](./modbus-validation.md); hardware- and condition-dependent gaps are tracked in the [Modbus validation backlog](./modbus-backlog.md). Register numbers below are zero-based **decimal** addresses; hexadecimal equivalents are included to make the wire ranges unambiguous.

| Decimal register read | Hex register read | Metric key(s) | Conversion |
| --- | --- | --- |
| `133` | `0x0085` | none (generator zero-domain gate) | must equal the exact live-observed raw value `0x0000` on every fetch; the mutable value is not cached or emitted |
| `500-505` | `0x01F4-0x01F9` | `DEYE_MODBUS_R500_RUN_STATE` | only register 500 is emitted as a source-local group `status`, empty-unit U16 enum: 0 standby, 1 self-check, 2 normal, 3 alarm, 4 fault, 5 activating; codes above 5 reject the snapshot and registers 501-505 are ignored |
| `514-529` | `0x0202-0x0211` | `Etdy_cg1`, `Etdy_dcg1`, `t_cg_n1`, `t_dcg_n1`, `E_B_D`, `E_S_D`, `E_B_TO`, `E_S_TO`, `Etdy_use1`, `E_C_T`, `PV_D_P_G`, `Etdy_ge1` | daily U16 and lifetime U32 low-word-first counters, each multiplied by `0.1 kWh`; register 529 intentionally supplies both daily-PV canonical keys |
| `536-539` | `0x0218-0x021B` | `R_T_D`, `GEN_P_D`, `GEN_P_TO` | exact zero-domain only: all four raw words must remain zero and the three canonical values are emitted as zero; a nonzero word rejects the snapshot before any scale or word-pairing interpretation |
| `540-541` | `0x021C-0x021D` | `T_DC`, `AC_T` | each documented read-only U16 uses `(raw - 1000) × 0.1 °C`; raw values above 3000 or decoded values outside `-100..100 °C` reject the whole snapshot |
| `551-552` | `0x0227-0x0228` | `DEYE_MODBUS_R551_POWER_SWITCH_STATE`, `AC` | register 551 emits only its documented low-nibble power-switch code (`0` off, `1` on) as a source-local group-`status` metric; codes 2-15 reject the snapshot and undefined upper bits are ignored. Register 552 is emitted unchanged as the canonical group-`status` raw U16 AC relay mask; all bits are preserved, including undefined or firmware-specific bits |
| `553-558` | `0x0229-0x022E` | `DEYE_MODBUS_R553_WARNING_WORD_1_RAW`, `DEYE_MODBUS_R554_WARNING_WORD_2_RAW`, `DEYE_MODBUS_R555_FAULT_WORD_1_RAW`, `DEYE_MODBUS_R556_FAULT_WORD_2_RAW`, `DEYE_MODBUS_R557_FAULT_WORD_3_RAW`, `DEYE_MODBUS_R558_FAULT_WORD_4_RAW` | six independent unsigned raw 16-bit words at scale 1, group `alert`, empty unit; no bit decoding or composite mask |
| `586` | `0x024A` | `B_T1`, `BMST` | `(unsigned raw - 1000) × 0.1 °C`, with documented and plausible-range validation; the one decoded value is exposed through both canonical cloud keys |
| `587-588` | `0x024B-0x024C` | `B_V1`, `B_left_cap1`, `BMS_SOC` | voltage `raw × 0.1 V`; SOC `raw × 1%` and must be at most 100; the one validated register-588 SOC value is exposed through both canonical cloud keys |
| `590-591` | `0x024E-0x024F` | `B_P1`, `B_C1` | signed 16-bit power `× 10 W` and signed 16-bit current `× 0.01 A` |
| `598-600` | `0x0256-0x0258` | `G_V_L1`, `G_V_L2`, `G_V_L3` | unsigned register value multiplied by `0.1 V` |
| `609-619` | `0x0261-0x026B` | `PG_F1`, `G_C_L1`, `G_C_L2`, `G_C_L3` | only register 609 is emitted as `U16 × 0.01 Hz` and internal currents 610-612 as `S16 × 0.01 A`; values are bounded to `0..100 Hz` and `±100 A`; external-CT currents and incomplete power low words 613-619 are ignored |
| `622-625` and `687-690` | `0x026E-0x0271` and `0x02AF-0x02B2` | `G_P_L1`, `G_P_L2`, `G_P_L3`, `PG_Pt1` | true signed 32-bit, low-word-first values in W; positive is import and negative is export; each high word must be `0x0000` or `0xFFFF`, the full joined value—not low-word bit 15—determines sign, every value must remain inside the conservative `-32767..32767 W` torn-pair coherence envelope, and the phase sum must equal the total |
| `627-638` and `691-695` | `0x0273-0x027E` and `0x02B3-0x02B7` | `AV1`, `AV2`, `AV3`, `AC1`, `AC2`, `AC3`, `A_Fo1`, `INV_O_P_L1`, `INV_O_P_L2`, `INV_O_P_L3`, `INV_O_P_T` | voltage is `U16 × 0.1 V`, current `S16 × 0.01 A`, and frequency `U16 × 0.01 Hz`; active words pair low-first as signed 32-bit values, require high words `0x0000` or `0xFFFF`, remain inside the conservative `-32767..32767 W` coherence envelope, and require the exact signed phase sum to equal the total; apparent candidate 637/695 is ignored |
| `643` | `0x0283` | `UPS_P` | this separate one-register request emits register 643 as an unsigned U16 value in W; no high word or phase semantics are inferred |
| `640-646` | `0x0280-0x0286` | `C_V_L1`, `C_V_L2`, `C_V_L3` | this block's voltage decoder emits only registers 644-646 as `U16 × 0.1 V`; it ignores registers 640-643. Register 643 is independently requested and emitted as `UPS_P` by the preceding row; full phase/total power pairing with candidate high words 696-699 remains backlog work |
| `650-653` and `656-659` | `0x028A-0x028D` and `0x0290-0x0293` | `LPP_A`, `LPP_B`, `LPP_C`, `E_Puse_t1` | phase pairs 650/656 through 652/658 are low-first signed 32-bit W with canonical `0000/FFFF` sign extension and a conservative `-32767..32767 W` torn-pair coherence envelope; dedicated total 653/659 remains independently in the zero-high, numerically U16 `0..65535 W` subset of its documented signed wire pair; no phase-sum equality is imposed and `C_P_L1..3` aliases remain excluded |
| `655-659` | `0x028F-0x0293` | `L_F` | register 655 is `U16 × 0.01 Hz`; registers 656-658 are additionally consumed as canonical signed phase high words, while dedicated-total high word 659 must remain zero |
| `661-671` | `0x0295-0x029F` | `GEN_P_L1`, `GEN_P_L2`, `GEN_P_L3`, `GEN_V_L1`, `GEN_V_L2`, `GEN_V_L3`, `EG_P_CT1`, `GEN_P_T` | exact zero-domain only: all eleven raw words must remain zero and the eight canonical values are emitted as zero; `EG_P_CT1` is a zero-only compatibility view of the same observed total as `GEN_P_T`, with no nonzero equivalence inferred |
| `672-679` | `0x02A0-0x02A7` | `DP1`, `DP2`, `DV1`, `DC1`, `DV2`, `DC2`, `S_P_T` | for gated type `0x0006`/2-MPPT: registers 672/673 are PV1/PV2 power at `10 W/count`, 676/678 are PV1/PV2 voltage at `0.1 V/count`, and 677/679 are PV1/PV2 current at `0.1 A/count`; registers 674-675 are the PV3/PV4 power words and must both remain zero; each channel and `S_P_T=(register 672 + register 673) × 10 W` are bounded to twice rated power, voltage to `1000 V`, and current to `100 A` |

Registers 553-558 are interpreted only as a six-word raw alert vector: a
complete valid Modbus snapshot with all six words equal to zero explicitly
reports no Modbus alert, while any non-zero word activates the Modbus alert
domain. The optional correlation worker sends the detected/recovered Home
Assistant notification and queries Jinko and Solarman only for bounded,
sanitized comparison evidence. Cloud results never clear the Modbus alert,
change the winning telemetry source, or assign unverified meanings to bits;
only a later complete Modbus snapshot containing six zeros resolves it.

Before telemetry, the client performs two profile gates together and caches them only after both succeed: decimal register `0` (`0x0000`) must contain device type `0x0006`, and decimal registers `20-22` (`0x0014-0x0016`) must decode to one official JKS-6/8/10/12/15/20H-EI rating (`6000`, `8000`, `10000`, `12000`, `15000`, or `20000 W`), exactly `2 MPPT`, `3 phases`, and raw register 22 `0x0203`. The 12 kW target is the only member with live-confirmed raw gate values; the other accepted ratings come from the official line specification and have not been individually live-correlated. The unvalidated 25 kW two-MPPT sibling is outside `jks-6-20h-ei-readonly-v1`; the structurally separate 29.9/30 kW BM3 three-MPPT variants also fail closed. Rated power is emitted as canonical `Pr1` in every snapshot and all three capabilities are retained in snapshot metadata. Registers `20-22` are read-only inherent properties; the unrelated R/W clock registers at decimal `62-64` are excluded. The device-type code is metadata, not Jinko `INV_MOD1`, because those fields use different code systems.

Every fetch opens one fresh short-lived TCP connection and runs all required fixed blocks sequentially under one shared absolute deadline. The first fetch sends exactly twenty-four allowlisted FC03 requests on that connection: two cached profile gates followed by twenty-two every-fetch reads. Once both gates have succeeded and are cached, each later fetch opens a new connection and sends exactly twenty-two requests. The first three every-fetch reads are the uncached generator zero-domain gate and its two telemetry blocks; the remaining nineteen retain their fixed order. Every request is sent once in the fixed allowlisted order. The first transport, protocol, profile, decode, or range error aborts the fetch, closes the connection, and returns no snapshot; there is no retry or reconnect within that fetch. The source has no arbitrary-register or Modbus-write API and never fills missing cloud metrics with guessed values. Output active low words in the existing `627 x 12` block are paired with the immediately following `691 x 5` high-word read. Only the four active pairs are emitted and only inside the conservative signed coherence envelope; apparent candidate 637/695 remains excluded. The existing `672 x 8` PV response emits `DP1`, `DP2`, `DV1`, `DC1`, `DV2`, `DC2`, and aggregate `S_P_T`; no additional PV request was added, and `DP3`/`DP4` remain excluded behind the mandatory zero gate at registers 674-675. Promoting the six PV-channel metrics first grew the locked core snapshot from 52 to 58 metrics; the three direct-load phases and second canonical view of the already decoded SOC then brought the surface to 62. The eleven raw-backed generator zeros brought it to 73, and `INV_O_P_L1..3` plus `INV_O_P_T` brought it to 77. Canonical `BMST` reuses the validated register-586 temperature, while the new `551 x 2` status read adds source-local `DEYE_MODBUS_R551_POWER_SWITCH_STATE` and canonical `AC`, bringing the current surface to 80 metrics. Register 529 above is a separate documented daily energy counter. `MODBUS_DEVICE_SN` becomes `device_sn`; it is required in a mixed priority chain and must equal the inverter serial exposed by the cloud sources so Prometheus does not create a parallel identity series. The required logger serial remains separate as `parent_sn`.

The generator one-shot used one connection for exactly three sequential unit-1 FC03 reads, with no retry: `133 x 1` at sequence `0x16`, `536 x 4` at `0x17`, and `661 x 11` at `0x18`. It returned `R133=0` and every word in the two telemetry blocks equal to zero. This is consistent with previously observed cloud generator zeros, but no fresh cloud bracket was available because the bridge was stopped for the isolated read. Production therefore claims only the observed zero domain: it reads all three blocks every time, emits the eleven canonical metrics only after every word passes the zero gate, and rejects the complete Modbus snapshot on any nonzero word so priority can fall back to Jinko or Solarman. No generator scale, sign, low/high pairing, or nonzero alias equivalence is inferred.

The six 553-558 values deliberately bypass the Jinko canonical schema. Every U16 value from `0` through `65535` is represented exactly by the bridge's `float64` metric value. The four fault words are never combined into a `uint64`/`float64` mask, because such a composite would exceed exact `float64` integer precision and lose forensic bits. The Jinko cloud keys `L_B_A_F`, `L_B_F_F`, `L_B_A_F2`, and `L_B_F_F2` remain lithium-BMS fields and are not aliases for these inverter-wide Deye words.

The run-state and register-551 power-switch metrics are deliberately source-local. Neither is emitted as `ST_PG1` or `INV_MOD1`: those Jinko keys use different upstream contracts. Registers 501-505 in the Deye run-state block failed the target cloud energy correlation and remain excluded. Register 551 likewise has no exact cloud alias: only its documented low nibble is emitted, while upper bits remain uninterpreted. Register 552 does have the exact shared canonical `AC` contract and is therefore emitted through that key without bit filtering.

For register 609, the two primary protocol maps establish the read-only address and grid-frequency meaning; the `0.01 Hz` scale comes from the maintained Deye P3 profile and the reviewed live/cloud correlation (`4995` raw against approximately `50.00 Hz`). Registers 610-612 are the documented internal grid-current triplet and correlated more closely than the external-CT triplet. Arbitrary changes in ignored registers 613-619 do not change any emitted value.

The direct-load decoder promotes the phase pairs 650/656, 651/657, and 652/658 as canonical `LPP_A/B/C`, plus dedicated total pair 653/659 as `E_Puse_t1`. An earlier reviewed positive sample had low words `19/34/248/301` and four zero high words. A later same-frame read produced a phase-word sum of `243 W` while dedicated total register 653 was `251 W`; that live counterexample proves phase-sum equality is not a valid production invariant, so all four values are independent and the sum is never a gate. A later production poll observed `R656=0xFFFF`, proving that phase-A sign extension occurs, although the error log did not retain the paired low word or the remaining high words. Production therefore accepts each phase only with canonical high `0x0000`/`0xFFFF` and a joined value inside `-32767..32767 W`. This symmetric phase envelope rejects both directions of an in-domain zero-crossing tear. Dedicated total remains deliberately narrower in meaning but wider in non-negative magnitude: R659 must be zero and R653 accepts `0..65535 W`. Thus no unverified signed-total contract or `Pr1`-derived cap is introduced, and pass-through total headroom is preserved. `C_P_L1..3` aliases are not emitted. Register 588 likewise supplies both canonical SOC keys, `B_left_cap1` and `BMS_SOC`, with identical validated values. Register 586 supplies both canonical temperature keys, `B_T1` and `BMST`; neither alias is synthesized as zero.

Output active power has a separate, stricter signed contract. The positive live pair was `879/883/1092/2854 W` in registers 633-636 with active high words 691-694 all zero, and the phase sum was exact. A later production failure recorded `R691=0xFFFF`, establishing that the target does enter the negative sign-extension domain; that log did not preserve the paired low words or the other active high words, so no exact negative magnitude is claimed from it. Production reads `627 x 12` and `691 x 5` consecutively, joins active pairs low-word-first as signed 32-bit values, accepts high words only when they are `0x0000` or `0xFFFF`, limits every joined phase and total to `-32767..32767 W`, and requires the exact signed phase sum to equal the dedicated total. The symmetric `0x7FFF` guard is not a rated-power cap: it makes a low word and stale sign-extension word decode outside the accepted envelope if the adjacent reads straddle zero. Any failed gate rejects the complete Modbus snapshot for priority fallback. Registers 637/695 remain ignored and `O_P` is not synthesized.

## Groups

| Group | Typical data |
| --- | --- |
| `basic` | Inverter type, rated power, basic electrical limits. |
| `version` | Firmware and protocol version numbers. |
| `electric` | PV strings, AC output, inverter output, production totals. |
| `grid` | Grid voltage, current, power, frequency, import/export energy. |
| `grid_load` | Optional Shelly Pro 3EM grid-load phase/total electrical and energy values. |
| `consumption` | Load voltage, load power, total consumption, daily consumption. |
| `battery` | Battery voltage, current, power, SOC, charge/discharge energy. |
| `bms` / `bms2` | BMS voltage, current, temperature, current limits, SOH, capacity. |
| `temperature` | Battery, DC, and AC temperatures. |
| `status` / `state` | Numeric status values. |
| `alert` | Alarm/fault flags and source-specific raw warning/fault words. |
| `generator` | Generator voltage, power, runtime, and production. |
| `ups` | UPS load power. |
| `inverter` | Solarman fallback group for generic inverter points. |

## Common Canonical Metrics

The exact set depends on the inverter, source, firmware, and upstream API response. These are the keys most commonly used by the bridge docs and cards.

| Key | Group | Name | Unit |
| --- | --- | --- | --- |
| `DV1` | `electric` | DC Voltage PV1 | `V` |
| `DC1` | `electric` | DC Current PV1 | `A` |
| `DP1` | `electric` | DC Power PV1 | `W` |
| `DV2` | `electric` | DC Voltage PV2 | `V` |
| `DC2` | `electric` | DC Current PV2 | `A` |
| `DP2` | `electric` | DC Power PV2 | `W` |
| `S_P_T` | `electric` | Total Solar Power | `W` |
| `Etdy_ge1` | `electric` | Daily Production (Active) | `kWh` |
| `Et_ge0` | `electric` | Cumulative Production (Active) | `kWh` |
| `INV_O_P_L1` | `electric` | Inverter Output Power L1 | `W` |
| `INV_O_P_L2` | `electric` | Inverter Output Power L2 | `W` |
| `INV_O_P_L3` | `electric` | Inverter Output Power L3 | `W` |
| `INV_O_P_T` | `electric` | Total Inverter Output Power | `W` |
| `G_V_L1` | `grid` | Grid Voltage L1 | `V` |
| `G_C_L1` | `grid` | Grid Current L1 | `A` |
| `G_P_L1` | `grid` | Grid Power L1 | `W` |
| `G_V_L2` | `grid` | Grid Voltage L2 | `V` |
| `G_C_L2` | `grid` | Grid Current L2 | `A` |
| `G_P_L2` | `grid` | Grid Power L2 | `W` |
| `G_V_L3` | `grid` | Grid Voltage L3 | `V` |
| `G_C_L3` | `grid` | Grid Current L3 | `A` |
| `G_P_L3` | `grid` | Grid Power L3 | `W` |
| `PG_F1` | `grid` | Grid Frequency | `Hz` |
| `PG_Pt1` | `grid` | Total Grid Power | `W` |
| `E_B_D` | `grid` | Daily Energy Buy | `kWh` |
| `E_S_D` | `grid` | Daily energy sell | `kWh` |
| `E_B_TO` | `grid` | Total Energy Buy | `kWh` |
| `E_S_TO` | `grid` | Total Energy Sell | `kWh` |
| `E_Puse_t1` | `consumption` | Total Consumption Power | `W` |
| `LPP_A` | `consumption` | Load phase power A | `W` |
| `LPP_B` | `consumption` | Load phase power B | `W` |
| `LPP_C` | `consumption` | Load phase power C | `W` |
| `Etdy_use1` | `consumption` | Daily Consumption | `kWh` |
| `E_C_T` | `consumption` | Total Consumption | `kWh` |
| `C_P_L1` | `consumption` | Load Power L1 | `W` |
| `C_P_L2` | `consumption` | Load Power L2 | `W` |
| `C_P_L3` | `consumption` | Load Power L3 | `W` |
| `C_V_L1` | `consumption` | Load Voltage L1 | `V` |
| `C_V_L2` | `consumption` | Load Voltage L2 | `V` |
| `C_V_L3` | `consumption` | Load Voltage L3 | `V` |
| `L_F` | `consumption` | Load Frequency | `Hz` |
| `B_V1` | `battery` | Battery Voltage | `V` |
| `B_C1` | `battery` | Battery Current | `A` |
| `B_P1` | `battery` | Battery Power | `W` |
| `B_left_cap1` | `battery` | SoC | `%` |
| `Etdy_cg1` | `battery` | Daily Charging Energy | `kWh` |
| `Etdy_dcg1` | `battery` | Daily Discharging Energy | `kWh` |
| `BMS_SOC` | `bms` | BMS_SOC | `%` |
| `BMST` | `bms` | BMS Temperature | `C` |
| `Li_B_SOH` | `bms` | Lithium battery SOH | `%` |
| `T_DC` | `temperature` | DC Temperature | `C` |
| `AC_T` | `temperature` | AC Temperature | `C` |
| `AC` | `status` | AC side relay status | none |
| `L_B_A_F` | `alert` | Lithium battery alarm flag | none |
| `L_B_F_F` | `alert` | Lithium battery fault flag | none |
| `GEN_P_L1` | `generator` | Gen Power L1 | `W` |
| `GEN_P_L2` | `generator` | Gen Power L2 | `W` |
| `GEN_P_L3` | `generator` | Gen Power L3 | `W` |
| `GEN_V_L1` | `generator` | Gen Voltage L1 | `V` |
| `GEN_V_L2` | `generator` | Gen Voltage L2 | `V` |
| `GEN_V_L3` | `generator` | Gen Voltage L3 | `V` |
| `EG_P_CT1` | `generator` | Generator Active Power | `W` |
| `GEN_P_T` | `generator` | Total Gen Power | `W` |
| `GEN_P_D` | `generator` | Daily Production Generator | `kWh` |
| `GEN_P_TO` | `generator` | Total Production Generator | `kWh` |
| `R_T_D` | `generator` | Gen Daily Run Time | `h` |
| `UPS_P` | `ups` | UPS Load Power | `W` |

## Sanitization

Metric keys and Home Assistant state keys are sanitized to lowercase ASCII identifiers for MQTT JSON keys:

- lowercase
- non-ASCII and punctuation become `_`
- repeated separators collapse
- leading and trailing `_` are removed

Example:

```text
group = "grid"
key = "PG_Pt1"
state key = "grid_pg_pt1"
```

Home Assistant then creates entity IDs from the discovery name and device context. See [Home Assistant MQTT Discovery](./home-assistant.md) for the full naming pattern.
