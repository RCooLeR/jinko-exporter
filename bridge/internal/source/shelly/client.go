package shelly

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/config"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/model"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/source"
)

const (
	gridLoadGroup                 = "grid_load"
	maxShellyRPCResponseBytes int = 64 * 1024
)

var _ source.Source = (*GridLoadClient)(nil)

type GridLoadClient struct {
	cfg             config.ShellyGridLoadConfig
	baseURL         *url.URL
	metadataBaseURL string
	client          *http.Client
}

type emStatus struct {
	ID                   *int     `json:"id"`
	ACurrent             *float64 `json:"a_current"`
	AVoltage             *float64 `json:"a_voltage"`
	AActivePower         *float64 `json:"a_act_power"`
	AApparentPower       *float64 `json:"a_aprt_power"`
	APowerFactor         *float64 `json:"a_pf"`
	AFrequency           *float64 `json:"a_freq"`
	BCurrent             *float64 `json:"b_current"`
	BVoltage             *float64 `json:"b_voltage"`
	BActivePower         *float64 `json:"b_act_power"`
	BApparentPower       *float64 `json:"b_aprt_power"`
	BPowerFactor         *float64 `json:"b_pf"`
	BFrequency           *float64 `json:"b_freq"`
	CCurrent             *float64 `json:"c_current"`
	CVoltage             *float64 `json:"c_voltage"`
	CActivePower         *float64 `json:"c_act_power"`
	CApparentPower       *float64 `json:"c_aprt_power"`
	CPowerFactor         *float64 `json:"c_pf"`
	CFrequency           *float64 `json:"c_freq"`
	NCurrent             *float64 `json:"n_current"`
	TotalCurrent         *float64 `json:"total_current"`
	TotalActivePower     *float64 `json:"total_act_power"`
	TotalApparentPower   *float64 `json:"total_aprt_power"`
	UserCalibratedPhases []string `json:"user_calibrated_phase"`
}

type emDataStatus struct {
	ID                     *int     `json:"id"`
	ATotalActiveEnergyWh   *float64 `json:"a_total_act_energy"`
	ATotalReturnedEnergyWh *float64 `json:"a_total_act_ret_energy"`
	BTotalActiveEnergyWh   *float64 `json:"b_total_act_energy"`
	BTotalReturnedEnergyWh *float64 `json:"b_total_act_ret_energy"`
	CTotalActiveEnergyWh   *float64 `json:"c_total_act_energy"`
	CTotalReturnedEnergyWh *float64 `json:"c_total_act_ret_energy"`
	TotalActiveEnergyWh    *float64 `json:"total_act"`
	TotalReturnedEnergyWh  *float64 `json:"total_act_ret"`
}

func NewGridLoadClient(cfg config.ShellyGridLoadConfig) (*GridLoadClient, error) {
	rawBaseURL := strings.TrimSpace(cfg.BaseURL)
	if err := config.ValidateShellyGridLoadURL(rawBaseURL); err != nil {
		return nil, err
	}
	if cfg.Timeout <= 0 {
		return nil, fmt.Errorf("shelly grid-load timeout must be > 0")
	}
	base, err := url.Parse(rawBaseURL)
	if err != nil {
		return nil, fmt.Errorf("shelly grid-load URL is invalid")
	}
	base.Scheme = strings.ToLower(base.Scheme)

	// Snapshot metadata is externally observable through MQTT. Publish only the
	// origin and keep any reverse-proxy path local to the HTTP client.
	metadataURL := (&url.URL{Scheme: base.Scheme, Host: base.Host}).String()

	return &GridLoadClient{
		cfg:             cfg,
		baseURL:         base,
		metadataBaseURL: metadataURL,
		client: &http.Client{
			Timeout: cfg.Timeout,
			// A Shelly RPC endpoint has no legitimate redirect flow. Refusing
			// redirects keeps every request on the validated origin and prevents
			// HTTPS downgrade or cross-host redirect surprises.
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

func (c *GridLoadClient) Name() string {
	return "shelly_grid_load"
}

func (c *GridLoadClient) Fetch(ctx context.Context) (*model.Snapshot, error) {
	var em emStatus
	if err := c.getRPC(ctx, "EM.GetStatus", &em); err != nil {
		return nil, err
	}
	if err := em.validate(c.cfg.EMID); err != nil {
		return nil, fmt.Errorf("validate Shelly EM.GetStatus: %w", err)
	}

	var emData emDataStatus
	if err := c.getRPC(ctx, "EMData.GetStatus", &emData); err != nil {
		return nil, err
	}
	if err := emData.validate(c.cfg.EMID); err != nil {
		return nil, fmt.Errorf("validate Shelly EMData.GetStatus: %w", err)
	}

	metrics := make([]model.Metric, 0, 32)
	add := func(key, name, unit string, value *float64) {
		if value == nil {
			return
		}
		metrics = append(metrics, model.Metric{
			Group: gridLoadGroup,
			Key:   key,
			Name:  name,
			Unit:  unit,
			Value: *value,
		})
	}
	addKWh := func(key, name string, valueWh *float64) {
		if valueWh == nil {
			return
		}
		value := *valueWh / 1000
		add(key, name, "kWh", &value)
	}

	addPhaseMetrics(&metrics, "l1", "L1", em.AVoltage, em.ACurrent, em.AActivePower, em.AApparentPower, em.APowerFactor, em.AFrequency)
	addPhaseMetrics(&metrics, "l2", "L2", em.BVoltage, em.BCurrent, em.BActivePower, em.BApparentPower, em.BPowerFactor, em.BFrequency)
	addPhaseMetrics(&metrics, "l3", "L3", em.CVoltage, em.CCurrent, em.CActivePower, em.CApparentPower, em.CPowerFactor, em.CFrequency)
	add("neutral_current", "Grid Load Neutral Current", "A", em.NCurrent)
	add("total_current", "Grid Load Total Current", "A", em.TotalCurrent)
	add("total_power", "Grid Load Total Power", "W", em.TotalActivePower)
	add("total_apparent_power", "Grid Load Total Apparent Power", "VA", em.TotalApparentPower)
	addKWh("l1_energy_total", "Grid Load L1 Energy Total", emData.ATotalActiveEnergyWh)
	addKWh("l1_returned_energy_total", "Grid Load L1 Returned Energy Total", emData.ATotalReturnedEnergyWh)
	addKWh("l2_energy_total", "Grid Load L2 Energy Total", emData.BTotalActiveEnergyWh)
	addKWh("l2_returned_energy_total", "Grid Load L2 Returned Energy Total", emData.BTotalReturnedEnergyWh)
	addKWh("l3_energy_total", "Grid Load L3 Energy Total", emData.CTotalActiveEnergyWh)
	addKWh("l3_returned_energy_total", "Grid Load L3 Returned Energy Total", emData.CTotalReturnedEnergyWh)
	addKWh("energy_total", "Grid Load Energy Total", emData.TotalActiveEnergyWh)
	addKWh("returned_energy_total", "Grid Load Returned Energy Total", emData.TotalReturnedEnergyWh)

	return &model.Snapshot{
		Source:      c.Name(),
		DeviceSN:    strings.Trim(strings.TrimPrefix(c.baseURL.Host, "["), "]"),
		CollectedAt: time.Now().UTC(),
		Metrics:     metrics,
		Meta: map[string]string{
			"shelly_grid_load_url":   c.metadataBaseURL,
			"shelly_grid_load_em_id": strconv.Itoa(c.cfg.EMID),
		},
	}, nil
}

func (status emStatus) validate(expectedID int) error {
	if status.ID == nil {
		return fmt.Errorf("response is missing component id")
	}
	if *status.ID != expectedID {
		return fmt.Errorf("component id is %d, want %d", *status.ID, expectedID)
	}
	if status.ACurrent == nil && status.AVoltage == nil && status.AActivePower == nil &&
		status.AApparentPower == nil && status.APowerFactor == nil && status.AFrequency == nil &&
		status.BCurrent == nil && status.BVoltage == nil && status.BActivePower == nil &&
		status.BApparentPower == nil && status.BPowerFactor == nil && status.BFrequency == nil &&
		status.CCurrent == nil && status.CVoltage == nil && status.CActivePower == nil &&
		status.CApparentPower == nil && status.CPowerFactor == nil && status.CFrequency == nil &&
		status.NCurrent == nil && status.TotalCurrent == nil && status.TotalActivePower == nil &&
		status.TotalApparentPower == nil {
		return fmt.Errorf("response contains no recognized telemetry")
	}
	return nil
}

func (status emDataStatus) validate(expectedID int) error {
	if status.ID == nil {
		return fmt.Errorf("response is missing component id")
	}
	if *status.ID != expectedID {
		return fmt.Errorf("component id is %d, want %d", *status.ID, expectedID)
	}
	if status.ATotalActiveEnergyWh == nil && status.ATotalReturnedEnergyWh == nil &&
		status.BTotalActiveEnergyWh == nil && status.BTotalReturnedEnergyWh == nil &&
		status.CTotalActiveEnergyWh == nil && status.CTotalReturnedEnergyWh == nil &&
		status.TotalActiveEnergyWh == nil && status.TotalReturnedEnergyWh == nil {
		return fmt.Errorf("response contains no recognized telemetry")
	}
	return nil
}

func addPhaseMetrics(metrics *[]model.Metric, keyPrefix, label string, voltage, current, activePower, apparentPower, powerFactor, frequency *float64) {
	add := func(key, name, unit string, value *float64) {
		if value == nil {
			return
		}
		*metrics = append(*metrics, model.Metric{
			Group: gridLoadGroup,
			Key:   key,
			Name:  name,
			Unit:  unit,
			Value: *value,
		})
	}
	add(keyPrefix+"_voltage", "Grid Load "+label+" Voltage", "V", voltage)
	add(keyPrefix+"_current", "Grid Load "+label+" Current", "A", current)
	add(keyPrefix+"_power", "Grid Load "+label+" Power", "W", activePower)
	add(keyPrefix+"_apparent_power", "Grid Load "+label+" Apparent Power", "VA", apparentPower)
	add(keyPrefix+"_power_factor", "Grid Load "+label+" Power Factor", "", powerFactor)
	add(keyPrefix+"_frequency", "Grid Load "+label+" Frequency", "Hz", frequency)
}

func (c *GridLoadClient) getRPC(ctx context.Context, method string, target any) error {
	u := *c.baseURL
	u.Path = strings.TrimRight(u.Path, "/") + "/rpc/" + method
	q := u.Query()
	q.Set("id", strconv.Itoa(c.cfg.EMID))
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("fetch Shelly %s: %w", method, ctxErr)
		}
		if urlErr, ok := errors.AsType[*url.Error](err); ok {
			err = urlErr.Err
		}
		return fmt.Errorf("fetch Shelly %s: request failed: %w", method, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("fetch Shelly %s: status %d", method, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxShellyRPCResponseBytes)+1))
	if err != nil {
		return fmt.Errorf("read Shelly %s response: %w", method, err)
	}
	if len(body) > maxShellyRPCResponseBytes {
		return fmt.Errorf("read Shelly %s response: body exceeds %d bytes", method, maxShellyRPCResponseBytes)
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return fmt.Errorf("decode Shelly %s: empty response", method)
	}

	var rpcError struct {
		Code *int `json:"code"`
	}
	if err := json.Unmarshal(body, &rpcError); err != nil {
		return fmt.Errorf("decode Shelly %s: %w", method, err)
	}
	if rpcError.Code != nil {
		return fmt.Errorf("fetch Shelly %s: RPC error code %d", method, *rpcError.Code)
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode Shelly %s: %w", method, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("decode Shelly %s: multiple JSON values", method)
		}
		return fmt.Errorf("decode Shelly %s: trailing data: %w", method, err)
	}
	return nil
}
