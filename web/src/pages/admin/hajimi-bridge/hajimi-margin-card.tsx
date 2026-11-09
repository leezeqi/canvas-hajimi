import { useQuery } from "@tanstack/react-query";
import { Alert, Button, DatePicker, Segmented, Select, Tabs } from "antd";
import dayjs, { type Dayjs } from "dayjs";
import { TrendingUp } from "lucide-react";
import { useMemo, useState } from "react";

import { formatHajimiUsd } from "@/lib/hajimi-bridge";
import {
    formatHajimiMarginOffset,
    formatHajimiMarginRate,
    HAJIMI_MARGIN_MAX_DAYS,
    HAJIMI_MARGIN_OFFSETS,
    HAJIMI_MARGIN_PRESETS,
    hajimiMarginCsv,
    hajimiMarginCsvFileName,
    hajimiMarginPresetRange,
    hajimiMarginRate,
    hajimiMarginStatusLabel,
    hajimiMarginTone,
    type HajimiMarginPreset,
} from "@/lib/hajimi-margin";
import { getAdminHajimiBridgeMargin, type HajimiMarginChannel, type HajimiMarginDay, type HajimiMarginModel, type HajimiMarginReport } from "@/services/api/hajimi-bridge";
import { AdminDataTable, AdminExportButton, AdminStatTile, AdminStatusBadge, AdminTableEmpty, SettingsSectionCard } from "../components/admin-ui";

type CostKeyRow = HajimiMarginReport["hajimi"]["keys"][number];
type CostModelRow = HajimiMarginReport["hajimi"]["models"][number];

const DATE_FORMAT = "YYYY-MM-DD";

function Money({ micro, tone }: { micro: number | null | undefined; tone?: boolean }) {
    const negative = tone && typeof micro === "number" && micro < 0;
    return <span className={negative ? "tabular-nums text-status-error" : "tabular-nums"}>{formatHajimiUsd(micro)}</span>;
}

const count = (value: number | null | undefined) => (value === null || value === undefined ? "--" : value.toLocaleString());

/** hajimi 互通毛利报表：影策已结算订单实收 vs hajimi 成本范围的实际扣费，逐日对比。 */
export function HajimiMarginCard() {
    const [offset, setOffset] = useState<number>(HAJIMI_MARGIN_OFFSETS[0].value);
    const [preset, setPreset] = useState<HajimiMarginPreset | undefined>("7d");
    const [range, setRange] = useState(() => hajimiMarginPresetRange("7d", new Date(), HAJIMI_MARGIN_OFFSETS[0].value));
    const query = { from: range.from, to: range.to, utcOffsetMinutes: offset };
    const margin = useQuery({
        queryKey: ["admin", "hajimi-bridge", "margin", query] as const,
        queryFn: ({ signal }) => getAdminHajimiBridgeMargin(query, signal),
        refetchOnWindowFocus: false,
    });
    const report = margin.data;

    const applyPreset = (next: HajimiMarginPreset, nextOffset = offset) => {
        setPreset(next);
        setRange(hajimiMarginPresetRange(next, new Date(), nextOffset));
    };

    const pickerValue: [Dayjs, Dayjs] = [dayjs(range.from, DATE_FORMAT), dayjs(range.to, DATE_FORMAT)];

    return (
        <SettingsSectionCard
            icon={<TrendingUp className="size-4" />}
            title="毛利报表"
            description="影策已结算订单的实收积分（1 积分 = 1 美元）对比 hajimi 对运营 key 的实际扣费，按下单日期逐日汇总。"
            layout="stacked"
            status={report ? { label: `hajimi 成本：${hajimiMarginStatusLabel(report.hajimi.status)}`, color: report.hajimi.status === "ok" ? "success" : report.hajimi.status === "unavailable" ? "error" : "warning" } : undefined}
        >
            {/* minmax(0,1fr)：让宽表格在自身容器里横向滚动，而不是把整张卡片撑宽。 */}
            <div className="grid min-w-0 grid-cols-[minmax(0,1fr)] gap-3 px-3 py-3">
                <div className="flex flex-wrap items-center gap-2" role="group" aria-label="报表范围">
                    <Segmented<HajimiMarginPreset | "custom">
                        size="small"
                        value={preset ?? "custom"}
                        options={[...HAJIMI_MARGIN_PRESETS, ...(preset ? [] : [{ value: "custom" as const, label: "自定义" }])]}
                        onChange={(value) => {
                            if (value !== "custom") applyPreset(value);
                        }}
                    />
                    <DatePicker.RangePicker
                        size="small"
                        allowClear={false}
                        value={pickerValue}
                        disabledDate={(current, info) => {
                            const anchor = info?.from;
                            return Boolean(anchor && Math.abs(current.diff(anchor, "day")) >= HAJIMI_MARGIN_MAX_DAYS);
                        }}
                        onChange={(value) => {
                            if (value?.[0] && value?.[1]) {
                                setPreset(undefined);
                                setRange({ from: value[0].format(DATE_FORMAT), to: value[1].format(DATE_FORMAT) });
                            }
                        }}
                        aria-label="日期范围"
                    />
                    <Select
                        size="small"
                        value={offset}
                        aria-label="切日时区"
                        options={HAJIMI_MARGIN_OFFSETS.map((item) => ({ value: item.value, label: `按${item.label}切日` }))}
                        onChange={(value) => {
                            setOffset(value);
                            if (preset) applyPreset(preset, value);
                        }}
                        style={{ minWidth: 132 }}
                    />
                    <span className="flex-1" />
                    <Button size="small" loading={margin.isFetching} onClick={() => void margin.refetch()}>
                        刷新
                    </Button>
                    <AdminExportButton
                        size="small"
                        label="导出 CSV"
                        disabled={!report}
                        exportFile={() => new Blob([report ? hajimiMarginCsv(report) : ""], { type: "text/csv;charset=utf-8" })}
                        fileName={() => (report ? hajimiMarginCsvFileName(report) : "yingce-margin.csv")}
                    />
                </div>

                {margin.error ? (
                    <Alert
                        type="error"
                        showIcon
                        message={margin.error instanceof Error ? margin.error.message : "读取毛利报表失败"}
                        action={
                            <Button size="small" onClick={() => void margin.refetch()}>
                                重试
                            </Button>
                        }
                    />
                ) : null}
                {report && report.hajimi.status !== "ok" ? <Alert type={report.hajimi.status === "unavailable" ? "error" : "warning"} showIcon message={report.hajimi.message} description="影策收入照常统计；hajimi 扣费、毛利等列显示为 --。" /> : null}

                <HajimiMarginKpis report={report} />

                {report && report.openOrders > 0 ? (
                    <p className="text-xs text-foreground/55">
                        区间内另有 {report.openOrders.toLocaleString()} 笔订单尚未结算（预留 {formatHajimiUsd(report.openReservedMicro)}），结算后才计入收入；它们在 hajimi 的用量可能已经计入扣费。
                    </p>
                ) : null}

                <HajimiMarginDailyTable report={report} loading={margin.isPending} />

                <Tabs
                    size="small"
                    items={[
                        { key: "models", label: "按模型（影策收入）", children: <HajimiMarginModelTable rows={report?.models ?? []} loading={margin.isPending} /> },
                        { key: "keys", label: "按 hajimi key（成本）", children: <HajimiMarginKeyTable report={report} loading={margin.isPending} /> },
                        { key: "hajimi-models", label: "按 hajimi 模型（成本）", children: <HajimiMarginCostModelTable rows={report?.hajimi.models ?? []} loading={margin.isPending} /> },
                        { key: "channels", label: "渠道识别", children: <HajimiMarginChannelTable report={report} loading={margin.isPending} /> },
                    ]}
                />
                <p className="text-xs text-foreground/55">
                    口径：收入按订单实际扣除的积分计（含签到、兑换码等赠送积分的消费）；"hajimi 扣费"是 hajimi 对成本范围内 key 的实际计费；"合并毛利"再扣除 hajimi 后台"成本"列（账号倍率 × 标准价），用于两边合起来看。订单按下单时间归日，hajimi
                    按请求完成时间归日，跨零点的请求可能差一天。渠道是否算"走 hajimi"按渠道当前地址判断，改过渠道地址会影响历史数据。
                    {report ? ` 当前按${formatHajimiMarginOffset(report.utcOffsetMinutes)}切日。` : ""}
                </p>
            </div>
        </SettingsSectionCard>
    );
}

function HajimiMarginKpis({ report }: { report?: HajimiMarginReport }) {
    const totals = report?.totals;
    const grossRate = totals ? hajimiMarginRate(totals.grossProfitMicro, totals.hajimiRevenueMicro) : null;
    const combinedRate = totals ? hajimiMarginRate(totals.combinedProfitMicro, totals.hajimiRevenueMicro) : null;
    const accounts = report?.hajimi.accounts ?? [];
    return (
        <div className="grid min-w-0 gap-3">
            <div className="grid overflow-hidden rounded-md border border-border sm:grid-cols-2 xl:grid-cols-4">
                <AdminStatTile label="hajimi 渠道收入" value={totals ? formatHajimiUsd(totals.hajimiRevenueMicro) : "--"} detail={totals ? `${totals.hajimiOrders.toLocaleString()} 笔已结算订单` : undefined} />
                <AdminStatTile
                    label="hajimi 扣费"
                    value={totals ? formatHajimiUsd(totals.hajimiChargedMicro) : "--"}
                    detail={totals?.hajimiRequests !== null && totals?.hajimiRequests !== undefined ? `${totals.hajimiRequests.toLocaleString()} 次请求` : undefined}
                />
                <AdminStatTile
                    label="毛利"
                    value={totals ? formatHajimiUsd(totals.grossProfitMicro) : "--"}
                    trend={grossRate !== null ? { value: `毛利率 ${formatHajimiMarginRate(grossRate)}`, tone: hajimiMarginTone(totals?.grossProfitMicro) } : undefined}
                    detail="收入 − hajimi 扣费"
                />
                <AdminStatTile
                    label="合并毛利"
                    value={totals ? formatHajimiUsd(totals.combinedProfitMicro) : "--"}
                    trend={combinedRate !== null ? { value: formatHajimiMarginRate(combinedRate), tone: hajimiMarginTone(totals?.combinedProfitMicro) } : undefined}
                    detail={totals?.hajimiUpstreamCostMicro !== null && totals?.hajimiUpstreamCostMicro !== undefined ? `扣除 hajimi 上游成本 ${formatHajimiUsd(totals.hajimiUpstreamCostMicro)}` : "收入 − hajimi 上游成本"}
                />
            </div>
            <div className="grid overflow-hidden rounded-md border border-border sm:grid-cols-2 xl:grid-cols-4">
                <AdminStatTile label="其他渠道收入" value={totals ? formatHajimiUsd(totals.otherRevenueMicro) : "--"} detail={totals ? `${totals.otherOrders.toLocaleString()} 笔，不经过 hajimi` : undefined} />
                <AdminStatTile
                    label="其他渠道预估成本"
                    value={totals ? formatHajimiUsd(totals.otherEstimatedCostMicro) : "--"}
                    detail={totals ? (totals.otherUncostedOrders > 0 ? `${totals.otherUncostedOrders.toLocaleString()} 笔未配置成本价，未计入` : "按渠道配置的成本价估算") : undefined}
                />
                <AdminStatTile label="全站收入" value={totals ? formatHajimiUsd(totals.hajimiRevenueMicro + totals.otherRevenueMicro) : "--"} detail="hajimi 渠道 + 其他渠道" />
                <AdminStatTile
                    label="成本账号余额"
                    value={accounts.length ? formatHajimiUsd(accounts.reduce((sum, item) => sum + item.balanceMicro, 0)) : "--"}
                    detail={accounts.length ? `hajimi 用户 ${accounts.map((item) => `#${item.userId}`).join("、")}` : "仅在 hajimi 配置了成本账号（YINGCE_COST_USER_IDS）时显示"}
                />
            </div>
        </div>
    );
}

function HajimiMarginDailyTable({ report, loading }: { report?: HajimiMarginReport; loading: boolean }) {
    const rows = useMemo(() => [...(report?.days ?? [])].reverse(), [report]);
    const totals = report?.totals;
    return (
        <AdminDataTable<HajimiMarginDay>
            empty={<AdminTableEmpty title="没有数据" description="所选范围内没有订单。" />}
            table={{
                rowKey: "date",
                size: "small",
                loading,
                dataSource: rows,
                pagination: false,
                scroll: { x: 1180, y: 420 },
                columns: [
                    { key: "date", title: "日期", dataIndex: "date", width: 110, fixed: "left", render: (value: string) => <span className="tabular-nums">{value}</span> },
                    { key: "hajimiOrders", title: "订单", dataIndex: "hajimiOrders", width: 70, align: "right", render: (value: number) => count(value) },
                    { key: "revenue", title: "hajimi 渠道收入", dataIndex: "hajimiRevenueMicro", width: 130, align: "right", render: (value: number) => <Money micro={value} /> },
                    { key: "requests", title: "hajimi 请求", dataIndex: "hajimiRequests", width: 100, align: "right", render: (value: number | null) => count(value) },
                    { key: "charged", title: "hajimi 扣费", dataIndex: "hajimiChargedMicro", width: 120, align: "right", render: (value: number | null) => <Money micro={value} /> },
                    { key: "gross", title: "毛利", dataIndex: "grossProfitMicro", width: 120, align: "right", render: (value: number | null) => <Money micro={value} tone /> },
                    { key: "rate", title: "毛利率", width: 80, align: "right", render: (_, row) => formatHajimiMarginRate(hajimiMarginRate(row.grossProfitMicro, row.hajimiRevenueMicro)) },
                    { key: "upstream", title: "hajimi 上游成本", dataIndex: "hajimiUpstreamCostMicro", width: 130, align: "right", render: (value: number | null) => <Money micro={value} /> },
                    { key: "combined", title: "合并毛利", dataIndex: "combinedProfitMicro", width: 120, align: "right", render: (value: number | null) => <Money micro={value} tone /> },
                    {
                        key: "otherRevenue",
                        title: "其他渠道收入",
                        dataIndex: "otherRevenueMicro",
                        width: 120,
                        align: "right",
                        render: (value: number, row) => (
                            <span title={`${row.otherOrders} 笔`}>
                                <Money micro={value} />
                            </span>
                        ),
                    },
                    {
                        key: "otherCost",
                        title: "其他渠道预估成本",
                        dataIndex: "otherEstimatedCostMicro",
                        width: 140,
                        align: "right",
                        render: (value: number, row) => (
                            <span title={row.otherUncostedOrders ? `${row.otherUncostedOrders} 笔未配置成本价` : undefined}>
                                <Money micro={value} />
                                {row.otherUncostedOrders ? <span className="text-foreground/45"> *</span> : null}
                            </span>
                        ),
                    },
                ],
                summary: () =>
                    totals ? (
                        <TableSummaryRow
                            cells={[
                                "合计",
                                count(totals.hajimiOrders),
                                formatHajimiUsd(totals.hajimiRevenueMicro),
                                count(totals.hajimiRequests),
                                formatHajimiUsd(totals.hajimiChargedMicro),
                                formatHajimiUsd(totals.grossProfitMicro),
                                formatHajimiMarginRate(hajimiMarginRate(totals.grossProfitMicro, totals.hajimiRevenueMicro)),
                                formatHajimiUsd(totals.hajimiUpstreamCostMicro),
                                formatHajimiUsd(totals.combinedProfitMicro),
                                formatHajimiUsd(totals.otherRevenueMicro),
                                formatHajimiUsd(totals.otherEstimatedCostMicro),
                            ]}
                        />
                    ) : null,
            }}
        />
    );
}

function TableSummaryRow({ cells }: { cells: string[] }) {
    return (
        <tr className="ant-table-summary-row">
            {cells.map((cell, index) => (
                <td key={index} className={index === 0 ? "ant-table-cell ant-table-cell-fix-left font-medium" : "ant-table-cell text-right font-medium tabular-nums"} style={index === 0 ? { position: "sticky", left: 0, zIndex: 2 } : undefined}>
                    {cell}
                </td>
            ))}
        </tr>
    );
}

function HajimiMarginModelTable({ rows, loading }: { rows: HajimiMarginModel[]; loading: boolean }) {
    return (
        <AdminDataTable<HajimiMarginModel>
            empty={<AdminTableEmpty title="没有已结算订单" />}
            table={{
                rowKey: (row) => `${row.viaHajimi ? "h" : "o"}:${row.model}`,
                size: "small",
                loading,
                dataSource: rows,
                pagination: rows.length > 20 ? { pageSize: 20, size: "small" } : false,
                scroll: { x: 720 },
                columns: [
                    { key: "model", title: "影策模型", dataIndex: "model", ellipsis: true },
                    { key: "via", title: "渠道", dataIndex: "viaHajimi", width: 110, render: (value: boolean) => <AdminStatusBadge label={value ? "hajimi" : "其他上游"} tone={value ? "info" : "neutral"} /> },
                    { key: "orders", title: "订单", dataIndex: "orders", width: 80, align: "right", render: (value: number) => count(value) },
                    { key: "revenue", title: "收入", dataIndex: "revenueMicro", width: 130, align: "right", render: (value: number) => <Money micro={value} /> },
                    {
                        key: "cost",
                        title: "预估成本（影策配置）",
                        dataIndex: "estimatedCostMicro",
                        width: 170,
                        align: "right",
                        render: (value: number, row) =>
                            row.uncostedOrders === row.orders ? (
                                <span className="text-foreground/45">未配置</span>
                            ) : (
                                <span title={row.uncostedOrders ? `${row.uncostedOrders} 笔未配置成本价` : undefined}>
                                    <Money micro={value} />
                                    {row.uncostedOrders ? " *" : ""}
                                </span>
                            ),
                    },
                ],
            }}
        />
    );
}

function HajimiMarginKeyTable({ report, loading }: { report?: HajimiMarginReport; loading: boolean }) {
    const keys = report?.hajimi.keys ?? [];
    return (
        <div className="grid min-w-0 grid-cols-[minmax(0,1fr)] gap-2">
            {report ? (
                <p className="text-xs text-foreground/55">
                    hajimi 成本范围：用户 {report.hajimi.scopeUserIds.length ? report.hajimi.scopeUserIds.map((id) => `#${id}`).join("、") : "无"}；key{" "}
                    {report.hajimi.scopeApiKeyIds.length ? report.hajimi.scopeApiKeyIds.map((id) => `#${id}`).join("、") : "无"}。在 hajimi 的 YINGCE_COST_USER_IDS / YINGCE_COST_API_KEY_IDS 中配置。
                </p>
            ) : null}
            <AdminDataTable<CostKeyRow>
                empty={<AdminTableEmpty title="没有 hajimi 用量" description={report?.hajimi.status === "ok" ? "成本范围内的 key 在所选区间没有请求。" : report?.hajimi.message} />}
                table={{
                    rowKey: "apiKeyId",
                    size: "small",
                    loading,
                    dataSource: keys,
                    pagination: false,
                    scroll: { x: 720 },
                    columns: [
                        { key: "name", title: "hajimi key", dataIndex: "name", ellipsis: true, render: (value: string, row) => `${value || "（未命名）"} · #${row.apiKeyId}` },
                        { key: "user", title: "所属用户", dataIndex: "userId", width: 110, render: (value: number) => (value ? `#${value}` : "--") },
                        { key: "requests", title: "请求", dataIndex: "requests", width: 90, align: "right", render: (value: number) => count(value) },
                        { key: "charged", title: "hajimi 扣费", dataIndex: "chargedMicro", width: 130, align: "right", render: (value: number) => <Money micro={value} /> },
                        { key: "upstream", title: "hajimi 上游成本", dataIndex: "upstreamCostMicro", width: 140, align: "right", render: (value: number) => <Money micro={value} /> },
                    ],
                }}
            />
        </div>
    );
}

function HajimiMarginCostModelTable({ rows, loading }: { rows: CostModelRow[]; loading: boolean }) {
    return (
        <AdminDataTable<CostModelRow>
            empty={<AdminTableEmpty title="没有 hajimi 用量" />}
            table={{
                rowKey: "model",
                size: "small",
                loading,
                dataSource: rows,
                pagination: rows.length > 20 ? { pageSize: 20, size: "small" } : false,
                scroll: { x: 640 },
                columns: [
                    { key: "model", title: "hajimi 计费模型", dataIndex: "model", ellipsis: true },
                    { key: "requests", title: "请求", dataIndex: "requests", width: 90, align: "right", render: (value: number) => count(value) },
                    { key: "charged", title: "hajimi 扣费", dataIndex: "chargedMicro", width: 130, align: "right", render: (value: number) => <Money micro={value} /> },
                    { key: "upstream", title: "hajimi 上游成本", dataIndex: "upstreamCostMicro", width: 140, align: "right", render: (value: number) => <Money micro={value} /> },
                ],
            }}
        />
    );
}

function HajimiMarginChannelTable({ report, loading }: { report?: HajimiMarginReport; loading: boolean }) {
    const rows = report?.channels ?? [];
    return (
        <div className="grid min-w-0 grid-cols-[minmax(0,1fr)] gap-2">
            {report ? (
                <p className="text-xs text-foreground/55">
                    系统渠道的 Base URL 主机名匹配 {report.channelRule.length ? report.channelRule.join("、") : "（未配置）"} 时算作 hajimi 渠道（可用影策后端环境变量 HAJIMI_BRIDGE_COST_CHANNEL_HOSTS 指定）。用户自带 key
                    的渠道不计入。归类按渠道当前的地址判断：渠道改过地址后，它以前的订单也会按新地址归类，历史毛利会随之变化。
                </p>
            ) : null}
            <AdminDataTable<HajimiMarginChannel>
                empty={<AdminTableEmpty title="没有渠道" />}
                table={{
                    rowKey: "id",
                    size: "small",
                    loading,
                    dataSource: rows,
                    pagination: false,
                    scroll: { x: 760 },
                    columns: [
                        { key: "name", title: "渠道", dataIndex: "name", ellipsis: true, render: (value: string, row) => (row.deleted ? `${value}（已删除）` : value) },
                        { key: "host", title: "主机名", dataIndex: "host", width: 200, ellipsis: true, render: (value: string) => <code className="text-xs">{value || "--"}</code> },
                        { key: "via", title: "归类", dataIndex: "viaHajimi", width: 110, render: (value: boolean, row) => <AdminStatusBadge label={value ? "hajimi" : row.system ? "其他上游" : "用户渠道"} tone={value ? "info" : "neutral"} /> },
                        { key: "orders", title: "订单", dataIndex: "orders", width: 80, align: "right", render: (value: number) => count(value) },
                        { key: "revenue", title: "收入", dataIndex: "revenueMicro", width: 130, align: "right", render: (value: number) => <Money micro={value} /> },
                    ],
                }}
            />
        </div>
    );
}
