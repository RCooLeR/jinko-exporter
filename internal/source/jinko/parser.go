package jinko

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/RCooLeR/jinko-exporter/internal/model"
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
			value, ok := parseNumber(firstNonEmpty(f.OrgValue, f.Value))
			if !ok {
				continue
			}
			key := strings.TrimSpace(f.StorageName)
			if key == "" {
				key = SanitizeKey(f.Key)
			}
			metrics = append(metrics, model.Metric{
				Group: group,
				Key:   key,
				Name:  strings.TrimSpace(f.Key),
				Unit:  normalizeUnit(f.Unit),
				Value: value,
			})
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
	case "в„ѓ":
		return "C"
	default:
		return unit
	}
}

func parseNumber(raw string) (float64, bool) {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, "\u00a0", " "))
	if raw == "" {
		return 0, false
	}
	raw = strings.ReplaceAll(raw, ",", "")
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, false
	}
	return value, true
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
