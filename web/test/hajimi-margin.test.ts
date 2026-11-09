import { describe, expect, test } from "bun:test";

import { formatHajimiMarginOffset, formatHajimiMarginRate, hajimiMarginCsv, hajimiMarginCsvFileName, hajimiMarginPresetRange, hajimiMarginRate, hajimiMarginStatusLabel, hajimiMarginTone } from "../src/lib/hajimi-margin";
import type { HajimiMarginDay, HajimiMarginReport } from "../src/services/api/hajimi-bridge";

const day = (overrides: Partial<HajimiMarginDay>): HajimiMarginDay => ({
    date: "",
    hajimiOrders: 0,
    hajimiRevenueMicro: 0,
    hajimiRequests: null,
    hajimiChargedMicro: null,
    hajimiUpstreamCostMicro: null,
    grossProfitMicro: null,
    combinedProfitMicro: null,
    otherOrders: 0,
    otherRevenueMicro: 0,
    otherEstimatedCostMicro: 0,
    otherUncostedOrders: 0,
    ...overrides,
});

describe("hajimi margin presets", () => {
    // 2026-11-08 18:30 at UTC+8 = 10:30 UTC; 2026-11-08 01:00 at UTC+8 is still 11-07 in UTC.
    const now = new Date(Date.UTC(2026, 10, 8, 10, 30));
    test("ranges follow the report offset", () => {
        expect(hajimiMarginPresetRange("7d", now, 480)).toEqual({ from: "2026-11-02", to: "2026-11-08" });
        expect(hajimiMarginPresetRange("30d", now, 480)).toEqual({ from: "2026-10-10", to: "2026-11-08" });
        expect(hajimiMarginPresetRange("thisMonth", now, 480)).toEqual({ from: "2026-11-01", to: "2026-11-08" });
        expect(hajimiMarginPresetRange("lastMonth", now, 480)).toEqual({ from: "2026-10-01", to: "2026-10-31" });
        const earlyBeijing = new Date(Date.UTC(2026, 10, 7, 17, 0));
        expect(hajimiMarginPresetRange("7d", earlyBeijing, 480).to).toBe("2026-11-08");
        expect(hajimiMarginPresetRange("7d", earlyBeijing, 0).to).toBe("2026-11-07");
        expect(hajimiMarginPresetRange("lastMonth", new Date(Date.UTC(2026, 0, 15)), 0)).toEqual({ from: "2025-12-01", to: "2025-12-31" });
    });
});

describe("hajimi margin formatting", () => {
    test("rate is null without revenue or profit", () => {
        expect(hajimiMarginRate(250_000, 1_000_000)).toBe(25);
        expect(hajimiMarginRate(-500_000, 1_000_000)).toBe(-50);
        expect(hajimiMarginRate(null, 1_000_000)).toBeNull();
        expect(hajimiMarginRate(100, 0)).toBeNull();
        expect(formatHajimiMarginRate(12.345)).toBe("12.3%");
        expect(formatHajimiMarginRate(null)).toBe("--");
        expect(hajimiMarginTone(-1)).toBe("error");
        expect(hajimiMarginTone(0)).toBe("success");
        expect(hajimiMarginTone(null)).toBe("neutral");
    });
    test("labels", () => {
        expect(hajimiMarginStatusLabel("ok")).toBe("已读取");
        expect(hajimiMarginStatusLabel("not_configured")).toBe("未配置成本范围");
        expect(hajimiMarginStatusLabel("unavailable")).toBe("读取失败");
        expect(formatHajimiMarginOffset(480)).toBe("北京时间");
        expect(formatHajimiMarginOffset(0)).toBe("UTC");
        expect(formatHajimiMarginOffset(-150)).toBe("UTC-02:30");
    });
});

describe("hajimi margin csv", () => {
    const report = {
        from: "2026-11-01",
        to: "2026-11-02",
        days: [
            day({ date: "2026-11-01", hajimiOrders: 2, hajimiRevenueMicro: 2_000_000, hajimiRequests: 3, hajimiChargedMicro: 1_500_000, hajimiUpstreamCostMicro: 600_000, grossProfitMicro: 500_000, combinedProfitMicro: 1_400_000 }),
            day({ date: "2026-11-02", otherOrders: 1, otherRevenueMicro: 1_234_567, otherUncostedOrders: 1 }),
        ],
        totals: day({
            hajimiOrders: 2,
            hajimiRevenueMicro: 2_000_000,
            hajimiRequests: 3,
            hajimiChargedMicro: 1_500_000,
            hajimiUpstreamCostMicro: 600_000,
            grossProfitMicro: 500_000,
            combinedProfitMicro: 1_400_000,
            otherOrders: 1,
            otherRevenueMicro: 1_234_567,
            otherUncostedOrders: 1,
        }),
    } as unknown as HajimiMarginReport;

    test("has BOM, header, daily rows and a total row", () => {
        const csv = hajimiMarginCsv(report);
        expect(csv.startsWith("﻿")).toBe(true);
        const lines = csv.slice(1).trimEnd().split("\r\n");
        expect(lines).toHaveLength(4);
        expect(lines[0].split(",")[0]).toBe("日期");
        expect(lines[1]).toBe("2026-11-01,2,2.000000,3,1.500000,0.500000,25.00%,0.600000,1.400000,0,0.000000,0.000000,0");
        // Unknown hajimi numbers stay empty instead of 0.
        expect(lines[2]).toBe("2026-11-02,0,0.000000,,,,,,,1,1.234567,0.000000,1");
        expect(lines[3].startsWith("合计,2,2.000000,3,1.500000,0.500000,25.00%")).toBe(true);
        expect(hajimiMarginCsvFileName(report)).toBe("yingce-margin-2026-11-01_2026-11-02.csv");
    });
});
