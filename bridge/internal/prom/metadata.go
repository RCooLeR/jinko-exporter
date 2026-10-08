package prom

import (
	"strings"

	"github.com/prometheus/client_golang/prometheus"
)

// Deliberately not a generic Meta exporter: URLs, transport details and other
// private/source-specific diagnostics must not become Prometheus value labels.
func (c *Collector) collectMetadataInfo(ch chan<- prometheus.Metric, meta map[string]string, sourceName, deviceSN string) {
	for _, key := range [...]string{
		"protocol_version_hex",
		"control_firmware_words",
		"communication_firmware_words",
		"bootloader_version_hex",
		"assistant_version_hex",
		"battery_input_code",
	} {
		value, present := meta[key]
		if !present || !validMetadataInfoValue(key, value) {
			continue
		}
		labels := append(c.deviceLabelValues(sourceName, deviceSN), key, value)
		ch <- prometheus.MustNewConstMetric(c.metadataInfoDesc, prometheus.GaugeValue, 1, labels...)
	}
}

// Require the producer's exact lossless representation; do not parse versions
// as numbers, trim/coerce malformed strings, or infer the meaning of raw codes.
func validMetadataInfoValue(key, value string) bool {
	words, width := 1, 4
	switch key {
	case "protocol_version_hex":
	case "control_firmware_words":
		words = 2
	case "communication_firmware_words":
		words = 3
	case "bootloader_version_hex", "assistant_version_hex":
		width = 2
	case "battery_input_code":
		return len(value) == 1 && value[0] >= '0' && value[0] <= '4'
	default:
		return false
	}
	if len(value) != words*width+words-1 {
		return false
	}
	parts := strings.Split(value, "-")
	if len(parts) != words {
		return false
	}
	for _, part := range parts {
		if len(part) != width {
			return false
		}
		for _, digit := range part {
			if !((digit >= '0' && digit <= '9') || (digit >= 'A' && digit <= 'F')) {
				return false
			}
		}
	}
	return true
}
