package jinko

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/model"
)

type detailResponse struct {
	DeviceID       int64      `json:"deviceId"`
	DeviceSN       string     `json:"deviceSn"`
	SiteID         int64      `json:"siteId"`
	CollectionTime float64    `json:"collectionTime"`
	ParentDeviceSN string     `json:"parDeviceSn"`
	Categories     []category `json:"paramCategoryList"`
}

type category struct {
	Name      string  `json:"name"`
	Tag       string  `json:"tag"`
	FieldList []field `json:"fieldList"`
}

type field struct {
	Key         string `json:"key"`
	Value       string `json:"value"`
	StorageName string `json:"storageName"`
	Unit        string `json:"unit"`
	OrgValue    string `json:"orgValue"`
}

func ParseDetailResponse(raw []byte) (*model.Snapshot, error) {
	var payload detailResponse
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decode jinko detail response: %w", err)
	}

	metrics := make([]model.Metric, 0, 128)
	metadata := make(map[string]string)
	rejectedMetadata := make(map[string]bool)
	for _, cat := range payload.Categories {
		group := normalizeGroup(cat.Tag, cat.Name)
		for _, f := range cat.FieldList {
			collectCloudMetadata(metadata, rejectedMetadata, f)
			value, ok := parseFieldNumber(f)
			if !ok {
				continue
			}
			key := strings.TrimSpace(f.StorageName)
			if key == "" {
				key = SanitizeKey(f.Key)
			}
			metric := model.Metric{
				Group: group,
				Key:   key,
				Name:  strings.TrimSpace(f.Key),
				Unit:  normalizeUnit(f.Unit),
				Value: value,
			}
			if canonical, ok := CanonicalizeMetric(metric); ok {
				metric = canonical
			}
			metrics = append(metrics, metric)
		}
	}

	// The API can return cached values while a device is offline. Missing or
	// invalid collection times must not turn that cache into a fresh snapshot.
	var collectedAt time.Time
	if payload.CollectionTime > 0 && payload.CollectionTime < float64(math.MaxInt64) {
		seconds, fraction := math.Modf(payload.CollectionTime)
		collectedAt = time.Unix(int64(seconds), int64(fraction*float64(time.Second))).UTC()
	}
	if len(metadata) == 0 {
		metadata = nil
	}

	return &model.Snapshot{
		DeviceSN:    strings.TrimSpace(payload.DeviceSN),
		ParentSN:    strings.TrimSpace(payload.ParentDeviceSN),
		DeviceID:    strconv.FormatInt(payload.DeviceID, 10),
		SiteID:      strconv.FormatInt(payload.SiteID, 10),
		CollectedAt: collectedAt,
		Metrics:     metrics,
		Meta:        metadata,
	}, nil
}

// Cloud text is its own source contract. In particular, these values do not
// establish aliases for raw Modbus words or interpret firmware sentinels.
func collectCloudMetadata(metadata map[string]string, rejected map[string]bool, f field) {
	var key string
	switch f.StorageName {
	case "PTCv1":
		key = "cloud_protocol_version"
	case "MAIN":
		key = "cloud_main_firmware"
	case "HMI":
		key = "cloud_hmi_firmware"
	case "CBAMSV":
		key = "cloud_assistant_version"
	case "CBAVM":
		key = "cloud_bootloader_version"
	case "L_B_H_V_N":
		key = "cloud_battery_hardware_version"
	case "L_B_H_V_N2":
		key = "cloud_battery2_hardware_version"
	case "BAT_FAC":
		key = "cloud_battery_manufacturer"
	default:
		return
	}
	if rejected[key] {
		return
	}
	value := f.OrgValue
	if value == "" {
		value = f.Value
	}
	valid := strings.TrimSpace(value) != "" && len(value) <= 128
	for _, r := range value {
		if unicode.IsControl(r) {
			valid = false
			break
		}
	}
	previous, exists := metadata[key]
	if !valid || (exists && previous != value) {
		// Once ambiguous or malformed, this key remains absent regardless of
		// later field order. Other metadata and legacy metrics stay independent.
		delete(metadata, key)
		rejected[key] = true
		return
	}
	metadata[key] = value
}

func normalizeGroup(tag string, name string) string {
	for _, candidate := range []string{tag, name} {
		value := SanitizeKey(candidate)
		switch value {
		case "basic", "electric", "electricity_generation", "grid", "load", "battery", "bms", "bms2", "temperature", "status", "state", "alert":
			return value
		}
	}
	return SanitizeKey(firstNonEmpty(tag, name))
}

func normalizeUnit(unit string) string {
	unit = strings.TrimSpace(unit)
	switch unit {
	case "\u2103":
		return "C"
	default:
		return unit
	}
}

func parseFieldNumber(f field) (float64, bool) {
	for _, candidate := range []string{f.OrgValue, f.Value} {
		if value, ok := parseNumber(candidate); ok {
			return value, true
		}
	}
	return 0, false
}

func parseNumber(raw string) (float64, bool) {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, "\u00a0", " "))
	if raw == "" {
		return 0, false
	}
	raw = normalizeNumberSeparators(raw)
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, false
	}
	return value, true
}

func normalizeNumberSeparators(raw string) string {
	raw = strings.ReplaceAll(raw, " ", "")
	if !strings.Contains(raw, ",") {
		return raw
	}
	if strings.Contains(raw, ".") {
		return strings.ReplaceAll(raw, ",", "")
	}

	parts := strings.Split(raw, ",")
	if len(parts) == 2 {
		whole, fraction := parts[0], parts[1]
		if len(fraction) == 3 && len(whole) > 0 {
			return whole + fraction
		}
		return whole + "." + fraction
	}
	return strings.ReplaceAll(raw, ",", "")
}

func SanitizeKey(input string) string {
	input = strings.ToLower(strings.TrimSpace(input))
	replacer := strings.NewReplacer(" ", "_", "-", "_", "/", "_", ".", "_", "(", "", ")", "", "%", "pct")
	input = replacer.Replace(input)
	var b strings.Builder
	b.Grow(len(input))
	lastUnderscore := false
	for _, r := range input {
		keep := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if keep {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	return strings.Trim(b.String(), "_")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
