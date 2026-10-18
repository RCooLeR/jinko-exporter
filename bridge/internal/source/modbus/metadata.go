package modbus

import (
	"fmt"
	"strconv"
)

// readMetadata uses only the four map-agreed read-only blocks. Registers 12/13
// conflict across maps and are intentionally absent. Stage 31 records the target
// capture; this preserves raw words, not MAIN/HMI aliases.
func readMetadata(session *readSession) (map[string]string, error) {
	protocol, err := session.readApproved(protocolVersionRead)
	if err != nil {
		return nil, err
	}
	controller, err := session.readApproved(controllerVersionsRead)
	if err != nil {
		return nil, err
	}
	firmware, err := session.readApproved(firmwareWordsRead)
	if err != nil {
		return nil, err
	}
	battery, err := session.readApproved(batteryInputCodeRead)
	if err != nil {
		return nil, err
	}
	return decodeMetadata(protocol, controller, firmware, battery)
}

func decodeMetadata(protocol, controller, firmware, battery []uint16) (map[string]string, error) {
	for _, block := range []struct {
		name  string
		words []uint16
		count int
	}{
		{"protocol version register 2", protocol, 1},
		{"controller versions register 11", controller, 1},
		{"firmware registers 14-18", firmware, 5},
		{"battery input code register 24", battery, 1},
	} {
		if err := requireWordCount(block.name, block.words, block.count); err != nil {
			return nil, err
		}
	}
	if battery[0] > 4 {
		return nil, fmt.Errorf("battery input code register 24 is %d; documented raw code range is 0..4", battery[0])
	}
	return map[string]string{
		"protocol_version_hex":         fmt.Sprintf("%04X", protocol[0]),
		"control_firmware_words":       fmt.Sprintf("%04X-%04X", firmware[0], firmware[1]),
		"communication_firmware_words": fmt.Sprintf("%04X-%04X-%04X", firmware[2], firmware[3], firmware[4]),
		"bootloader_version_hex":       fmt.Sprintf("%02X", byte(controller[0])),
		"assistant_version_hex":        fmt.Sprintf("%02X", byte(controller[0]>>8)),
		"battery_input_code":           strconv.FormatUint(uint64(battery[0]), 10),
	}, nil
}
