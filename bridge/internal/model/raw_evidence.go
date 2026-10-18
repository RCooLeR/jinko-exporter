package model

import "time"

// ModbusReadWindow bounds one already-scheduled, protocol-validated FC03 read.
// The four windows describe sequential reads, not an atomic inverter sample.
type ModbusReadWindow struct {
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
}

// ModbusEvidence contains only fixed raw words copied from a complete successful
// Fetch. It has no frames, network addresses, logger/device identity, credentials,
// arbitrary metadata, or inferred register meanings. Fixed arrays make value
// copies independent; each snapshot owns a fresh instance. Downstream diagnostic
// observers must copy the value before retaining it and observe it only after
// Priority accepts the snapshot. This data does not authorize additional reads.
type ModbusEvidence struct {
	StartedAt      time.Time        `json:"started_at"`
	FinishedAt     time.Time        `json:"finished_at"`
	BMSWords       [9]uint16        `json:"bms_words_210_218"`
	BMSWindow      ModbusReadWindow `json:"bms_window"`
	RelayWords     [2]uint16        `json:"relay_words_551_552"`
	RelayWindow    ModbusReadWindow `json:"relay_window"`
	AlertWords     [6]uint16        `json:"alert_words_553_558"`
	AlertWindow    ModbusReadWindow `json:"alert_window"`
	RunState       uint16           `json:"run_state_word_500"`
	RunStateWindow ModbusReadWindow `json:"run_state_window"`
}
