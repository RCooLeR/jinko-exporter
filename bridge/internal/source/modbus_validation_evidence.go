package source

import (
	"errors"
	"sync"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/model"
	"github.com/rs/zerolog/log"
)

const (
	validationChangeInterval = time.Minute
	validationChangeBudget   = 64
)

// This observer is passive: it cannot fetch a source, notify, retry, or write
// a device. Its nine one-time milestones and 64 transition records are bounded
// per process; restart intentionally starts a new evidence session.
type modbusValidationObserver struct {
	mu         sync.Mutex
	seen       uint16
	last       validationStatus
	hasLast    bool
	lastChange time.Time
	changes    int
	exhausted  bool
	sequence   uint64
	now        func() time.Time
	emit       func(modbusValidationRecord)
}

type validationStatus struct {
	relay [2]uint16
	alert [6]uint16
	run   uint16
}

type modbusValidationRecord struct {
	CaptureIndex uint64               `json:"capture_index"`
	Reasons      []string             `json:"reasons"`
	Evidence     model.ModbusEvidence `json:"evidence"`
}

// EnableModbusValidationEvidence is called once during source construction,
// before Fetch starts. It never starts a background task or enables polling.
func (p *Priority) EnableModbusValidationEvidence() error {
	if p == nil || len(p.sources) == 0 || isNilInterface(p.sources[0]) || p.sources[0].Name() != "modbus" {
		return errors.New("Modbus validation evidence requires modbus as the first source")
	}
	if p.validationEvidence != nil {
		return errors.New("Modbus validation evidence is already enabled")
	}
	for _, src := range p.sources[1:] {
		if !isNilInterface(src) && src.Name() == "modbus" {
			return errors.New("Modbus validation evidence requires exactly one Modbus source")
		}
	}
	p.validationEvidence = &modbusValidationObserver{now: time.Now, emit: logModbusValidationEvidence}
	return nil
}

func logModbusValidationEvidence(record modbusValidationRecord) {
	log.Info().
		Str("source", "modbus").
		Str("profile", "jks-6-20h-ei-readonly-v1").
		Uint64("capture_index", record.CaptureIndex).
		Strs("reasons", record.Reasons).
		Interface("raw_evidence", record.Evidence).
		Msg("recorded passive Modbus validation evidence; raw words are not decoded sensors")
}

func (o *modbusValidationObserver) observe(snapshot *model.Snapshot) {
	if o == nil || !validModbusValidationEvidence(snapshot) {
		return
	}
	// Fixed arrays/windows are value copies. Neither later snapshot mutation nor
	// the logging sink can alter the deduplication state or a retained record.
	evidence := *snapshot.ModbusEvidence
	status := validationStatus{relay: evidence.RelayWords, alert: evidence.AlertWords, run: evidence.RunState}
	o.mu.Lock()
	defer o.mu.Unlock()
	now := o.now()
	var reasons []string
	conditions := [...]struct {
		active bool
		reason string
	}{
		{true, "first_complete_sample"},
		{evidence.BMSWords[1] != 0, "r211_first_nonzero"},
		{evidence.BMSWords[2] != 0, "r212_first_nonzero"},
		{evidence.BMSWords[8] != 0, "r218_first_nonzero"},
		{evidence.BMSWords[1] != 0 && evidence.BMSWords[1] != 0xffff, "r211_first_nonzero_non_ffff"},
		{evidence.BMSWords[2] != 0 && evidence.BMSWords[2] != 0xffff, "r212_first_nonzero_non_ffff"},
		{evidence.BMSWords[8] != 0 && evidence.BMSWords[8] != 0xffff, "r218_first_nonzero_non_ffff"},
		{evidence.BMSWords[6]&0x8000 != 0, "r216_first_high_bit"},
		{evidence.BMSWords[6]&0x8000 != 0 && evidence.BMSWords[6] != 0xffff, "r216_first_high_bit_non_ffff"},
	}
	for index, condition := range conditions {
		flag := uint16(1 << index)
		if condition.active && o.seen&flag == 0 {
			o.seen |= flag
			reasons = append(reasons, condition.reason)
		}
	}
	changed := o.hasLast && status != o.last
	if changed && o.changes < validationChangeBudget && now.Sub(o.lastChange) >= validationChangeInterval {
		o.changes++
		o.lastChange = now
		reasons = append(reasons, "status_words_changed")
	}
	if changed && o.changes >= validationChangeBudget && !o.exhausted {
		o.exhausted = true
		reasons = append(reasons, "status_change_budget_exhausted")
	}
	if len(reasons) == 0 {
		return
	}
	if !o.hasLast {
		o.lastChange = now
	}
	o.last, o.hasLast = status, true // Last emitted, never merely observed.
	o.sequence++
	o.emit(modbusValidationRecord{CaptureIndex: o.sequence, Reasons: reasons, Evidence: evidence})
}

func validModbusValidationEvidence(snapshot *model.Snapshot) bool {
	if snapshot == nil || snapshot.Source != "modbus" || snapshot.ModbusEvidence == nil {
		return false
	}
	e := snapshot.ModbusEvidence
	if e.StartedAt.IsZero() || e.FinishedAt.Before(e.StartedAt) ||
		e.FinishedAt.Sub(e.StartedAt) > 30*time.Second || !e.FinishedAt.Equal(snapshot.CollectedAt) {
		return false
	}
	previous := e.StartedAt
	for _, window := range [...]model.ModbusReadWindow{e.BMSWindow, e.RelayWindow, e.AlertWindow, e.RunStateWindow} {
		if window.StartedAt.Before(previous) || window.FinishedAt.Before(window.StartedAt) || window.FinishedAt.After(e.FinishedAt) {
			return false
		}
		previous = window.FinishedAt
	}
	signature, err := ModbusAlertSignatureFromSnapshot(snapshot)
	return err == nil && signature.values() == e.AlertWords
}
