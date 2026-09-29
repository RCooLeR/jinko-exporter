export const GRID_BUY_RATE_UAH_PER_KWH = 4.32;
export const GREEN_TARIFF_GROSS_RATE_UAH_PER_KWH = 6.7991;
export const GREEN_TARIFF_PERSONAL_INCOME_TAX_RATE = 0.18;
export const GREEN_TARIFF_MILITARY_LEVY_RATE = 0.05;
export const GREEN_TARIFF_TOTAL_TAX_RATE =
  GREEN_TARIFF_PERSONAL_INCOME_TAX_RATE + GREEN_TARIFF_MILITARY_LEVY_RATE;
export const GREEN_TARIFF_NET_RATE_UAH_PER_KWH =
  GREEN_TARIFF_GROSS_RATE_UAH_PER_KWH * (1 - GREEN_TARIFF_TOTAL_TAX_RATE);

export const calculateDailyGridBalanceUAH = (
  buyTodayKwh: number | null,
  sellTodayKwh: number | null
): number | null => {
  if (!Number.isFinite(buyTodayKwh) || !Number.isFinite(sellTodayKwh)) return null;

  const netExportKwh = Number(sellTodayKwh) - Number(buyTodayKwh);
  if (netExportKwh > 0) return netExportKwh * GREEN_TARIFF_NET_RATE_UAH_PER_KWH;
  if (netExportKwh < 0) return netExportKwh * GRID_BUY_RATE_UAH_PER_KWH;
  return 0;
};
