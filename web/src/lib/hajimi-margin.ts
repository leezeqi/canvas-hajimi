import { HAJIMI_MICRO_PER_USD } from "@/lib/hajimi-bridge";
import type { HajimiMarginDay, HajimiMarginHajimiStatus, HajimiMarginReport } from "@/services/api/hajimi-bridge";

/** 报表默认按北京时间（UTC+8）切日；另提供 UTC 方便和上游账单对照。 */
export const HAJIMI_MARGIN_OFFSETS = [
    { value: 480, label: "北京时间" },
    { value: 0, label: "UTC" },
] as const;

export const HAJIMI_MARGIN_MAX_DAYS = 92;

export type HajimiMarginPreset = "7d" | "30d" | "thisMonth" | "lastMonth";

export const HAJIMI_MARGIN_PRESETS: { value: HajimiMarginPreset; label: string }[] = [
    { value: "7d", label: "近 7 天" },
    { value: "30d", label: "近 30 天" },
    { value: "thisMonth", label: "本月" },
    { value: "lastMonth", label: "上月" },
];

const pad = (value: number) => String(value).padStart(2, "0");

/** 把某个时刻换算成"该 UTC 偏移下的日历日期"。 */
export function hajimiMarginToday(now: Date, offsetMinutes: number) {
    const shifted = new Date(now.getTime() + offsetMinutes * 60_000);
    return { year: shifted.getUTCFullYear(), month: shifted.getUTCMonth(), day: shifted.getUTCDate() };
}

function formatYMD(year: number, month: number, day: number) {
    const date = new Date(Date.UTC(year, month, day));
    return `${date.getUTCFullYear()}-${pad(date.getUTCMonth() + 1)}-${pad(date.getUTCDate())}`;
}

export function hajimiMarginPresetRange(preset: HajimiMarginPreset, now: Date, offsetMinutes: number) {
    const { year, month, day } = hajimiMarginToday(now, offsetMinutes);
    switch (preset) {
        case "30d":
            return { from: formatYMD(year, month, day - 29), to: formatYMD(year, month, day) };
        case "thisMonth":
            return { from: formatYMD(year, month, 1), to: formatYMD(year, month, day) };
        case "lastMonth":
            return { from: formatYMD(year, month - 1, 1), to: formatYMD(year, month, 0) };
        default:
            return { from: formatYMD(year, month, day - 6), to: formatYMD(year, month, day) };
    }
}

/** 毛利率（百分比）；收入为 0 或利润未知时返回 null。 */
export function hajimiMarginRate(profitMicro: number | null | undefined, revenueMicro: number) {
    if (profitMicro === null || profitMicro === undefined || !Number.isFinite(profitMicro) || revenueMicro <= 0) return null;
    return (profitMicro * 100) / revenueMicro;
}

export function formatHajimiMarginRate(rate: number | null) {
    return rate === null ? "--" : `${rate.toFixed(1)}%`;
}

export function hajimiMarginTone(profitMicro: number | null | undefined): "success" | "error" | "neutral" {
    if (profitMicro === null || profitMicro === undefined) return "neutral";
    return profitMicro < 0 ? "error" : "success";
}

export function hajimiMarginStatusLabel(status: HajimiMarginHajimiStatus) {
    switch (status) {
        case "ok":
            return "已读取";
        case "not_configured":
            return "未配置成本范围";
        case "disabled":
            return "互通未启用";
        default:
            return "读取失败";
    }
}

export function formatHajimiMarginOffset(offsetMinutes: number) {
    const known = HAJIMI_MARGIN_OFFSETS.find((item) => item.value === offsetMinutes);
    if (known) return known.label;
    const sign = offsetMinutes < 0 ? "-" : "+";
    const abs = Math.abs(offsetMinutes);
    return `UTC${sign}${pad(Math.floor(abs / 60))}:${pad(abs % 60)}`;
}

const csvUsd = (micro: number | null) => (micro === null ? "" : (micro / HAJIMI_MICRO_PER_USD).toFixed(6));

function csvCell(value: string | number) {
    const text = String(value);
    return /[",\r\n]/.test(text) ? `"${text.replace(/"/g, '""')}"` : text;
}

export const HAJIMI_MARGIN_CSV_HEADER = [
    "日期",
    "hajimi 渠道订单数",
    "hajimi 渠道收入(USD)",
    "hajimi 请求数",
    "hajimi 扣费(USD)",
    "毛利(USD)",
    "毛利率",
    "hajimi 上游成本(USD)",
    "合并毛利(USD)",
    "其他渠道订单数",
    "其他渠道收入(USD)",
    "其他渠道预估成本(USD)",
    "其他渠道未配置成本订单数",
];

function csvRow(label: string, day: HajimiMarginDay) {
    const rate = hajimiMarginRate(day.grossProfitMicro, day.hajimiRevenueMicro);
    return [
        label,
        day.hajimiOrders,
        csvUsd(day.hajimiRevenueMicro),
        day.hajimiRequests ?? "",
        csvUsd(day.hajimiChargedMicro),
        csvUsd(day.grossProfitMicro),
        rate === null ? "" : `${rate.toFixed(2)}%`,
        csvUsd(day.hajimiUpstreamCostMicro),
        csvUsd(day.combinedProfitMicro),
        day.otherOrders,
        csvUsd(day.otherRevenueMicro),
        csvUsd(day.otherEstimatedCostMicro),
        day.otherUncostedOrders,
    ]
        .map(csvCell)
        .join(",");
}

/** 逐日明细 + 合计行；带 BOM，Excel 打开不乱码。 */
export function hajimiMarginCsv(report: HajimiMarginReport) {
    const lines = [HAJIMI_MARGIN_CSV_HEADER.map(csvCell).join(","), ...report.days.map((day) => csvRow(day.date, day)), csvRow("合计", report.totals)];
    return `﻿${lines.join("\r\n")}\r\n`;
}

export function hajimiMarginCsvFileName(report: Pick<HajimiMarginReport, "from" | "to">) {
    return `yingce-margin-${report.from}_${report.to}.csv`;
}
