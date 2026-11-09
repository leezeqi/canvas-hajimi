import { useQuery, useQueryClient } from "@tanstack/react-query";
import { App, Button } from "antd";
import { ArrowLeftRight, RefreshCw, Scale, ServerCog } from "lucide-react";
import { useState, type ReactNode } from "react";

import { formatHajimiUsd, formatRemoteSummaryValue, hajimiReasonMessage, hajimiTransferDirectionLabel, hajimiTransferSourceLabel, hajimiTransferStatusLabel, hajimiTransferStatusTone, remoteSummaryLabel } from "@/lib/hajimi-bridge";
import { getAdminHajimiBridgeOverview, retryAdminHajimiTransfer, type HajimiBridgeAdminOverview, type HajimiBridgeAttentionTransfer } from "@/services/api/hajimi-bridge";
import { AdminPageFrame } from "../components/admin-shell";
import { AdminDataTable, AdminStatTile, AdminStatusBadge, AdminTableEmpty, SettingsSectionCard } from "../components/admin-ui";
import { HajimiMarginCard } from "./hajimi-margin-card";

const overviewQueryKey = ["admin", "hajimi-bridge", "overview"] as const;

const formatTime = (value?: string | null) => (value ? new Date(value).toLocaleString("zh-CN", { hour12: false }) : "--");

/** hajimi 余额互通（fork）：连接配置、对账结果与待人工处理的划转。 */
export default function HajimiBridgeAdminPage() {
    const { message, modal } = App.useApp();
    const queryClient = useQueryClient();
    const overview = useQuery({ queryKey: overviewQueryKey, queryFn: ({ signal }) => getAdminHajimiBridgeOverview(signal), refetchOnWindowFocus: false });
    const [retryingId, setRetryingId] = useState("");

    const retry = (transfer: HajimiBridgeAttentionTransfer) => {
        modal.confirm({
            title: "重新处理这笔划转？",
            content: `${transfer.username || transfer.userId} · ${hajimiTransferDirectionLabel(transfer.direction)} ${formatHajimiUsd(transfer.amountMicro)}。系统会以同一划转编号重新向 hajimi 确认，不会重复扣款。`,
            okText: "重新处理",
            cancelText: "取消",
            onOk: async () => {
                setRetryingId(transfer.id);
                try {
                    await retryAdminHajimiTransfer(transfer.id);
                    message.success("已提交重新处理，请稍后刷新查看状态");
                    await queryClient.invalidateQueries({ queryKey: overviewQueryKey });
                } catch (error) {
                    message.error(error instanceof Error ? error.message : "重新处理失败");
                } finally {
                    setRetryingId("");
                }
            },
        });
    };

    const data = overview.data;
    return (
        <AdminPageFrame
            title="hajimi 互通"
            description="hajimi 余额转入、转回的连接状态、对账结果、毛利报表与待人工处理的划转"
            scroll
            actions={
                <Button icon={<RefreshCw className="size-3.5" />} loading={overview.isFetching} onClick={() => void overview.refetch()}>
                    刷新
                </Button>
            }
        >
            <div className="admin-settings-stack">
                {overview.error ? (
                    <div className="px-3 py-3 text-sm" role="alert">
                        {overview.error instanceof Error ? overview.error.message : "读取 hajimi 互通状态失败"}
                        <Button type="link" onClick={() => void overview.refetch()}>
                            重试
                        </Button>
                    </div>
                ) : null}
                <div className="grid overflow-hidden rounded-md border border-border sm:grid-cols-2 xl:grid-cols-4">
                    <AdminStatTile label="关联用户" value={data ? data.linkedUsers.toLocaleString() : "--"} detail="使用 hajimi 账号登录的影策账号" />
                    <AdminStatTile label="处理中" value={data ? data.pendingCount.toLocaleString() : "--"} detail="等待 hajimi 确认的划转" />
                    <AdminStatTile label="待人工处理" value={data ? data.attention.length.toLocaleString() : "--"} detail="两边记录不一致，需要核对" />
                    <AdminStatTile label="hajimi 连接" value={data ? (data.reachable ? "正常" : "无法连接") : "--"} detail={data?.config.urlHost || undefined} />
                </div>
                <HajimiBridgeConfigCard overview={data} />
                <HajimiBridgeReconcileCard overview={data} />
                <HajimiMarginCard />
                <SettingsSectionCard icon={<ArrowLeftRight className="size-4" />} title="待人工处理的划转" description="hajimi 与影策记录矛盾时不会自动动钱；核对后可按原划转编号重新处理。" layout="stacked">
                    <AdminDataTable<HajimiBridgeAttentionTransfer>
                        empty={<AdminTableEmpty title="没有待处理的划转" description="出现记录不一致的划转时会列在这里。" />}
                        table={{
                            rowKey: "id",
                            loading: overview.isPending,
                            dataSource: data?.attention ?? [],
                            pagination: false,
                            scroll: { x: 960 },
                            columns: [
                                { key: "createdAt", title: "时间", dataIndex: "createdAt", width: 170, render: (value: string) => <span className="tabular-nums">{formatTime(value)}</span> },
                                { key: "user", title: "用户", width: 180, render: (_, row) => <span title={row.userId}>{row.username || row.userId}</span> },
                                { key: "direction", title: "方向", width: 130, render: (_, row) => hajimiTransferDirectionLabel(row.direction) },
                                { key: "amount", title: "金额", dataIndex: "amountMicro", width: 120, align: "right", render: (value: number) => <span className="tabular-nums">{formatHajimiUsd(value)}</span> },
                                { key: "status", title: "状态", dataIndex: "status", width: 110, render: (value: string) => <AdminStatusBadge label={hajimiTransferStatusLabel(value)} tone={hajimiTransferStatusTone(value)} /> },
                                {
                                    key: "reason",
                                    title: "原因",
                                    dataIndex: "reason",
                                    ellipsis: true,
                                    render: (value: string, row) => (
                                        <span title={[row.lastError ? `最近错误：${row.lastError}` : "", row.attempts ? `已尝试 ${row.attempts} 次` : ""].filter(Boolean).join("；") || undefined}>{hajimiReasonMessage(value) || "--"}</span>
                                    ),
                                },
                                { key: "source", title: "来源", dataIndex: "source", width: 110, render: (value: string) => hajimiTransferSourceLabel(value) },
                                { key: "id", title: "划转编号", dataIndex: "id", width: 200, ellipsis: true, render: (value: string) => <code className="text-xs">{value}</code> },
                                {
                                    key: "actions",
                                    title: "操作",
                                    width: 110,
                                    fixed: "right",
                                    render: (_, row) => (
                                        <Button size="small" loading={retryingId === row.id} disabled={Boolean(retryingId)} onClick={() => retry(row)}>
                                            重新处理
                                        </Button>
                                    ),
                                },
                            ],
                        }}
                    />
                </SettingsSectionCard>
            </div>
        </AdminPageFrame>
    );
}

function HajimiBridgeConfigCard({ overview }: { overview?: HajimiBridgeAdminOverview }) {
    const config = overview?.config;
    return (
        <SettingsSectionCard
            icon={<ServerCog className="size-4" />}
            title="连接配置"
            description="由后端环境变量 HAJIMI_BRIDGE_* 决定，这里只读展示，不显示密钥。"
            status={config ? { label: config.enabled ? "已开启" : "未开启", color: config.enabled ? "success" : undefined } : undefined}
        >
            <dl className="grid gap-3 px-3 py-3 text-sm sm:grid-cols-2">
                <ConfigItem label="hajimi 内部地址">{config?.urlHost || "--"}</ConfigItem>
                <ConfigItem label="签名 Key ID">{config?.keyId || "--"}</ConfigItem>
                <ConfigItem label="登录身份来源域名">{config?.expectedIdpHost || "--"}</ConfigItem>
                <ConfigItem label="与当前 OAuth 授权地址">
                    {config ? (
                        <AdminStatusBadge
                            label={config.idpHostMatches ? "一致" : "不一致"}
                            tone={config.idpHostMatches ? "success" : "error"}
                            title={config.idpHostMatches ? undefined : "OAuth 授权地址的域名与 HAJIMI_BRIDGE_EXPECTED_IDP_HOST 不一致时，任何用户都不会被识别为 hajimi 关联账号"}
                        />
                    ) : (
                        "--"
                    )}
                </ConfigItem>
            </dl>
        </SettingsSectionCard>
    );
}

function HajimiBridgeReconcileCard({ overview }: { overview?: HajimiBridgeAdminOverview }) {
    const result = overview?.lastReconcile;
    const remote = overview?.remoteSummary;
    return (
        <SettingsSectionCard
            icon={<Scale className="size-4" />}
            title="对账"
            description="定期比对影策本地已到账、已转回金额与 hajimi 已提交金额；不一致只告警，不自动调账。"
            status={result ? { label: result.ok ? "一致" : "存在差异", color: result.ok ? "success" : "error" } : { label: "尚未对账" }}
        >
            <div className="grid gap-3 px-3 py-3 text-sm">
                {result ? (
                    <dl className="grid gap-3 sm:grid-cols-2">
                        <ConfigItem label="对账时间">{formatTime(result.at)}</ConfigItem>
                        <ConfigItem label="结果">{result.ok ? "一致" : "存在差异"}</ConfigItem>
                        <ConfigItem label="影策已到账（转入）">{formatHajimiUsd(result.localInMicro)}</ConfigItem>
                        <ConfigItem label="hajimi 已转入影策">{formatHajimiUsd(result.remoteToCanvasMicro)}</ConfigItem>
                        <ConfigItem label="影策已转回">{formatHajimiUsd(result.localOutMicro)}</ConfigItem>
                        <ConfigItem label="hajimi 已收到转回">{formatHajimiUsd(result.remoteToHajimiMicro)}</ConfigItem>
                    </dl>
                ) : (
                    <p className="text-foreground/55">还没有对账记录。</p>
                )}
                {result?.mismatches.length ? (
                    <ul className="grid gap-1 text-xs text-status-error" aria-label="对账差异">
                        {result.mismatches.map((item) => (
                            <li key={item}>{item}</li>
                        ))}
                    </ul>
                ) : null}
                {remote ? (
                    <div>
                        <strong className="block text-xs text-foreground/55">hajimi 实时汇总</strong>
                        <dl className="mt-1 grid gap-2 sm:grid-cols-2">
                            {Object.entries(remote).map(([key, value]) => (
                                <ConfigItem key={key} label={remoteSummaryLabel(key)}>
                                    {formatRemoteSummaryValue(key, value)}
                                </ConfigItem>
                            ))}
                        </dl>
                    </div>
                ) : overview && !overview.reachable ? (
                    <p className="text-xs text-foreground/55">hajimi 暂时无法连接，无法读取实时汇总。</p>
                ) : null}
            </div>
        </SettingsSectionCard>
    );
}

function ConfigItem({ label, children }: { label: string; children: ReactNode }) {
    return (
        <div className="min-w-0">
            <dt className="text-xs text-foreground/55">{label}</dt>
            <dd className="mt-0.5 truncate tabular-nums">{children}</dd>
        </div>
    );
}
