# Local Modbus Validation Ledger

The local Modbus source grows through fixed, documented FC03 blocks. A normal
scaled numeric promotion is not added to production polling until all of these
gates pass:

1. The same register meaning and read-only status are confirmed in at least
   two compatible Deye protocol maps.
2. The exact Solarman V5 request, Modbus CRC, outer checksum, and parser are
   covered by offline golden tests.
3. A separate one-shot tool reads the block once without retry or polling.
4. Raw values, scale, sign, and word order are compared with vendor-cloud or
   inverter-display values.
5. Only the confirmed fields are promoted into the production allowlist.

A narrower raw-status or same-register canonical-alias stage may be admitted
without a new target read only when a later section explicitly records the
missing evidence, two compatible read-only maps agree on the exact field, the
existing canonical schema has the same raw contract, and production does not
decode undocumented bits. That exception applies only to Stage 21 below; its
first target read remains an acceptance task rather than evidence already
claimed by this ledger.

The generator zero-domain stage below is deliberately narrower than a normal
numeric promotion: its exact local words were all zero, consistent with older
cloud zeros, but there was no fresh simultaneous cloud bracket. Production
therefore accepts and emits only zero; any nonzero word rejects the complete
Modbus snapshot before scale, sign, word pairing, or nonzero alias semantics
can be inferred.

There is no raw-register option and no sequential scan of undocumented,
reserved, configuration, or control addresses.

This ledger records completed or explicitly bounded production evidence. See
the [Modbus validation backlog](./modbus-backlog.md) for open acceptance reads,
hardware-dependent domains, intentionally excluded candidates, and the gates
required before any of them can be promoted.

## Validation Status

| Registers | Status | Purpose |
| --- | --- | --- |
| `0x0000 x 1` | Live verified, production canary | Device type must be `0x0006`. |
| `0x0256 x 3` | Live verified, production | Grid L1/L2/L3 voltage. |
| `0x0283 x 1` (`643`) | Live verified, production narrow scalar | A separate one-register read emits `UPS_P` as unsigned U16 watts. It is not treated as a low word in production; full phase/total pairing remains backlog work. |
| `0x01F4 x 6` (`500-505`) | Live verified; register 500 production, registers 501-505 excluded | Run state is promoted source-locally; the energy/time meanings remain rejected after failing cloud correlation. |
| `0x024B x 2` (`587-588`) | Live verified, production | Battery-1 voltage and SOC; the validated SOC is exposed as both canonical `B_left_cap1` and `BMS_SOC`. |
| `0x024A x 1` (`586`) | Live verified, production | Battery-1 temperature; one decoded value supplies canonical `B_T1` and `BMST`. |
| `0x02A0 x 8` (`672-679`) | Live/protocol verified, production | Emits PV1/PV2 `DP1`, `DP2`, `DV1`, `DC1`, `DV2`, `DC2`, and aggregate `S_P_T` for the gated 2-MPPT target; PV3/PV4 power words must remain zero. |
| `0x0202 x 16` (`514-529`) | Live verified, production | Battery/grid/load/PV energy counters. |
| `0x021C x 2` (`540-541`) | Bracket live/cloud verified, production | DC and AC temperatures; both rows are documented read-only. |
| `0x0227 x 2` (`551-552`) | Two-map/schema verified, production narrow raw-status contract; first target read pending | Register 551 low-nibble power switch code and register 552 full raw AC relay mask. |
| `0x0229 x 6` (`553-558`) | Live zero verified, production raw-only | Warning words 1-2 and fault words 1-4; exact U16 words only, no per-bit interpretation. |
| `0x0014 x 3` (`20-22`) | 12 kW target live verified; JKS-6/8/10/12/15/20H-EI line specification-gated | Rated power must be one of 6/8/10/12/15/20 kW with exactly 2 MPPT, 3 phases, and raw22 `0x0203`; the unvalidated 25 kW two-MPPT sibling and the separate 29.9/30 kW BM3 three-MPPT variants are rejected. |
| `0x0085 x 1` (`133`) | Live zero verified, production zero-domain gate | Mutable generator-port mode/configuration word; must remain exactly zero and is re-read on every fetch. |
| `0x0218 x 4` (`536-539`) | Live zero verified, production zero-domain only | Emits generator daily runtime, daily energy, and total energy only as raw-backed zeros; any nonzero word rejects Modbus. |
| `0x0295 x 11` (`661-671`) | Live zero verified, production zero-domain only | Emits generator phase voltage/power and total-power canonical keys only as raw-backed zeros; any nonzero word rejects Modbus. |
| `0x024E x 2` (`590-591`) | Live verified, production | Battery power and Battery-1 current. |
| `0x0261 x 11` (`609-619`) | Live/cloud verified; frequency and internal currents production, registers 613-619 excluded | `PG_F1` and `G_C_L1..3` are promoted; external CT currents and incomplete power low words remain ignored. |
| `0x026E x 4` (`622-625`) | Live verified, production with `687-690` | Selected grid phase/total power low words. |
| `0x02AF x 4` (`687-690`) | Live verified, production with `622-625` | Selected grid phase/total power high words; signed import/export confirmed. |
| `0x0273 x 12` (`627-638`) | Scalars live verified; active power conservative signed production with `691-694` | Output voltage/current/frequency and gated active-power low words are promoted; apparent candidate 637 remains ignored. |
| `0x02B3 x 5` (`691-695`) | Positive pairing live correlated; `R691=0xFFFF` later observed; conservative signed production | Active highs 691-694 must be `0x0000` or `0xFFFF` and pair with 633-636 inside the signed coherence envelope; apparent candidate 695 remains ignored. |
| `0x0280 x 7` (`640-646`) | Load-voltage scalars live/cloud verified, production; power fields excluded | Only load-side L1/L2/L3 voltage at 644-646 is promoted. |
| `0x028A x 4` (`650-653`) | Positive-domain live/cloud verified, production with `656-659` | Low phase/total direct-load words emit canonical `LPP_A/B/C` and dedicated total `E_Puse_t1`. |
| `0x028F x 5` (`655-659`) | Load frequency and direct-load high-word pairing production | Register 655 emits frequency; every phase/total high word 656-659 must be zero. |

## Stage 1: Registers 500-505

The following definitions agree across the inspected Deye three-phase
protocol maps. Every register is marked `R` in the FC03 real-time area.

| Register | Documented field | Raw interpretation before live validation |
| --- | --- | --- |
| `500` | Run state | U16 enum: 0 standby, 1 self-check, 2 normal, 3 alarm, 4 fault, 5 activating. |
| `501` | Grid-side active generation today | Signed 16-bit range, `0.1 kWh` per count. |
| `502` | Grid-side reactive generation today | Signed 16-bit range, `0.1 kVArh` per count. |
| `503` | Grid connection time today | U16 seconds. |
| `504` | Grid-side total active generation, low word | Low 16 bits. |
| `505` | Grid-side total active generation, high word | High 16 bits. |

For registers 504-505, the documented low-word-first reconstruction is:

```text
raw = (uint32(register_505) << 16) | uint32(register_504)
kWh = raw * 0.1
```

Stage 1 deliberately reports the six raw words first. Register 501 and the
504-505 pair must not be labelled as PV yield until their live values are
compared: the same map defines separate PV counters and separate grid
buy/sell counters elsewhere.

### Live result: 2026-08-15

Exactly one reviewed FC03 request returned a valid 44-byte Solarman V5 frame
with a valid Modbus CRC and these raw words:

```text
500=0x0002  501=0x0000  502=0x0000
503=0x0000  504=0x0000  505=0x0000
```

Register 500 is consistent with the documented `Normal` state. The energy
fields are not promoted: the stored Jinko cloud fixture contains non-zero
`Etdy_ge1` and `Et_ge0` values while registers 501 and 504-505 returned zero.
This OEM therefore cannot be assumed to expose the cloud counters at the
standard Deye addresses. At this Stage 1 checkpoint no field was added to
production polling. Stage 13 later promotes only the independently documented
register-500 run-state enum while preserving the exclusion of registers
501-505.

References:

- [Deye Modbus RTU V105.1, printed pages 28-29](https://github.com/user-attachments/files/16798469/MODBUS.RTU.V105.1-20231006.pdf)
- [Deye three-phase HV protocol V104-2, printed page 28](https://hybrydaplus.pl/wp-content/uploads/2024/09/MODBUS.RTU_3PLV_HV_Protocol_V104-2.pdf)
- [Later Deye three-phase protocol map, printed pages 27-28](https://tpenergy.com.vn/wp-content/uploads/2025/10/Triple-Phase-Inverter-Modbus-RTU-Protocol.pdf)

## Stage 2A: Registers 587-588

The next candidate deliberately reads only two adjacent registers. Both are
marked `R` in the documented FC03 real-time area; no configuration or control
register is included.

| Register | Documented field | Candidate interpretation |
| --- | --- | --- |
| `587` (`0x024B`) | Battery-1 voltage | For the already verified HV device type `0x0006`: U16, `raw * 0.1 V`. |
| `588` (`0x024C`) | Battery-1 state of charge | U16, `raw * 1%`, documented range `0-100`. |

The reviewed inner request is fixed to unit `1`, FC03, start `587`, quantity
`2`. It does not include register 586 (temperature-offset ambiguity), register
589 (Battery-2 SOC), or signed power/current registers 590-591. The isolated
one-shot tool reports raw words only and stops on the first timeout, exception,
or validation error without reconnecting or retrying.

### Live result: 2026-08-15

Exactly one reviewed FC03 request returned a valid 36-byte Solarman V5 frame.
The V5 length, checksum, control, sequence, logger identity, type and status all
validated; the nested Modbus response also had the expected unit, FC03 byte
count and CRC. No retry or second request was made.

```text
587=0x1781 (6017)  588=0x0064 (100)
```

For the already verified HV device type `0x0006`, the documented candidates are
`601.7 V` and `100% SOC`. The old Jinko cloud fixture contains `614.00 V` and
`100%`; the voltage difference is plausible at different collection times and
strongly supports the HV scale over the LV alternative (`60.17 V`). A later
public read-only cloud snapshot reported `604.2 V` and `100%`. The voltage
differs by only `2.5 V` (about `0.4%`) despite collection lag, while SOC matches
exactly. This closes the target-specific scale and semantic correlation gate.

References:

- [Deye SG01-HP3-AM2 protocol V104.3.1, printed pages 4 and 32](https://forum.iobroker.net/assets/uploads/files/1706097675773-modbusrtu%E4%B8%89%E7%9B%B8%E9%AB%98%E5%8E%8B%E5%82%A8%E8%83%BD%E9%80%9A%E4%BF%A1%E8%A7%84%E7%BA%A6v104-%E9%AB%98%E5%8E%8B-3-1-1111_sg01-hp3-am2.pdf)
- [Deye Modbus RTU V105.1, printed pages 4 and 32-33](https://github.com/user-attachments/files/16798469/MODBUS.RTU.V105.1-20231006.pdf)
- [Maintained `deye_p3.yaml` implementation profile](https://github.com/davidrapan/ha-solarman/blob/main/custom_components/solarman/inverter_definitions/deye_p3.yaml)

## Stage 4: Energy Registers 514-529

All sixteen registers are documented `R` values in the P3 FC03 real-time
area. Daily counters are U16 values; lifetime counters are U32 pairs with the
low word first. Every count represents `0.1 kWh`.

### Live result: 2026-08-15

Exactly one reviewed FC03 request returned a valid 64-byte Solarman V5 frame.
The V5 identity, control, sequence, length and checksum passed, as did the
nested unit, FC03 byte count and Modbus CRC. No retry or second request was
made.

| Registers | Canonical metric | Decoded value |
| --- | --- | ---: |
| `514` | `Etdy_cg1` | `1.2 kWh` |
| `515` | `Etdy_dcg1` | `0.8 kWh` |
| `516-517` | `t_cg_n1` | `1038.7 kWh` |
| `518-519` | `t_dcg_n1` | `1059.4 kWh` |
| `520` | `E_B_D` | `3.5 kWh` |
| `521` | `E_S_D` | `57.4 kWh` |
| `522-523` | `E_B_TO` | `2504.3 kWh` |
| `524-525` | `E_S_TO` | `16770.4 kWh` |
| `526` | `Etdy_use1` | `6.2 kWh` |
| `527-528` | `E_C_T` | `2769.7 kWh` |
| `529` | daily PV (`PV_D_P_G` / `Etdy_ge1`) | `67.1 kWh` |

The non-zero high word at register 525 makes the documented low-word-first
order unambiguous on this target. All five lifetime totals exceed the older
2026-04-02 Jinko cloud fixture, while the daily PV and grid-sell values are
mutually plausible. A fresh public cloud snapshot then matched most counters
exactly and differed by only one `0.1 kWh` count for counters that advanced
between reads: local/cloud total grid buy was `2504.3/2504.4`, total load was
`2769.7/2769.8`, and battery total charge/discharge matched exactly at
`1038.7/1059.4`. This closes the target-specific address, scale and word-order
correlation gate.

Bytes 21-24 of this V5 response decode like an old timestamp even though all
lifetime counters are newer than the historical fixture. They are therefore
treated as stale or opaque logger metadata and are not used as `CollectedAt`;
the connector keeps the local response-receipt time.

## Stage 5: Capability Registers 20-22

The Deye P3 inherent-property area `0-59` is read-only and uses FC03. Decimal
registers `20-22` are distinct from the R/W RTC registers at decimal `62-64`:

| Registers | Meaning | Decode |
| --- | --- | --- |
| `20-21` | Rated power, low word first | `((high << 16) | low) * 0.1 W` |
| `22` | MPPT and phase count | MPPT=`(raw >> 8) & 0x0F`; phases=`raw & 0x0F` |

One reviewed request returned a valid 38-byte V5/FC03 response without retry:

```text
20=0xD4C0  21=0x0001  22=0x0203
```

The result is exactly `12000.0 W`, `2 MPPT`, and `3 phases`. Rated power matches
the fresh cloud `Pr1=12000 W`, while the phase count matches the already
verified three-phase HV type. This is the live target fingerprint and also
establishes that zero PV3/PV4 words do not represent physical MPPTs on this
inverter.

The official JKS-6/8/10/12/15/20H-EI line lists 2-MPPT, three-phase models at
ratings `6000/8000/10000/12000/15000/20000 W`. Production accepts exactly
those six rated-power values together with device type `0x0006` and raw22
`0x0203`. Only the 12 kW target has live-confirmed raw gate values and telemetry;
the other five ratings are admitted from the official line specification. For
those five ratings, requiring type `0x0006` and raw22 `0x0203` is a conservative
reuse of the 12 kW live fingerprint, not a claim that their raw values have
been observed or are stated by the datasheet. The unvalidated 25 kW two-MPPT
sibling is deliberately outside `jks-6-20h-ei-readonly-v1`. The structurally
separate 29.9/30 kW BM3 three-MPPT variants also remain fail-closed and require
another profile. Published revisions/table layouts show model-dependent 40 A
or 80 A pass-through, but do not support a safe per-model split here;
pass-through is therefore not inferred from `Pr1`.

Family-scope references:

- [Jinko JKS-6-20H-EI-A7-2 datasheet](https://jinkosolar.eu/wp-content/uploads/2024/11/JKS-6-20H-EI-A7-2.pdf)
- [Deye SUN-(5-25)K-SG01HP3-EU datasheet](https://www.deyeinverter.com/deyeinverter/2023/06/27/datasheet_sun-%285-25%29k-sg01hp3-eu_230626_en.pdf)
- [Deye SUN-29.9-50K-SG01HP3-EU-BM3/BM4 separate-family datasheet](https://www.deyeinverter.com/deyeinverter/2024/07/16/sun-29.9-50k-sg01hp3-eu-bm4-1.pdf)

## Stage 6: Battery Flow Registers 590-591

Both registers are documented `R` in the FC03 real-time area. Register 590 is
signed Battery output power; verified HV type `0x0006` uses `10 W/count`.
Register 591 is signed Battery-1 current at `0.01 A/count`. The primary table
does not define direction labels, so the one-shot initially reported raw words
only.

One reviewed request returned a valid 36-byte V5/FC03 response without retry:

```text
590=0x0009  591=0x0010
```

This decodes to `+90 W` and `+0.16 A`. The immediately preceding public cloud
snapshot reported `+100 W`, `+0.17 A`, and `603.1 V`: the values have identical
signs and differ by exactly one native quantum. The physical cross-check gives
`603.1 V * 0.16 A = 96.5 W`, only `6.5 W` from the Modbus power value. This
strongly validates address, signed encoding, HV scale, magnitude and the
Modbus-to-cloud sign orientation. Naming positive as discharge remains based
on the maintained Deye profile because the primary table does not spell out
the direction convention.

## Stage 7: Grid Registers 609-619

This contiguous block is entirely documented `R` in the FC03 real-time area:
609 is grid frequency; 610-612 and 613-615 are internal and external CT
currents; 616-619 are the low words of external CT phase/total active power.
The complete power values require high words 705-708 and are therefore not yet
eligible for production decoding.

One reviewed request returned a valid 54-byte V5/FC03 response without retry:

```text
frequency:          4995
internal currents:  53, 66, 155
external currents:  53, 63, 157
external powers:    100, 102, 120, 322
```

The maintained-profile frequency scale gives `49.95 Hz`, versus fresh cloud
`50.00 Hz`. Internal currents decode to `0.53/0.66/1.55 A`, versus cloud
`0.53/0.65/1.54 A`; the external triplet is `0.53/0.63/1.57 A`. This sample
strongly favors internal registers 610-612 for canonical `G_C_L1..3`.
External CT low-word powers were `100/102/120/322 W`, versus fresh cloud
`102/101/121/324 W`, and the three local phases sum exactly to the local total.
These checks validate frequency/current scales and low-word power addresses,
but the signed power metrics remain blocked until high words 705-708 are read.

## Stage 8: Selected Grid Power Low Words 622-625

Registers 622-625 are documented `R` values in the FC03 real-time area. They
are the low words of selected grid phase A/B/C and total active power; their
corresponding high words are registers 687-690. The one-shot therefore
reported raw U16 words only and did not infer signedness or import/export
direction.

One reviewed request returned a valid 40-byte V5/FC03 response without retry:

```text
622=0x0065 (101)  623=0x0062 (98)
624=0x0075 (117)  625=0x013C (316)
```

The same-frame phase sum is exact: `101 + 98 + 117 = 316`. The immediately
preceding cloud snapshot reported `99/101/118 W` and total `318 W`; the
per-phase differences are only `+2/-3/-1 W`, with a total difference of
`-2 W`. This strongly validates the addresses, phase/total structure and
`1 W/count` low-word scale despite the non-atomic collection times.

The separately reviewed high-word request then returned a second valid
40-byte V5/FC03 response without retry:

```text
687=0x0000  688=0x0000  689=0x0000  690=0x0000
```

Joining the two reviewed blocks low-word first produces
`101/98/117/316 W`. High-word-first joining would produce implausible
multi-megawatt values, so the word order and positive quadrant are strongly
validated. Fresh cloud values were `99/102/117/318 W`; phase sums were exact
in both sources. Cloud also satisfied `grid 318 W + inverter output -5 W =
load 313 W`, establishing that positive grid power on this target is import.

On 2026-08-16, natural export provided the missing negative-quadrant check.
Without changing inverter state, the same two reviewed reads returned:

```text
low:  FCF7 / FCF4 / FCFA / F6E5
high: FFFF / FFFF / FFFF / FFFF
```

Low-first signed 32-bit joining gives `-777/-780/-774 W`, total `-2331 W`;
the three phases sum exactly to the total. The nearby cloud snapshot was
`-704/-704/-702 W`, total `-2110 W`, also with an exact phase sum. The uniform
difference is consistent with cloud lag while morning export was increasing.
Together with the prior positive/import sample, this confirms two's-complement
sign extension, low-first pairing, `1 W/count`, positive import and negative
export. Registers 622-625 plus 687-690 are production-promoted as one validated
logical value set read through two fixed FC03 blocks.

The production decoder joins the complete low-first 32-bit word before
interpreting the sign. It accepts only family-envelope high words `0x0000` or
`0xFFFF`, but deliberately does not use low-word bit 15 as the sign: synthetic
`+40000 W` and `-40000 W` fixtures cover both sides of that boundary. Each
phase and total is constrained to the JKS-family envelope `±65535 W`, followed
by an exact signed phase-sum check.

## Stage 9: Output Registers 627-638 and 691-695

Both ranges are documented `R` values in the FC03 real-time area. Registers
627-629 are output phase voltages, 630-632 phase currents, 633-637 power low
words, and 638 output frequency. Registers 691-695 are the corresponding
power high words. The two ranges were read separately to avoid unrelated
registers between them.

The first night-time sample confirmed mixed signed scalar values and low-word
mapping. A daytime pair then returned:

```text
voltage: 238.9 / 240.9 / 236.2 V
current: +3.80 / +3.80 / +4.60 A
frequency: 49.95 Hz
active low words: 879 / 883 / 1092 / 2854
apparent candidate low word: 2854
high words 691-695: 0000 / 0000 / 0000 / 0000 / 0000
```

The active phase sum is exactly `2854 W`. The nearby cloud snapshot was
`795/836/1004 W`, total `2635 W`; all values rose coherently while generation
was ramping. Voltages, currents and frequency also closely matched cloud, and
same-frame `V*I` values were physically consistent with the power magnitude.
Therefore output voltages 627-629 (`0.1 V`), signed currents 630-632
(`0.01 A`) and frequency 638 (`0.01 Hz`) are production-promoted through the
fixed `627 x 12` read. Active powers 633-636 are production-promoted with the
immediately following fixed `691 x 5` read in a conservative signed domain.
Each low/high pair is joined low-word-first and interpreted as two's-complement
signed 32-bit. Every active high must be the canonical sign extension
`0x0000` or `0xFFFF`, every joined value must remain in
`-0x7FFF..0x7FFF W`, and the exact signed sum of the three phases must equal the
dedicated total. These gates emit `INV_O_P_L1..3` and `INV_O_P_T` atomically or
reject the entire Modbus snapshot.

The symmetric `0x7FFF` boundary is deliberately narrower than the wire type.
For example, low words encoding `-100/+20/+30/-50 W` are
`65436/20/30/65486`; if a later high-word read had already changed to all zero,
the unsigned phase sum would still match and could publish roughly 65 kW.
Joining that torn pair first yields values outside `-32767..32767 W`, so the envelope
rejects it. The inverse positive-to-negative tear is rejected the same way.
Inside this envelope the high word carries only sign extension, so an unchanged
sign cannot tear the magnitude across the two reads. This is an encoding
coherence guard, not an inverter-rated-power or pass-through cap.

A later production log observed `R691=0xFFFF`, confirming that the target
enters the negative sign-extension domain. It did not retain registers 633-636
or the other high words, so this evidence does not establish an exact negative
magnitude or a simultaneous Modbus/cloud correlation. Register 637+695 joins
to `2.854 kVA` in the earlier positive sample if divided by 1000 and is close
to cloud `2.64 kVA`, but it equals active power exactly at near-unity power
factor. Because the primary unit wording is ambiguous and the maintained
profile omits this pair, both words remain ignored and `O_P` remains excluded
until a materially non-unity-PF sample distinguishes apparent power from an
alias.

## Stage 2B: Register 586

Register `586` (`0x024A`) is documented as Battery-1 temperature, `R`, one
16-bit value in the FC03 real-time area. The inspected P3 tables specify a raw
range of `0-3000` and a `0.1 °C` resolution, but do not explicitly document an
offset or signed encoding.

The maintained `deye_p3.yaml` profile treats the value as unsigned, subtracts
an offset of 1000, and then applies the 0.1 scale:

```text
temperature_C = (uint16(raw) - 1000) * 0.1
```

Before the live read below, that offset was only an implementation-backed
candidate for this Jinko OEM. The stored historical `B_T1=13.00 °C` would
correspond to `raw=1130` with the offset model or `raw=130` without it, making a
single raw read strongly discriminating. A fresh, closely timed cloud/display
comparison was therefore the required production-promotion gate.

The isolated one-shot is fixed to unit `1`, FC03, start `586`, quantity `1`.
It reports only the raw word and stops on the first timeout, exception, framing
error, checksum failure or CRC failure.

### Live result: 2026-08-15

Exactly one reviewed FC03 request returned a valid 34-byte Solarman V5 frame.
The outer length, checksum, control, sequence, identity, type and status all
validated, as did the nested Modbus unit, FC03 byte count and CRC. No retry or
second request was made.

```text
586=0x0500 (1280)
```

The maintained-profile candidate decodes this as `(1280 - 1000) * 0.1 =
28.0 °C`. A literal no-offset interpretation would produce `128.0 °C`, which
is physically implausible for normally operating battery equipment and exceeds
the maintained profile's validation maximum. This strongly validates the
`+1000` wire offset on this target. The historical Jinko `B_T1=13.00 °C`
fixture is from a different season and collection time. A later public
read-only cloud snapshot reported both `B_T1=28 °C` and `BMST=28 °C`, exactly
matching the offset decode. This closes the target-specific temperature
correlation gate; no additional Modbus read is required solely to establish
the offset.

References:

- [Deye SG01-HP3-AM2 protocol V104.3.1, printed page 32](https://forum.iobroker.net/assets/uploads/files/1706097675773-modbusrtu%E4%B8%89%E7%9B%B8%E9%AB%98%E5%8E%8B%E5%82%A8%E8%83%BD%E9%80%9A%E4%BF%A1%E8%A7%84%E7%BA%A6v104-%E9%AB%98%E5%8E%8B-3-1-1111_sg01-hp3-am2.pdf)
- [Deye Modbus RTU V105.1, printed page 32](https://github.com/user-attachments/files/16798469/MODBUS.RTU.V105.1-20231006.pdf)
- [Maintained `deye_p3.yaml` implementation profile](https://github.com/davidrapan/ha-solarman/blob/main/custom_components/solarman/inverter_definitions/deye_p3.yaml)
- [Current parser offset-before-scale implementation](https://github.com/davidrapan/ha-solarman/blob/main/custom_components/solarman/parser.py)

## Stage 3: PV Registers 672-679

All eight registers are documented `R` values in the FC03 real-time area. For
the verified HV type `0x0006`, registers 672-675 use `10 W/count`; registers
676/678 use `0.1 V/count`; registers 677/679 use `0.1 A/count`. They represent
DC PV inputs/MPPT channels, not AC phases.

### Live result: 2026-08-15

Exactly one reviewed FC03 request returned a valid 48-byte Solarman V5 frame.
The outer length, checksum, control, sequence, identity, type and status all
validated, as did the nested unit, FC03 byte count and CRC. No retry or second
request was made.

```text
672=0x0003  673=0x0004  674=0x0000  675=0x0000
676=0x04F4  677=0x0002  678=0x0514  679=0x0003
```

Candidate decode at approximately 20:45:25 Europe/Kyiv:

```text
PV1: 30 W, 126.8 V, 0.2 A   (V*I = 25.36 W)
PV2: 40 W, 130.0 V, 0.3 A   (V*I = 39.00 W)
PV3: 0 W
PV4: 0 W
```

The same-frame `V*I` results closely match the independently reported power at
the documented `0.1 A` and `10 W` resolutions, strongly validating the address
map and scales during weak generation. A later separately reviewed exact read
again returned zero in registers 674-675 and its two-channel sum correlated
with fresh cloud aggregate solar power. The compatible read-only protocol maps
and maintained implementation profile establish the register meanings and
scales. The [stored Jinko detail fixture](../testdata/jinko_detail_response.json)
independently carries the `DP1`/`DP2`, `DV1`/`DV2`, and `DC1`/`DC2`
storage-name, display-name, and unit tuples; it supplies label provenance, not
a claim that its values were sampled in the same Modbus frame. The
[shared canonical dictionary](../internal/source/jinko/schema.go) applies those
same key/group/name/unit identities to Jinko, local Modbus, and every recognized
Solarman point:

| Register or expression | Decode | Canonical metric |
| --- | --- | --- |
| `672` | `U16 × 10 W` | `electric/DP1`, `DC Power PV1`, `W` |
| `673` | `U16 × 10 W` | `electric/DP2`, `DC Power PV2`, `W` |
| `676` | `U16 × 0.1 V` | `electric/DV1`, `DC Voltage PV1`, `V` |
| `677` | `U16 × 0.1 A` | `electric/DC1`, `DC Current PV1`, `A` |
| `678` | `U16 × 0.1 V` | `electric/DV2`, `DC Voltage PV2`, `V` |
| `679` | `U16 × 0.1 A` | `electric/DC2`, `DC Current PV2`, `A` |
| `(672 + 673) × 10 W` | validated two-channel sum | `electric/S_P_T`, `Total Solar Power`, `W` |

### Production promotion: 2026-08-16

Together with the independently gated `2 MPPT` capability, production emits
all seven metrics from this one already-approved response. Registers 674 and
675 remain the PV3/PV4 power words and must both equal zero; either becoming
non-zero rejects the whole snapshot. The aggregate remains capped at twice the
current profile-gated per-model AC rating, and the same limit is applied to
each channel independently. PV voltage is bounded to `1000 V` and PV current
to `100 A`. No extra FC03 block was added by this stage, so the first/later
fetch plans remained 19/17 requests at that checkpoint. Promoting the six per-channel
metrics increased the locked snapshot at that stage from 52 to 58 metrics.

References:

- [Deye SG01-HP3-AM2 protocol V104.3.1](https://forum.iobroker.net/assets/uploads/files/1706097675773-modbusrtu%E4%B8%89%E7%9B%B8%E9%AB%98%E5%8E%8B%E5%82%A8%E8%83%BD%E9%80%9A%E4%BF%A1%E8%A7%84%E7%BA%A6v104-%E9%AB%98%E5%8E%8B-3-1-1111_sg01-hp3-am2.pdf)
- [Deye Modbus RTU V105.1, printed page 37](https://github.com/user-attachments/files/16798469/MODBUS.RTU.V105.1-20231006.pdf)
- [Maintained `deye_p3.yaml` implementation profile](https://github.com/davidrapan/ha-solarman/blob/main/custom_components/solarman/inverter_definitions/deye_p3.yaml)

## Stage 10: Load-Side Scalars in Registers 640-646 and 655-659

The inspected compatible Deye P3 maps mark all registers in both fixed blocks
as `R` in the FC03 real-time area. Exact separately reviewed one-shot reads
validated the Solarman V5 envelope, nested FC03 response and CRC before the
selected scalars were compared with fresh cloud/display values.

- The `640 x 7` read emits only registers 644-646 as `C_V_L1`, `C_V_L2`, and
  `C_V_L3` at `0.1 V/count`. Registers 640-643 are ignored by this block's
  voltage decoder. Register 643 is also read independently by the existing
  `643 x 1` request and emitted as the narrow unsigned-U16 `UPS_P`; possible
  full phase/total pairing with high words 696-699 remains unimplemented.
- The `655 x 5` read emits only register 655 as `L_F` at `0.01 Hz/count`.
  At this Stage 10 checkpoint, registers 656-659 were retained but not yet
  paired with their direct-load power low words.

Zero volts or zero frequency is accepted because it can represent an outage or
inactive load side. The production decoder applies upper validation bounds of
`500 V` per phase and `100 Hz`, but does not impose fragile non-zero minimums
or cross-frame equality checks. No direct-load phase/total power metric was
promoted at this checkpoint. Stage 15 later adds the separately live-tested
650-653 low words and first promotes the positive-domain total; Stage 17 adds
the independently decoded canonical phases from those same responses.

## Stage 11: DC and AC Temperatures in Registers 540-541

Both compatible Deye P3 protocol maps mark registers 540 and 541 as `R` in
the FC03 real-time area. They define the DC and AC temperatures as unsigned
words with the same offset scale already verified for the battery temperature:

```text
temperature_C = (raw - 1000) / 10
```

One exact `540 x 2` read was performed between two fresh cloud snapshots. It
returned raw values `1250` and `1440`, decoding to `25.0 °C` and `44.0 °C`.
The before/local/after bracket was:

| Metric | Cloud before | Local FC03 | Cloud after |
| --- | ---: | ---: | ---: |
| `T_DC` | `25.0 °C` | `25.0 °C` | `25.0 °C` |
| `AC_T` | `43.6 °C` | `44.0 °C` | `44.1 °C` |

The unchanged DC value and smoothly bracketed AC value validate both address
order and scale without assuming simultaneous cloud collection. Production
therefore emits only canonical `T_DC` and `AC_T`. It requires exactly two
words, rejects either raw word above `3000`, applies the plausible range
`-100..100 °C`, and returns no partial snapshot on any failure. The request is
fixed FC03/read-only and adds no configuration or write-function path.

## Stage 12: Raw Warning/Fault Words in Registers 553-558

The inspected compatible Deye P3 V104.3.1 and V105.1 protocol maps place all
six contiguous registers in the read-only FC03 real-time area:

| Register | Documented field | Production representation |
| --- | --- | --- |
| `553` | Warning message word 1 | Independent raw U16. |
| `554` | Warning message word 2 | Independent raw U16. |
| `555` | Fault information word 1 | Independent raw U16. |
| `556` | Fault information word 2 | Independent raw U16. |
| `557` | Fault information word 3 | Independent raw U16. |
| `558` | Fault information word 4 | Independent raw U16. |

The exact one-shot request was fixed to unit `1`, FC03, start `553`, quantity
`6`, and Solarman V5 sequence `0x14`. It used one connection, one application
request, and no retry.

### Live result: 2026-08-16

The 44-byte V5 response length, control, sequence, type, status, checksum, and
nested Modbus unit, FC03 byte count, and CRC all validated. The six returned
words were:

```text
553=0x0000  554=0x0000  555=0x0000
556=0x0000  557=0x0000  558=0x0000
```

This all-clear sample validates the exact transport, address order, response
shape, and zero state on the target. It does not validate individual bit names
or severities, including reserved or firmware-specific bits. Production
therefore emits the six independent raw U16 values only. It does not combine
them, decode bits, or map them to the Jinko lithium-BMS alert keys. Any failure
of this warning/fault read rejects the whole snapshot without retry.

References:

- [Deye SG01-HP3-AM2 protocol V104.3.1](https://forum.iobroker.net/assets/uploads/files/1706097675773-modbusrtu%E4%B8%89%E7%9B%B8%E9%AB%98%E5%8E%8B%E5%82%A8%E8%83%BD%E9%80%9A%E4%BF%A1%E8%A7%84%E7%BA%A6v104-%E9%AB%98%E5%8E%8B-3-1-1111_sg01-hp3-am2.pdf)
- [Deye Modbus RTU V105.1](https://github.com/user-attachments/files/16798469/MODBUS.RTU.V105.1-20231006.pdf)

## Stage 13: Source-Local Run State from Register 500

Stage 1 already performed the exact live `500 x 6` FC03 read and returned
`500=0x0002`, matching the documented `Normal` state. No additional live read
was needed for this promotion. The production request is fixed to unit `1`,
FC03, start register `500` (`0x01F4`), quantity `6`, and Solarman V5 sequence
`0x04`. It is appended after the sequence-`0x14` warning/fault read; sequence
bytes are request identities and are intentionally not monotonic execution
counters.

Only register 500 is decoded:

| Code | Meaning |
| ---: | --- |
| `0` | Standby |
| `1` | Self-check |
| `2` | Normal |
| `3` | Alarm |
| `4` | Fault |
| `5` | Activating |

Codes above `5` reject the whole snapshot. Registers 501-505 are accepted as
arbitrary U16 trailing words but are neither interpreted nor emitted: their
documented energy/time meanings did not correlate on this target. Production
emits one source-local, unitless group-`status` metric named
`DEYE_MODBUS_R500_RUN_STATE`. It does not synthesize the Jinko `ST_PG1` or
`INV_MOD1` aliases and the numeric status metric has no automatic alert
semantics.

The block is one exact request in the fixed sequential stream on the fetch's
single connection and shared absolute deadline. It has no retry or write path,
and any transport, protocol, exception, length, checksum, CRC, or enum failure
aborts the fetch, closes the connection without reconnecting, and returns no
partial snapshot.

## Stage 14: Grid Frequency and Internal Currents from Registers 609-612

Stage 7 already performed the exact live `609 x 11` FC03 read and correlated
the returned frequency and both current triplets with fresh cloud data. No new
live read was needed for this promotion. The production request is fixed to
unit `1`, FC03, start register `609` (`0x0261`), quantity `11`, and Solarman V5
sequence `0x0B`. It is appended after the sequence-`0x04` run-state read;
sequence bytes are fixed request identities rather than execution counters.

Production emits only these fields:

| Register | Canonical metric | Decode and guard |
| ---: | --- | --- |
| `609` | `PG_F1` | unsigned raw divided by 100; `0..100 Hz` |
| `610` | `G_C_L1` | signed 16-bit raw divided by 100; absolute value at most `100 A` |
| `611` | `G_C_L2` | signed 16-bit raw divided by 100; absolute value at most `100 A` |
| `612` | `G_C_L3` | signed 16-bit raw divided by 100; absolute value at most `100 A` |

The primary maps establish the read-only FC03 address and field identity. The
frequency divisor is sourced from the maintained Deye P3 definition and is
additionally supported by the reviewed `4995` raw versus approximately
`50.00 Hz` cloud sample; it is not claimed as an independently printed scale
in both primary tables. The internal-current triplet decoded as
`0.53/0.66/1.55 A`, closely bracketing the cloud `0.53/0.65/1.54 A` sample.

Registers 613-619 are accepted as arbitrary U16 trailing words but are never
decoded or emitted. In particular, registers 616-619 are only external-CT
power low words; their complete signed values would require high words
705-708. Mutating any ignored word cannot affect the four promoted metrics.
The block remains one exact request in the fixed sequential stream on the
fetch's single connection and shared absolute deadline, with no retry or write
path. Any transport, protocol, exception, length, checksum, CRC,
frequency-range, or current-range failure aborts the fetch, closes the
connection without reconnecting, and returns no partial snapshot. At this
Stage 14 checkpoint the first fetch performed 18 fixed requests on one
connection and a later fetch performed 16 on a fresh connection; two successful
fetches therefore used two connections and 34 requests after the two profile
gates were cached by their first joint success. Stage 15 below adds one earlier
fixed telemetry read and updates the current counts.

## Stage 15: Positive-Domain Direct-Load Total from Registers 650-653 and 656-659

An exact read-only one-shot used unit `1`, FC03, start register `650`
(`0x028A`), quantity `4`, and fixed Solarman V5 sequence `0x12`. It returned
the low words `19/34/248/301 W`. The immediately following already validated
`655 x 5`, sequence-`0x13`, response returned all four high words 656-659 as
`0x0000`. The local phase words therefore summed exactly:

```text
19 + 34 + 248 = 301 W
```

The immediate cloud bracket was `LPP_A/B/C=18/33/249 W`, whose sum was
`300 W`, and canonical `E_Puse_t1=300 W`. The local total differed by only
`1 W`. This supports the direct-load address, pair ordering, and
positive-domain magnitude without assuming perfectly atomic cloud collection.
A later live same-frame counterexample described below proves that the exact
phase/total equality in this sample was incidental rather than universal.

Production places the fixed sequence-`0x12` read immediately before the
sequence-`0x13` read and emits canonical phases `LPP_A/B/C` plus dedicated
total `E_Puse_t1`. The decoder
requires exactly four low words and exactly five frequency/high words, then
applies all of these fail-closed gates:

- every phase/total high word 656-659 must equal `0x0000`;
- all four low-first pairs are decoded independently, with no phase-sum gate;
- zero and the full U16 boundary `65535 W` are accepted for every pair.

This first promotion is deliberately limited to the verified non-negative
domain. Any nonzero high word, including `0xFFFF` negative sign extension, is
rejected until a negative value is independently validated. There is no
`Pr1`-derived cap: official model-dependent pass-through is a separate limit,
and the 40 A/80 A revision/table-layout ambiguity is not safe to encode as a
per-model rule here. Alternative phase aliases `C_P_L1..3` and all
inverter-output power aliases remain excluded.

The combined production stage opens one fresh short-lived connection per fetch
and sends one exact request per fixed block sequentially under one shared
absolute deadline, with no retry or write path. A transport, exception, count,
checksum, CRC, or direct-load high-word failure aborts the fetch, closes the connection
without reconnecting, and returns no partial snapshot. At this historical
checkpoint, the first fetch sent 19 requests on one connection and each later
fetch sent 17 on a fresh connection after the profile gates were cached. Two
successful fetches therefore used two connections and 36 requests.

## Stage 16: One-Connection Transport and Direct-Load Invariant Correction

A controlled production `fetch` opened one fresh TCP connection and
successfully exchanged the two profile gates plus UPS, load-voltage,
direct-load-low, and load-frequency/high responses sequentially on that same
socket. The sixth response completed with valid V5 and Modbus integrity, which
demonstrates that the logger accepts multiple strict request/response cycles on
one connection without pipelining.

The fetch then stopped fail-closed during decode: the three direct-load phase
low words summed to `243 W`, while dedicated total register 653 was `251 W`.
No retry, reconnect, cloud fallback, partial snapshot, or lingering TCP session
occurred. Because all four low words were returned atomically in the same FC03
response, timing between the low/high reads cannot explain the mismatch. This
is direct target evidence that `650+651+652=653` is not a valid production
invariant.

Production treats 653/659 as the independent canonical `E_Puse_t1` pair and
does not compare it with the phase sum. The phase pairs 650/656, 651/657, and
652/658 are independently exposed as `LPP_A/B/C`. All four high words must
remain zero for the verified non-negative domain. Regression tests retain a
sanitized `243 W` versus `251 W` mismatch and prove that arbitrary phase/total
mismatch is accepted without changing the dedicated total.

After that correction, one separately authorized Modbus-only verification
fetch completed the entire locked first-fetch plan: one fresh TCP connection,
all 19 sequential request/response exchanges, and the then-current 52-metric
snapshot. This is the historical pre-string-metric live result. Six values
from the same already-captured `672 x 8` response first grew production to 58
metrics; the three phase values already captured by the direct-load reads and
the second canonical view of register-588 SOC brought the Stage 17 contract to
62 metrics, without adding a request. The later generator zero-domain stage
raised that checkpoint to 73 metrics, and the staged output-active-power
promotion raised the Stage 19 contract to 77 metrics. The
process exited successfully only after the session's exact-plan completion
check. A redacting verification wrapper confirmed the `modbus` source, locked
profile/function/device-type metadata, that historical unique 52-key surface,
finite values, and the grid, daily-PV, run-state, frequency, and PV-bound
invariants. Cloud fallback and retry were disabled, and an immediate post-run
check found no lingering target socket.

The wrapper initially labelled its own post-processing as failed only because
PowerShell had already materialized `collected_at` as a `DateTime` and the
wrapper attempted to parse that value a second time as a locale-formatted
string. All snapshot-content checks had already passed, the production process
had exited with code zero, and an immediate UTC comparison placed the captured
time within 24 seconds. No additional device request was made for that harness
issue.

## Stage 17: Canonical Direct-Load Phase and BMS SOC Surface

The Stage 15 live frame already contained all four low-first direct-load pairs,
and its immediate cloud bracket independently reported `LPP_A/B/C` with the
same phase order and magnitude. Production therefore exposes pairs 650/656,
651/657, and 652/658 as the shared canonical `consumption/LPP_A`, `LPP_B`, and
`LPP_C` metrics. It retains the Stage 16 correction: the phase sum need not
equal dedicated total 653/659. Each of the four pairs is limited to the exact
live-verified non-negative domain by requiring its high word to be zero; a
nonzero word rejects the complete snapshot. Offline tests cover zero, the
`65535 W` boundary, low-word sign-bit values, arbitrary phase/total mismatch,
every high-word rejection, and the canonical group/name/unit tuples.

Register 588 was already decoded and range-checked as SOC `0..100%`. The cloud
contract exposes that same stored value through both `battery/B_left_cap1`
(`SoC`) and `bms/BMS_SOC` (`BMS_SOC`), so Modbus now emits both canonical keys
with one identical register-derived value. This is an alias of observed data,
not a synthetic zero. These four additional metrics reuse existing responses:
at this checkpoint the read order remained 19 requests on the first connection
and 17 on later connections, and the locked core surface was 62 metrics.

## Stage 18: Generator Zero-Domain Surface

The isolated generator probe ran while the bridge was stopped. It opened one
TCP connection and sent exactly three unit-1 FC03 requests sequentially, with
one response before the next request and no retry or reconnect:

1. register `133 x 1` (`0x0085`), sequence `0x16`;
2. registers `536 x 4` (`0x0218-0x021B`), sequence `0x17`;
3. registers `661 x 11` (`0x0295-0x029F`), sequence `0x18`.

All V5 framing, sequence, logger identity, lengths, checksums, Modbus headers,
byte counts, and CRCs passed. The raw result was `R133=0` and every word from
536-539 and 661-671 equal to zero. These local values are consistent with
previously observed Jinko generator zeros, but they were not freshly bracketed
because the regular bridge was stopped for exclusive logger access.

Production consequently implements an exact zero-domain contract, not a full
generator decoder. Register 133 is mutable and is re-read before both generator
telemetry blocks on every fetch. All sixteen raw words must equal zero. Only
after that gate passes does Modbus emit the eleven shared canonical metrics
`GEN_P_L1..3`, `GEN_V_L1..3`, `R_T_D`, `EG_P_CT1`, `GEN_P_T`, `GEN_P_D`, and
`GEN_P_TO`, each with value zero and the Jinko dictionary's group/name/unit.
`EG_P_CT1` is a compatibility view of the same observed zero total as
`GEN_P_T`; no nonzero equivalence is claimed. Any nonzero word aborts the
entire snapshot so configured Jinko/Solarman fallback can run. No generator
scale, sign, low/high pairing, phase-sum invariant, or nonzero alias is encoded.

At the Stage 18 checkpoint, the immutable production plan placed these three reads immediately after the
two cached physical profile gates and before the previous seventeen telemetry
reads. The first fetch therefore uses one connection and 22 sequential
requests; later fetches use one fresh connection and 20 requests because only
the two physical gates are cached. The Modbus surface is 73 unique metrics.
Golden-frame/hash tests cover sequences `0x16-0x18`; decoder tests mutate every
generator word away from zero; Stage 18 tests covered exact order, 22/20 counts,
canonical labels, and fail-closed no-partial-snapshot behavior.

The compatible primary maps and maintained profile establish the read-only
address/field layout used to choose these three blocks. They do not widen the
production value domain: their documented scales and low/high pairings remain
unimplemented until a nonzero target capture is independently correlated.

References:

- [Deye three-phase Modbus address list](https://badenergy.dk/wp-content/uploads/2025/01/Deye_3p_modbus_address_list.pdf)
- [Deye Modbus RTU V105.1](https://github.com/user-attachments/files/16798469/MODBUS.RTU.V105.1-20231006.pdf)
- [Maintained `deye_p3.yaml` implementation profile](https://github.com/davidrapan/ha-solarman/blob/main/custom_components/solarman/inverter_definitions/deye_p3.yaml)

## Stage 19: Positive-Only Output Active Power

At this historical checkpoint, the reviewed Stage 9 pair was promoted only in
the live-verified nonnegative domain. Production appended one compile-time FC03
block, registers `691 x 5` (`0x02B3-0x02B7`) at sequence `0x0F`, immediately
after `627 x 12` at sequence `0x0E`. The two requests ran back-to-back on the
same existing fetch connection with no intervening device read. Only active
pairs 633/691, 634/692, 635/693, and 636/694 were interpreted; apparent
candidate 637/695 was deliberately ignored.

Stage 19 required every active high word to equal `0x0000`, accepted each
joined phase and total only in the then-verified `0..32767 W` domain, and
required the exact phase sum to equal the dedicated total. A nonzero high word,
out-of-domain value, or sum mismatch was terminal for that Modbus fetch: no
partial snapshot, retry, or reconnect was allowed, and a configured priority
chain could continue with Jinko or Solarman. This stage did **not** claim or
accept negative output.

The four emitted canonical tuples were `electric/INV_O_P_L1/Inverter Output
Power L1/W`, the corresponding L2 and L3 tuples, and
`electric/INV_O_P_T/Total Inverter Output Power/W`. The extra read raised the
Stage 19 checkpoint to 23 requests on the first connection and 21 on later
fresh connections; that checkpoint's Modbus surface was 77 metrics. Golden
request tests locked the sequence/range and synthetic frame SHA-256
`667ee1799c7734fb9ea8f2b3a2b9f470c3a0167e3bb7ab277cfe62add6e944d3`.
Decoder and end-to-end tests covered the positive live sample, zero, upper
positive boundary and rejection, nonzero active highs, exact sum failure,
ignored 637/695 mutations, one-connection order, and fail-closed fallback.

Production later observed `R691=0xFFFF`, exposing the limit of this
positive-only gate. Stage 22 supersedes the Stage 19 value-domain contract with
canonical signed extension and a symmetric coherence guard; it does not change
the request introduced here or add apparent-power support.

## Stage 20: 2026-08-16 Production Acceptance

On 2026-08-16, one supervised Modbus-only production fetch completed the
then-current immutable plan without retry: one connection, 23 sequential requests,
23 valid responses, source `modbus`, and exactly 77 unique metrics. The
container had cloud sources, Shelly enrichment, MQTT publishing, and alerts
disabled for the acceptance run.

The staged output-active-power gate accepted phase values `85 W`, `83 W`, and
`278 W` with the dedicated total `446 W`; the phase sum exactly matched the
total. The two PV powers were `340 W` and `450 W`, with total `790 W`. All
eleven generator metrics passed the zero-domain gate and were emitted as zero.
This acceptance confirms the positive quadrant of the output contract and the
77-metric surface. It did not provide a correlated negative magnitude and does
not widen the nonzero-generator contract described above.

## Stage 21: BMS Temperature Alias and Relay Status

Register 586 was already live/read-only verified and bracketed against the
cloud in Stage 2B. The same cloud snapshot exposed identical `B_T1=28 °C` and
`BMST=28 °C` values. Production therefore emits the one decoded and
range-checked register-586 value through both canonical definitions:
`temperature/B_T1/Temperature- Battery/C` and
`bms/BMST/BMS Temperature/C`. This alias adds no Modbus request and does not
invent a value.

Two compatible Deye read-only maps define decimal registers 551-552 in the
FC03 real-time area. Register 551 is the turn-off/on status: its low nibble is
`0` for off or `1` for on. Production ignores its undefined upper bits,
rejects low-nibble codes 2-15, and emits the accepted code as the source-local
`status/DEYE_MODBUS_R551_POWER_SWITCH_STATE/Deye Inverter Power Switch State`
metric. No `ST_PG1` or `INV_MOD1` alias is synthesized because neither has the
same upstream contract.

Register 552 is the AC relay status mask. The documented meanings include bit
0 inverter relay, bit 2 grid relay, bit 3 generator relay, bit 4 grid-supply
relay, and bits 7-8 dry contacts; bit 1 is listed as reserved/undefined. The
retained Jinko fixture exposes the exact canonical `status/AC/AC side relay
status` value `7`, which includes bit 1. Production consequently preserves and
emits the complete register-552 U16 without filtering or rejecting any bit.
It does not turn the read into relay control and exposes no Modbus write path.

The immutable request is unit 1, FC03, start `551` (`0x0227`), quantity `2`,
sequence `0x19`. It runs after `514 x 16` energy and before the existing,
unchanged `553 x 6` warning/fault request. Its sanitized request SHA-256 is
`6ca3776bb36eade99c93a97ee34386f44951674e297b181ed6eaa043b989ac36`;
the synthetic `R551=1, R552=7` response fixture SHA-256 is
`509c179e86d3fb18fd6cd2073097e193492bee72267704bf81ef46d3e8101c89`,
and its FC03-exception fixture SHA-256 is
`40950ff784256db70daae36f6320b126f4917ff649f6bb996ec5982f80306ad0`.
Tests lock the bytes, range, order, one-connection behavior, full-U16 relay
preservation, and no-partial-snapshot failure path.

This offline promotion deliberately does not claim a new target read or a
simultaneous Modbus/cloud relay bracket. The exact address and raw semantics
come from the compatible maps and existing canonical schema/fixture; a first
supervised target read remains pending. The production code contract is now
24 sequential requests on the first fresh connection and 22 on later fresh
connections after the two physical gates are cached. `BMST`, the source-local
register-551 state, and canonical `AC` raise the exact Modbus surface from 77
to 80 unique metrics. Any status-read transport/protocol failure or invalid
register-551 low nibble rejects the complete snapshot without retry or
reconnect so configured cloud fallback can run.

## Stage 22: Signed Output and Torn-Pair Guard (Supersedes Stage 19)

At `2026-08-16T17:16:33Z`, the Stage 19 positive-only decoder caused the
production priority chain to reject a Modbus snapshot because output high-word
register 691 was `0xFFFF`, then continue to the Jinko source as designed. This
is direct target evidence that the output
register family enters the negative sign-extension domain. The application log
did not include registers 633-636, registers 692-694, or the resulting Jinko
phase values, so it is not recorded as an exact negative magnitude correlation.

This stage supersedes only Stage 19's accepted high-word and value domain. The
output decoder consequently uses the compatible-map low/high pairing while
retaining a conservative, mechanically safe value domain. It joins 633/691
through 636/694 low-word-first, converts the full word to signed 32-bit, accepts
only high words `0x0000` and `0xFFFF`, requires every joined value to remain in
`-32767..32767 W`, and compares the three-phase sum to the total in signed
`int64`. Register 637/695 remains ignored.

The symmetric bound is what makes the two separate reads safe across a zero
crossing. A negative low word paired with a later `0x0000` high decodes above
`32767 W`; a nonnegative low word paired with a later `0xFFFF` high decodes
below `-32767 W`. Both fail before the phase-sum gate, including constructed
cases where an unsigned or naively signed sum could otherwise still match.
Within the envelope, the high word contains sign extension only, so ordinary
magnitude changes with an unchanged sign cannot tear the value. The boundary
is not derived from inverter rated power or AC pass-through current.

This promotion changes neither the fixed read plan nor the canonical surface:
the first fetch remains 24 requests, later fetches remain 22, and a successful
core snapshot remains 80 metrics. Unit tests cover both signed boundaries,
mixed and all-negative values, both torn-crossing directions, invalid high
words, exact signed-sum failure, and ignored apparent power. End-to-end client
tests cover successful signed publication plus fail-closed no-partial-snapshot
behavior. The independent synthetic signed-high protocol fixture is locked by
SHA-256 `c7f948fb86ca8dda4365843fc9840bf705574e599056b6dbf91f4b4f4bd420b2`.
