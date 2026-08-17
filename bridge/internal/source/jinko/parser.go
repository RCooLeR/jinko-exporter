package jinko

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

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
	for _, cat := range payload.Categories {
		group := normalizeGroup(cat.Tag, cat.Name)
		for _, f := range cat.FieldList {
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

	collectedAt := time.Now().UTC()
	if payload.CollectionTime > 0 {
		collectedAt = time.Unix(int64(payload.CollectionTime), 0).UTC()
	}

	return &model.Snapshot{
		DeviceSN:    strings.TrimSpace(payload.DeviceSN),
		ParentSN:    strings.TrimSpace(payload.ParentDeviceSN),
		DeviceID:    strconv.FormatInt(payload.DeviceID, 10),
		SiteID:      strconv.FormatInt(payload.SiteID, 10),
		CollectedAt: collectedAt,
		Metrics:     metrics,
	}, nil
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
