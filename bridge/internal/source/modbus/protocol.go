package modbus

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	v5RequestLength     = 36
	v5RequestRTUOffset  = 26
	v5ResponseRTUOffset = 25
	maxV5FrameLength    = 128
)

func buildReadRequest(loggerSerial uint32, unit byte, id readID) ([]byte, error) {
	if loggerSerial == 0 {
		return nil, errors.New("logger serial must be non-zero")
	}
	if unit != modbusUnitID {
		return nil, fmt.Errorf("unit %d is not approved", unit)
	}
	spec, ok := approvedReadSpec(id)
	if !ok {
		return nil, fmt.Errorf("modbus read ID %d is not in the compile-time allowlist", id)
	}

	frame := make([]byte, v5RequestLength)
	frame[0] = 0xA5
	binary.LittleEndian.PutUint16(frame[1:3], uint16(v5RequestLength-13))
	frame[3] = 0x10
	frame[4] = 0x45
	frame[5] = spec.sequence
	frame[6] = 0x00
	binary.LittleEndian.PutUint32(frame[7:11], loggerSerial)
	frame[11] = 0x02

	rtu := frame[v5RequestRTUOffset : v5RequestRTUOffset+8]
	rtu[0] = unit
	rtu[1] = readHoldingRegisters
	binary.BigEndian.PutUint16(rtu[2:4], spec.start)
	binary.BigEndian.PutUint16(rtu[4:6], spec.quantity)
	crc := modbusCRC(rtu[:6])
	rtu[6] = byte(crc)
	rtu[7] = byte(crc >> 8)

	frame[len(frame)-2] = checksum8(frame[1 : len(frame)-2])
	frame[len(frame)-1] = 0x15
	if err := validateReadRequest(frame, loggerSerial, unit, id); err != nil {
		return nil, err
	}
	return frame, nil
}

func validateReadRequest(frame []byte, loggerSerial uint32, unit byte, id readID) error {
	spec, ok := approvedReadSpec(id)
	if !ok {
		return errors.New("request is not in the compile-time allowlist")
	}
	if len(frame) != v5RequestLength {
		return fmt.Errorf("request length is %d; expected %d", len(frame), v5RequestLength)
	}
	if frame[0] != 0xA5 || frame[len(frame)-1] != 0x15 {
		return errors.New("invalid V5 request markers")
	}
	if int(binary.LittleEndian.Uint16(frame[1:3]))+13 != len(frame) {
		return errors.New("invalid V5 request length field")
	}
	if frame[3] != 0x10 || frame[4] != 0x45 || frame[5] != spec.sequence || frame[6] != 0x00 {
		return errors.New("invalid V5 request control or sequence")
	}
	if binary.LittleEndian.Uint32(frame[7:11]) != loggerSerial || frame[11] != 0x02 {
		return errors.New("invalid V5 request serial or frame type")
	}
	for _, value := range frame[12:v5RequestRTUOffset] {
		if value != 0 {
			return errors.New("V5 request reserved bytes must be zero")
		}
	}
	if checksum8(frame[1:len(frame)-2]) != frame[len(frame)-2] {
		return errors.New("invalid V5 request checksum")
	}

	rtu := frame[v5RequestRTUOffset : len(frame)-2]
	if len(rtu) != 8 || !validModbusCRC(rtu) {
		return errors.New("invalid Modbus request length or CRC")
	}
	if rtu[0] != unit || rtu[1] != readHoldingRegisters {
		return errors.New("request is not the approved FC03 operation")
	}
	if binary.BigEndian.Uint16(rtu[2:4]) != spec.start || binary.BigEndian.Uint16(rtu[4:6]) != spec.quantity {
		return errors.New("request register range differs from the approved read")
	}
	return nil
}

func parseReadResponse(frame []byte, loggerSerial uint32, unit byte, id readID) ([]uint16, error) {
	spec, ok := approvedReadSpec(id)
	if !ok {
		return nil, errors.New("response spec is not in the compile-time allowlist")
	}
	if len(frame) < 18 {
		return nil, fmt.Errorf("V5 frame is too short: %d", len(frame))
	}
	if frame[0] != 0xA5 || frame[len(frame)-1] != 0x15 {
		return nil, errors.New("invalid V5 response markers")
	}
	payloadLength := int(binary.LittleEndian.Uint16(frame[1:3]))
	if len(frame) != payloadLength+13 {
		return nil, fmt.Errorf("V5 response length mismatch: frame=%d payload=%d", len(frame), payloadLength)
	}
	if checksum8(frame[1:len(frame)-2]) != frame[len(frame)-2] {
		return nil, errors.New("invalid V5 response checksum")
	}
	if frame[3] != 0x10 || frame[4] != 0x15 {
		return nil, fmt.Errorf("unexpected V5 response control %02X %02X", frame[3], frame[4])
	}
	// Only the low sequence byte is echoed. Byte 6 is maintained by the logger.
	if frame[5] != spec.sequence {
		return nil, fmt.Errorf("unexpected V5 response sequence 0x%02X", frame[5])
	}
	if binary.LittleEndian.Uint32(frame[7:11]) != loggerSerial {
		return nil, errors.New("response contains a different logger serial")
	}
	if frame[11] != 0x02 || frame[12] != 0x01 {
		return nil, fmt.Errorf("unexpected V5 response type/status %02X/%02X", frame[11], frame[12])
	}
	if len(frame) < v5ResponseRTUOffset+5+2 {
		return nil, errors.New("V5 response cannot contain a Modbus RTU frame")
	}

	rtu := frame[v5ResponseRTUOffset : len(frame)-2]
	if len(rtu) == 5 && rtu[0] == unit && rtu[1] == (readHoldingRegisters|0x80) {
		if !validModbusCRC(rtu) {
			return nil, errors.New("invalid CRC on Modbus exception response")
		}
		return nil, fmt.Errorf("modbus exception 0x%02X", rtu[2])
	}
	expectedRTULength := 5 + int(spec.quantity)*2
	if len(rtu) != expectedRTULength {
		return nil, fmt.Errorf("unexpected Modbus response length %d; expected %d", len(rtu), expectedRTULength)
	}
	if !validModbusCRC(rtu) {
		return nil, errors.New("invalid Modbus response CRC")
	}
	if rtu[0] != unit || rtu[1] != readHoldingRegisters || int(rtu[2]) != int(spec.quantity)*2 {
		return nil, fmt.Errorf("unexpected Modbus response header %02X %02X %02X", rtu[0], rtu[1], rtu[2])
	}

	values := make([]uint16, spec.quantity)
	for index := range values {
		offset := 3 + index*2
		values[index] = binary.BigEndian.Uint16(rtu[offset : offset+2])
	}
	return values, nil
}

func checksum8(data []byte) byte {
	var sum byte
	for _, value := range data {
		sum += value
	}
	return sum
}

func modbusCRC(data []byte) uint16 {
	crc := uint16(0xFFFF)
	for _, value := range data {
		crc ^= uint16(value)
		for range 8 {
			if crc&1 != 0 {
				crc = (crc >> 1) ^ 0xA001
			} else {
				crc >>= 1
			}
		}
	}
	return crc
}

func validModbusCRC(frame []byte) bool {
	if len(frame) < 3 {
		return false
	}
	crc := modbusCRC(frame[:len(frame)-2])
	return frame[len(frame)-2] == byte(crc) && frame[len(frame)-1] == byte(crc>>8)
}
