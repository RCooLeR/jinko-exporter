package jinko

import (
	"strings"

	"github.com/RCooLeR/jinko-exporter/internal/model"
)

type MetricDefinition struct {
	Key   string
	Group string
	Name  string
	Unit  string
}

var canonicalMetricDefinitions = []MetricDefinition{
	{Key: "INV_MOD1", Group: "basic", Name: "Inverter Type", Unit: ""},
	{Key: "Pr1", Group: "basic", Name: "Rated Power", Unit: "W"},
	{Key: "HV_V", Group: "basic", Name: "HV Voltage", Unit: "V"},
	{Key: "BN_V", Group: "basic", Name: "Bus-N Voltage", Unit: "V"},
	{Key: "PTCv1", Group: "version", Name: "Protocol Version", Unit: ""},
	{Key: "LBVN", Group: "version", Name: "Lithium Battery Version Number", Unit: ""},
	{Key: "L_B_V_N2", Group: "version", Name: "Lithium battery2 version number", Unit: ""},
	{Key: "A_B_F_V", Group: "version", Name: "Arc Board Firmware Version", Unit: ""},
	{Key: "ENG_VER", Group: "version", Name: "English Version", Unit: ""},
	{Key: "ESP_VER", Group: "version", Name: "Spanish Version", Unit: ""},
	{Key: "HUN_VER", Group: "version", Name: "Hungarian Version", Unit: ""},
	{Key: "DEU_VER", Group: "version", Name: "German Version", Unit: ""},
	{Key: "PO_VER", Group: "version", Name: "Polish Version", Unit: ""},
	{Key: "UK_VER", Group: "version", Name: "Ukrainian Version", Unit: ""},
	{Key: "CZ_VER", Group: "version", Name: "Czech Version", Unit: ""},
	{Key: "ITA_VER", Group: "version", Name: "Italian Version", Unit: ""},
	{Key: "L_B_V_N", Group: "version", Name: "Lithium battery version number", Unit: ""},
	{Key: "DV1", Group: "electric", Name: "DC Voltage PV1", Unit: "V"},
	{Key: "DV2", Group: "electric", Name: "DC Voltage PV2", Unit: "V"},
	{Key: "DV3", Group: "electric", Name: "DC Voltage PV3", Unit: "V"},
	{Key: "DV4", Group: "electric", Name: "DC Voltage PV4", Unit: "V"},
	{Key: "PV_D_P_G", Group: "electric", Name: "PV daily power generation (active)", Unit: "kWh"},
	{Key: "P_F1", Group: "electric", Name: "Power factor", Unit: ""},
	{Key: "A_V_MA", Group: "electric", Name: "AC Voltage Max", Unit: "V"},
	{Key: "A_V_MI", Group: "electric", Name: "AC Voltage Min", Unit: "V"},
	{Key: "O_P", Group: "electric", Name: "Output power", Unit: "kVA"},
	{Key: "DC1", Group: "electric", Name: "DC Current PV1", Unit: "A"},
	{Key: "DC2", Group: "electric", Name: "DC Current PV2", Unit: "A"},
	{Key: "DC3", Group: "electric", Name: "DC Current PV3", Unit: "A"},
	{Key: "DC4", Group: "electric", Name: "DC Current PV4", Unit: "A"},
	{Key: "DP1", Group: "electric", Name: "DC Power PV1", Unit: "W"},
	{Key: "DP2", Group: "electric", Name: "DC Power PV2", Unit: "W"},
	{Key: "DP3", Group: "electric", Name: "DC Power PV3", Unit: "W"},
	{Key: "DP4", Group: "electric", Name: "DC Power PV4", Unit: "W"},
	{Key: "AV1", Group: "electric", Name: "AC Voltage R/U/A", Unit: "V"},
	{Key: "AV2", Group: "electric", Name: "AC Voltage S/V/B", Unit: "V"},
	{Key: "AV3", Group: "electric", Name: "AC Voltage T/W/C", Unit: "V"},
	{Key: "AC1", Group: "electric", Name: "AC Current R/U/A", Unit: "A"},
	{Key: "AC2", Group: "electric", Name: "AC Current S/V/B", Unit: "A"},
	{Key: "AC3", Group: "electric", Name: "AC Current T/W/C", Unit: "A"},
	{Key: "A_Fo1", Group: "electric", Name: "AC Output Frequency R", Unit: "Hz"},
	{Key: "Et_ge0", Group: "electric", Name: "Cumulative Production (Active)", Unit: "kWh"},
	{Key: "Etdy_ge1", Group: "electric", Name: "Daily Production (Active)", Unit: "kWh"},
	{Key: "INV_O_P_L1", Group: "electric", Name: "Inverter Output Power L1", Unit: "W"},
	{Key: "INV_O_P_L2", Group: "electric", Name: "Inverter Output Power L2", Unit: "W"},
	{Key: "INV_O_P_L3", Group: "electric", Name: "Inverter Output Power L3", Unit: "W"},
	{Key: "INV_O_P_T", Group: "electric", Name: "Total Inverter Output Power", Unit: "W"},
	{Key: "AC_S_A", Group: "electric", Name: "AC_Solar phaseA", Unit: "W"},
	{Key: "AC_S_B", Group: "electric", Name: "AC_Solar phaseB", Unit: "W"},
	{Key: "AC_S_C", Group: "electric", Name: "AC_Solar phaseC", Unit: "W"},
	{Key: "S_P_T", Group: "electric", Name: "Total Solar Power", Unit: "W"},
	{Key: "G_V_L1", Group: "grid", Name: "Grid\u00a0Voltage\u00a0L1", Unit: "V"},
	{Key: "G_C_L1", Group: "grid", Name: "Grid\u00a0Current\u00a0L1", Unit: "A"},
	{Key: "G_P_L1", Group: "grid", Name: "Grid Power L1", Unit: "W"},
	{Key: "G_V_L2", Group: "grid", Name: "Grid\u00a0Voltage\u00a0L2", Unit: "V"},
	{Key: "G_C_L2", Group: "grid", Name: "Grid\u00a0Current\u00a0L2", Unit: "A"},
	{Key: "G_P_L2", Group: "grid", Name: "Grid Power L2", Unit: "W"},
	{Key: "G_V_L3", Group: "grid", Name: "Grid\u00a0Voltage\u00a0L3", Unit: "V"},
	{Key: "G_C_L3", Group: "grid", Name: "Grid\u00a0Current\u00a0L3", Unit: "A"},
	{Key: "G_P_L3", Group: "grid", Name: "Grid Power L3", Unit: "W"},
	{Key: "ST_PG1", Group: "grid", Name: "Grid Status", Unit: ""},
	{Key: "CT1_P_E", Group: "grid", Name: "External\u00a0CT1\u00a0Power", Unit: "W"},
	{Key: "CT2_P_E", Group: "grid", Name: "External\u00a0CT2\u00a0Power", Unit: "W"},
	{Key: "CT3_P_E", Group: "grid", Name: "External\u00a0CT3\u00a0Power", Unit: "W"},
	{Key: "CT_T_E", Group: "grid", Name: "Total\u00a0External\u00a0CT\u00a0Power", Unit: "W"},
	{Key: "PG_F1", Group: "grid", Name: "Grid Frequency", Unit: "Hz"},
	{Key: "PG_Pt1", Group: "grid", Name: "Total Grid Power", Unit: "W"},
	{Key: "G16", Group: "grid", Name: "Total Grid reactive Power", Unit: "Var"},
	{Key: "A_RP_PG", Group: "grid", Name: "A-Phase Reactive Power Of Power Grid", Unit: "Var"},
	{Key: "B_RP_PG", Group: "grid", Name: "B-Phase Reactive Power Of Power Grid", Unit: "Var"},
	{Key: "C_RP_PG", Group: "grid", Name: "C-Phase Reactive Power Of Power Grid", Unit: "Var"},
	{Key: "E_B_D", Group: "grid", Name: "Daily\u00a0Energy\u00a0Buy", Unit: "kWh"},
	{Key: "E_S_D", Group: "grid", Name: "Daily\u00a0energy\u00a0sell", Unit: "kWh"},
	{Key: "E_B_TO", Group: "grid", Name: "Total\u00a0Energy\u00a0Buy", Unit: "kWh"},
	{Key: "E_S_TO", Group: "grid", Name: "Total\u00a0Energy\u00a0Sell", Unit: "kWh"},
	{Key: "GS_A", Group: "grid", Name: "Internal L1 Power", Unit: "W"},
	{Key: "GS_B", Group: "grid", Name: "Internal L2 Power", Unit: "W"},
	{Key: "GS_C", Group: "grid", Name: "Internal L3 Power", Unit: "W"},
	{Key: "GS_T", Group: "grid", Name: "Internal Power", Unit: "W"},
	{Key: "A_RP_INV", Group: "grid", Name: "Inverter A-Phase Reactive Power", Unit: "Var"},
	{Key: "B_RP_INV", Group: "grid", Name: "Inverter B-Phase Reactive Power", Unit: "Var"},
	{Key: "C_RP_INV", Group: "grid", Name: "Inverter C-Phase Reactive Power", Unit: "Var"},
	{Key: "C_V_L1", Group: "consumption", Name: "Load Voltage  L1", Unit: "V"},
	{Key: "C_V_L2", Group: "consumption", Name: "Load Voltage  L2", Unit: "V"},
	{Key: "C_V_L3", Group: "consumption", Name: "Load Voltage  L3", Unit: "V"},
	{Key: "C_P_L1", Group: "consumption", Name: "Load  Power L1", Unit: "W"},
	{Key: "C_P_L2", Group: "consumption", Name: "Load  Power L2", Unit: "W"},
	{Key: "C_P_L3", Group: "consumption", Name: "Load  Power L3", Unit: "W"},
	{Key: "E_Puse_t1", Group: "consumption", Name: "Total Consumption Power", Unit: "W"},
	{Key: "E_Suse_t1", Group: "consumption", Name: "Total Consumption Apparent Power", Unit: "VA"},
	{Key: "Etdy_use1", Group: "consumption", Name: "Daily Consumption", Unit: "kWh"},
	{Key: "E_C_T", Group: "consumption", Name: "Total\u00a0Consumption", Unit: "kWh"},
	{Key: "L_F", Group: "consumption", Name: "Load Fequency", Unit: "Hz"},
	{Key: "LPP_A", Group: "consumption", Name: "Load phase power A", Unit: "W"},
	{Key: "LPP_B", Group: "consumption", Name: "Load phase power B", Unit: "W"},
	{Key: "LPP_C", Group: "consumption", Name: "Load phase power C", Unit: "W"},
	{Key: "B_ST1", Group: "battery", Name: "Battery Status", Unit: ""},
	{Key: "B_V1", Group: "battery", Name: "Battery Voltage", Unit: "V"},
	{Key: "B_C1", Group: "battery", Name: "Battery Current", Unit: "A"},
	{Key: "B_P1", Group: "battery", Name: "Battery Power", Unit: "W"},
	{Key: "B_left_cap1", Group: "battery", Name: "SoC", Unit: "%"},
	{Key: "t_cg_n1", Group: "battery", Name: "Total Charging Energy", Unit: "kWh"},
	{Key: "t_dcg_n1", Group: "battery", Name: "Total Discharging Energy", Unit: "kWh"},
	{Key: "Etdy_cg1", Group: "battery", Name: "Daily Charging Energy", Unit: "kWh"},
	{Key: "Etdy_dcg1", Group: "battery", Name: "Daily Discharging Energy", Unit: "kWh"},
	{Key: "B_TYP1", Group: "battery", Name: "Battery Type", Unit: ""},
	{Key: "Ba_NR", Group: "battery", Name: "Battery Number", Unit: ""},
	{Key: "BMS_B_V1", Group: "bms", Name: "BMS Voltage", Unit: "V"},
	{Key: "BMS_B_C1", Group: "bms", Name: "BMS Current", Unit: "A"},
	{Key: "BMST", Group: "bms", Name: "BMS Temperature", Unit: "\u2103"},
	{Key: "BMS_C_V", Group: "bms", Name: "BMS Charge Voltage", Unit: "V"},
	{Key: "BMS_D_V", Group: "bms", Name: "BMS Discharge Voltage", Unit: "V"},
	{Key: "BMS_C_C_L", Group: "bms", Name: "Charge Current Limit", Unit: "A"},
	{Key: "BMS_D_C_L", Group: "bms", Name: "Discharge Current Limit", Unit: "A"},
	{Key: "BMS_SOC", Group: "bms", Name: "BMS_SOC", Unit: "%"},
	{Key: "BMS_CC1", Group: "bms", Name: "BMS Charging Max Current", Unit: "A"},
	{Key: "BMS_DC1", Group: "bms", Name: "BMS DischargeMax Current", Unit: "A"},
	{Key: "Li_B_TP", Group: "bms", Name: "Li-bat type", Unit: ""},
	{Key: "Li_B_SOH", Group: "bms", Name: "Lithium battery SOH", Unit: "%"},
	{Key: "B_R_CAP", Group: "bms", Name: "Battery Rating Capacity", Unit: "AH"},
	{Key: "Li_B_TP2", Group: "bms2", Name: "Li-bat2 type", Unit: ""},
	{Key: "Li_B_SOH2", Group: "bms2", Name: "Lithium battery2 SOH", Unit: "%"},
	{Key: "B_R_CAP2", Group: "bms2", Name: "Battery2 Rating Capacity", Unit: "AH"},
	{Key: "B_T1", Group: "temperature", Name: "Temperature- Battery", Unit: "\u2103"},
	{Key: "T_DC", Group: "temperature", Name: "DC Temperature", Unit: "\u2103"},
	{Key: "AC_T", Group: "temperature", Name: "AC Temperature", Unit: "\u2103"},
	{Key: "AC", Group: "status", Name: "AC side relay status", Unit: ""},
	{Key: "L_B_A_F", Group: "alert", Name: "Lithium battery alarm flag", Unit: ""},
	{Key: "L_B_F_F", Group: "alert", Name: "Lithium battery fault flag", Unit: ""},
	{Key: "L_B_A_F2", Group: "alert", Name: "Lithium battery2 alarm flag", Unit: ""},
	{Key: "L_B_F_F2", Group: "alert", Name: "Lithium battery2 fault flag", Unit: ""},
	{Key: "GEN_P_L1", Group: "generator", Name: "Gen Power L1", Unit: "W"},
	{Key: "GEN_P_L2", Group: "generator", Name: "Gen Power L2", Unit: "W"},
	{Key: "GEN_P_L3", Group: "generator", Name: "Gen Power L3", Unit: "W"},
	{Key: "GEN_V_L1", Group: "generator", Name: "Gen Voltage L1", Unit: "V"},
	{Key: "GEN_V_L2", Group: "generator", Name: "Gen Voltage L2", Unit: "V"},
	{Key: "GEN_V_L3", Group: "generator", Name: "Gen Voltage L3", Unit: "V"},
	{Key: "R_T_D", Group: "generator", Name: "Gen\u00a0Daily\u00a0Run\u00a0Time", Unit: "h"},
	{Key: "EG_P_CT1", Group: "generator", Name: "Generator Active Power", Unit: "W"},
	{Key: "GEN_P_T", Group: "generator", Name: "Total Gen Power", Unit: "W"},
	{Key: "GEN_P_D", Group: "generator", Name: "Daily Production Generator", Unit: "kWh"},
	{Key: "GEN_P_TO", Group: "generator", Name: "Total Production Generator", Unit: "kWh"},
	{Key: "UPS_P", Group: "ups", Name: "UPS Load Power", Unit: "W"},
}

var (
	canonicalMetricByKey  = buildMetricIndex(func(def MetricDefinition) string { return strings.ToLower(def.Key) })
	canonicalMetricByName = buildMetricIndex(func(def MetricDefinition) string { return SanitizeKey(def.Name) })
)

func CanonicalizeMetric(metric model.Metric) (model.Metric, bool) {
	def, ok := LookupMetricDefinition(metric.Key, metric.Name)
	if !ok {
		return model.Metric{}, false
	}

	metric.Group = def.Group
	metric.Key = def.Key
	metric.Name = def.Name
	metric.Unit = normalizeUnit(def.Unit)
	return metric, true
}

func LookupMetricDefinition(key string, name string) (MetricDefinition, bool) {
	if def, ok := canonicalMetricByKey[strings.ToLower(strings.TrimSpace(key))]; ok {
		return def, true
	}
	if def, ok := canonicalMetricByName[SanitizeKey(name)]; ok {
		return def, true
	}
	return MetricDefinition{}, false
}

func buildMetricIndex(keyFunc func(MetricDefinition) string) map[string]MetricDefinition {
	index := make(map[string]MetricDefinition, len(canonicalMetricDefinitions))
	for _, def := range canonicalMetricDefinitions {
		key := strings.TrimSpace(keyFunc(def))
		if key == "" {
			continue
		}
		if _, ok := index[key]; !ok {
			index[key] = def
		}
	}
	return index
}
