import { useQueryClient } from "@tanstack/react-query";
import { App, Button, InputNumber, Skeleton, Switch } from "antd";
import { ArrowDownToLine, ArrowLeftRight, ArrowUpFromLine, ChevronLeft, ChevronRight, CircleAlert, ExternalLink, History, Link2Off, RefreshCw, Repeat } from "lucide-react";
import { useEffect, useRef, useState, type ReactNode } from "react";

import { Callout } from "@/components/ui/product/callout";
import { announceHajimiCreditsChanged, hajimiBridgeWalletQueryKey, useHajimiBridgeWallet, useHajimiTransferAction, useHajimiTransfers } from "@/hooks/use-hajimi-bridge";
import {
    formatHajimiUsd,
    HAJIMI_TRANSFER_PRESETS_USD,
    hajimiDailyRemainingMicro,
    hajimiReasonMessage,
    hajimiTransferDirectionLabel,
    hajimiTransferErrorMessage,
    hajimiTransferOutcome,
    hajimiTransferSourceLabel,
    hajimiTransferStatusHint,
    hajimiTransferStatusLabel,
    hajimiTransferStatusTone,
    hajimiWalletView,
    hajimiWithdrawableMicro,
    microToUsd,
    usdToMicro,
    validateHajimiAutoTopup,
    validateHajimiTransferIn,
    validateHajimiTransferOut,
    type HajimiTransferIntent,
} from "@/lib/hajimi-bridge";
import { cn } from "@/lib/utils";
import { updateHajimiAutoTopup, type HajimiAutoTopup, type HajimiBridgeWallet, type HajimiTransfer, type HajimiTransferPage } from "@/services/api/hajimi-bridge";
import { useUserStore } from "@/stores/use-user-store";

const TRANSFER_PAGE_SIZE = 10;

/**
 * 积分中心里的“hajimi 余额”页签。只在互通开启时由积分中心展示；
 * 未绑定、hajimi 不可达、正常三种状态在视图里区分。
 */
export function HajimiBridgeWalletPanel({ availableMicro, onCreditsChanged }: { availableMicro: number | null; onCreditsChanged: () => void | Promise<void> }) {
    const queryClient = useQueryClient();
    const userId = useUserStore((state) => state.user?.id) || "";
    const walletQuery = useHajimiBridgeWallet(true);
    const view = hajimiWalletView(walletQuery.data);
    const [page, setPage] = useState(1);
    const transfersQuery = useHajimiTransfers(page, TRANSFER_PAGE_SIZE, view === "linked" || view === "unreachable");
    const transferIn = useHajimiTransferAction();
    const transferOut = useHajimiTransferAction();
    const lastStatuses = useRef(new Map<string, string>());

    const refreshCredits = async () => {
        await Promise.all([announceHajimiCreditsChanged(queryClient, userId), onCreditsChanged()]);
    };

    // 处理中的划转在轮询里到账或退回时，影策积分也变了，需要刷新积分余额。
    useEffect(() => {
        const items = transfersQuery.data?.items;
        if (!items) return;
        let settled = false;
        for (const item of items) {
            const previous = lastStatuses.current.get(item.id);
            if (previous === "pending" && (item.status === "credited" || item.status === "refunded")) settled = true;
            lastStatuses.current.set(item.id, item.status);
        }
        if (settled) void refreshCredits();
    }, [transfersQuery.data]);

    const submitTransfer = async (intent: HajimiTransferIntent) => {
        const action = intent.direction === "in" ? transferIn : transferOut;
        const transfer = await action.mutateAsync(intent);
        setPage(1);
        await refreshCredits();
        return transfer;
    };

    const saveAutoTopup = async (input: HajimiAutoTopup) => {
        const saved = await updateHajimiAutoTopup(input);
        queryClient.setQueryData<HajimiBridgeWallet>(hajimiBridgeWalletQueryKey(userId), (current) => (current ? { ...current, autoTopup: saved } : current));
        return saved;
    };

    if (walletQuery.isPending) {
        return (
            <div className="workspace-wallet-content is-hajimi">
                <section className="workspace-wallet-section">
                    <Skeleton active paragraph={{ rows: 4 }} />
                </section>
            </div>
        );
    }
    if (walletQuery.error) {
        return (
            <div className="workspace-wallet-content is-hajimi">
                <div className="workspace-wallet-inline-state is-error">
                    <CircleAlert />
                    <div>
                        <strong>hajimi 余额读取失败</strong>
                        <span>{walletQuery.error instanceof Error ? walletQuery.error.message : "请稍后重试"}</span>
                    </div>
                    <Button onClick={() => void walletQuery.refetch()}>重试</Button>
                </div>
            </div>
        );
    }

    return (
        <HajimiBridgeWalletView
            wallet={walletQuery.data ?? null}
            availableMicro={availableMicro}
            refreshing={walletQuery.isFetching}
            onRefresh={() => void Promise.all([walletQuery.refetch(), transfersQuery.refetch()])}
            transfers={transfersQuery.data}
            transfersLoading={transfersQuery.isPending && transfersQuery.fetchStatus !== "idle"}
            transfersError={transfersQuery.error}
            page={page}
            onPageChange={setPage}
            transferringIn={transferIn.isPending}
            transferringOut={transferOut.isPending}
            onTransfer={submitTransfer}
            onSaveAutoTopup={saveAutoTopup}
        />
    );
}

export type HajimiBridgeWalletViewProps = {
    wallet: HajimiBridgeWallet | null;
    availableMicro: number | null;
    refreshing?: boolean;
    onRefresh?: () => void;
    transfers?: HajimiTransferPage;
    transfersLoading?: boolean;
    transfersError?: unknown;
    page?: number;
    onPageChange?: (page: number) => void;
    transferringIn?: boolean;
    transferringOut?: boolean;
    onTransfer: (intent: HajimiTransferIntent) => Promise<HajimiTransfer>;
    onSaveAutoTopup: (input: HajimiAutoTopup) => Promise<HajimiAutoTopup>;
};

export function HajimiBridgeWalletView(props: HajimiBridgeWalletViewProps) {
    const { wallet } = props;
    const view = hajimiWalletView(wallet);
    if (view === "hidden" || !wallet) return null;
    if (view === "unlinked") {
        return (
            <div className="workspace-wallet-content is-hajimi">
                <div className="workspace-wallet-empty" data-hajimi-state="unlinked">
                    <Link2Off />
                    <strong>尚未关联 hajimi 账号</strong>
                    <span>使用 hajimi 账号登录的影策账号才能转入 hajimi 余额</span>
                </div>
            </div>
        );
    }
    const unreachable = view === "unreachable";
    const withdrawable = hajimiWithdrawableMicro(wallet, props.availableMicro);
    return (
        <div className="workspace-wallet-content is-hajimi" data-hajimi-state={view}>
            <section className="workspace-wallet-section">
                <div className="workspace-wallet-section-heading">
                    <div>
                        <h3>hajimi 余额</h3>
                        <p>1 美元 = 1 积分。转入后即可用于生成；从 hajimi 转入且未用掉的积分可以转回。</p>
                    </div>
                    <div className="flex shrink-0 items-center gap-1">
                        {wallet.hajimiPublicUrl ? (
                            <Button type="text" href={wallet.hajimiPublicUrl} target="_blank" rel="noopener noreferrer" icon={<ExternalLink className="size-3.5" />}>
                                去 hajimi 充值
                            </Button>
                        ) : null}
                        <Button type="text" aria-label="刷新 hajimi 余额" icon={<RefreshCw className="size-3.5" />} loading={props.refreshing} onClick={props.onRefresh} />
                    </div>
                </div>
                {unreachable ? (
                    <Callout tone="warning" title="hajimi 暂时无法连接，稍后再试" className="mb-3">
                        转入和转回暂不可用；已提交的划转会在恢复后自动确认。
                    </Callout>
                ) : wallet.grant && wallet.grant !== "active" ? (
                    <Callout tone="warning" title={hajimiReasonMessage("grant_inactive")} className="mb-3">
                        hajimi 侧的授权已撤销或失效，重新使用 hajimi 账号登录后即可继续转入。
                    </Callout>
                ) : null}
                {wallet.pendingCount > 0 ? (
                    <Callout tone="info" className="mb-3">
                        有 {wallet.pendingCount} 笔划转正在处理，确认后会自动更新。
                    </Callout>
                ) : null}
                <div className="grid gap-3 sm:grid-cols-3">
                    <HajimiStat label="hajimi 余额" value={unreachable ? "--" : formatHajimiUsd(wallet.hajimiBalanceMicro)} />
                    <HajimiStat
                        label="今日已转入"
                        value={wallet.limits ? `${formatHajimiUsd(wallet.limits.userDailyUsedMicro)} / ${formatHajimiUsd(wallet.limits.userDailyMicro)}` : "--"}
                        detail={wallet.limits ? `单笔 ${formatHajimiUsd(wallet.limits.minMicro)}–${formatHajimiUsd(wallet.limits.maxMicro)}` : undefined}
                    />
                    <HajimiStat label="可转回" value={formatHajimiUsd(withdrawable)} detail="仅限从 hajimi 转入的部分" />
                </div>
            </section>
            <HajimiTransferInSection wallet={wallet} disabled={unreachable || wallet.grant !== "active"} submitting={Boolean(props.transferringIn)} onTransfer={props.onTransfer} />
            <HajimiTransferOutSection wallet={wallet} withdrawableMicro={withdrawable} disabled={unreachable || wallet.grant !== "active"} submitting={Boolean(props.transferringOut)} onTransfer={props.onTransfer} />
            <HajimiAutoTopupSection wallet={wallet} onSave={props.onSaveAutoTopup} />
            <HajimiTransferHistory transfers={props.transfers} loading={props.transfersLoading} error={props.transfersError} page={props.page ?? 1} onPageChange={props.onPageChange} />
        </div>
    );
}

function HajimiStat({ label, value, detail }: { label: string; value: string; detail?: string }) {
    return (
        <div className="min-w-0 rounded-xl border border-[var(--user-border)] bg-[var(--user-surface-muted)] px-4 py-3">
            <span className="block text-[11px] text-[var(--user-ink-muted)]">{label}</span>
            <strong className="mt-1 block truncate text-base font-semibold tabular-nums text-[var(--user-ink)]">{value}</strong>
            {detail ? <small className="mt-0.5 block truncate text-[10px] text-[var(--user-ink-muted)]">{detail}</small> : null}
        </div>
    );
}

function SectionHeading({ title, description, icon }: { title: string; description: ReactNode; icon: ReactNode }) {
    return (
        <div className="workspace-wallet-section-heading">
            <div>
                <h3>{title}</h3>
                <p>{description}</p>
            </div>
            {icon}
        </div>
    );
}

function useTransferFeedback() {
    const { message } = App.useApp();
    return (transfer: HajimiTransfer) => {
        const outcome = hajimiTransferOutcome(transfer);
        message[outcome.tone](outcome.text);
    };
}

function HajimiTransferInSection({ wallet, disabled, submitting, onTransfer }: { wallet: HajimiBridgeWallet; disabled: boolean; submitting: boolean; onTransfer: HajimiBridgeWalletViewProps["onTransfer"] }) {
    const { message } = App.useApp();
    const feedback = useTransferFeedback();
    const [amountUsd, setAmountUsd] = useState<number | null>(10);
    const amountMicro = usdToMicro(amountUsd);
    const validation = validateHajimiTransferIn(amountMicro, wallet);
    const remaining = wallet.limits ? hajimiDailyRemainingMicro(wallet.limits) : null;

    const submit = async () => {
        if (validation) {
            message.warning(validation);
            return;
        }
        try {
            feedback(await onTransfer({ direction: "in", amountMicro, source: "manual" }));
        } catch (error) {
            message.error(hajimiTransferErrorMessage(error));
        }
    };

    return (
        <section className="workspace-wallet-section" aria-label="从 hajimi 转入">
            <SectionHeading title="转入影策积分" description={remaining === null ? "从 hajimi 余额转入，1 美元 = 1 积分。" : `从 hajimi 余额转入，今日还可转入 ${formatHajimiUsd(remaining)}。`} icon={<ArrowDownToLine />} />
            <div className="workspace-wallet-providers" role="group" aria-label="转入金额预设">
                {HAJIMI_TRANSFER_PRESETS_USD.map((preset) => (
                    <button key={preset} type="button" className={cn(amountUsd === preset && "is-selected")} disabled={disabled} aria-pressed={amountUsd === preset} onClick={() => setAmountUsd(preset)}>
                        ${preset}
                    </button>
                ))}
            </div>
            <div className="workspace-wallet-redeem-row">
                <InputNumber
                    size="large"
                    aria-label="转入金额（美元）"
                    prefix="$"
                    min={0}
                    precision={2}
                    step={1}
                    value={amountUsd}
                    disabled={disabled}
                    onChange={(value) => setAmountUsd(typeof value === "number" ? value : null)}
                    onPressEnter={() => void submit()}
                    className="min-w-0 flex-1"
                />
                <Button size="large" type="primary" loading={submitting} disabled={disabled || Boolean(validation)} onClick={() => void submit()}>
                    转入 {amountMicro > 0 ? formatHajimiUsd(amountMicro) : ""}
                </Button>
            </div>
            {!disabled && validation && amountMicro > 0 ? <p className="mt-2 text-[11px] text-[var(--user-ink-muted)]">{validation}</p> : null}
        </section>
    );
}

function HajimiTransferOutSection({
    wallet,
    withdrawableMicro,
    disabled,
    submitting,
    onTransfer,
}: {
    wallet: HajimiBridgeWallet;
    withdrawableMicro: number | null;
    disabled: boolean;
    submitting: boolean;
    onTransfer: HajimiBridgeWalletViewProps["onTransfer"];
}) {
    const { message, modal } = App.useApp();
    const feedback = useTransferFeedback();
    const [amountUsd, setAmountUsd] = useState<number | null>(null);
    const amountMicro = usdToMicro(amountUsd);
    const validation = validateHajimiTransferOut(amountMicro, withdrawableMicro, wallet.limits);
    const noWithdrawable = withdrawableMicro !== null && withdrawableMicro <= 0;

    const submit = () => {
        if (validation) {
            message.warning(validation);
            return;
        }
        modal.confirm({
            title: "转回 hajimi？",
            content: `将从影策积分扣除 ${formatHajimiUsd(amountMicro)}，转回到 hajimi 余额。`,
            okText: "确认转回",
            cancelText: "取消",
            onOk: async () => {
                try {
                    feedback(await onTransfer({ direction: "out", amountMicro, source: "manual" }));
                    setAmountUsd(null);
                } catch (error) {
                    message.error(hajimiTransferErrorMessage(error));
                }
            },
        });
    };

    return (
        <section className="workspace-wallet-section" aria-label="转回 hajimi">
            <SectionHeading title="转回 hajimi" description={`可转回 ${formatHajimiUsd(withdrawableMicro)}。影策充值、兑换码、签到获得的积分不能转回。`} icon={<ArrowUpFromLine />} />
            <div className="workspace-wallet-redeem-row">
                <InputNumber
                    size="large"
                    aria-label="转回金额（美元）"
                    prefix="$"
                    min={0}
                    precision={2}
                    step={1}
                    value={amountUsd}
                    disabled={disabled || noWithdrawable}
                    placeholder={noWithdrawable ? "暂无可转回额度" : "输入转回金额"}
                    onChange={(value) => setAmountUsd(typeof value === "number" ? value : null)}
                    className="min-w-0 flex-1"
                />
                <Button size="large" disabled={disabled || withdrawableMicro === null || noWithdrawable} onClick={() => setAmountUsd(withdrawableMicro === null ? null : Math.floor(microToUsd(withdrawableMicro) * 100) / 100)}>
                    全部
                </Button>
                <Button size="large" loading={submitting} disabled={disabled || noWithdrawable || Boolean(validation)} onClick={submit}>
                    转回 hajimi
                </Button>
            </div>
        </section>
    );
}

type AutoTopupDraft = { enabled: boolean; thresholdUsd: number | null; amountUsd: number | null; dailyLimitUsd: number | null };

function draftFromAutoTopup(value: HajimiAutoTopup): AutoTopupDraft {
    return { enabled: value.enabled, thresholdUsd: microToUsd(value.thresholdMicro), amountUsd: microToUsd(value.amountMicro), dailyLimitUsd: microToUsd(value.dailyLimitMicro) };
}

function autoTopupFromDraft(draft: AutoTopupDraft): HajimiAutoTopup {
    return { enabled: draft.enabled, thresholdMicro: usdToMicro(draft.thresholdUsd), amountMicro: usdToMicro(draft.amountUsd), dailyLimitMicro: usdToMicro(draft.dailyLimitUsd) };
}

function HajimiAutoTopupSection({ wallet, onSave }: { wallet: HajimiBridgeWallet; onSave: HajimiBridgeWalletViewProps["onSaveAutoTopup"] }) {
    const { message } = App.useApp();
    const savedKey = JSON.stringify(wallet.autoTopup);
    const [draft, setDraft] = useState<AutoTopupDraft>(() => draftFromAutoTopup(wallet.autoTopup));
    const [saving, setSaving] = useState(false);

    useEffect(() => {
        setDraft(draftFromAutoTopup(wallet.autoTopup));
    }, [savedKey]);

    const next = autoTopupFromDraft(draft);
    const dirty = JSON.stringify(next) !== savedKey;
    const validation = validateHajimiAutoTopup(next, wallet.limits);

    const save = async () => {
        if (validation) {
            message.warning(validation);
            return;
        }
        setSaving(true);
        try {
            await onSave(next);
            message.success(next.enabled ? "自动转入已开启" : "自动转入设置已保存");
        } catch (error) {
            message.error(error instanceof Error ? error.message : "保存自动转入设置失败");
        } finally {
            setSaving(false);
        }
    };

    return (
        <section className="workspace-wallet-section" aria-label="自动转入">
            <SectionHeading title="自动转入" description="可用积分低于阈值时，自动从 hajimi 余额转入固定金额；每日累计不超过上限。" icon={<Repeat />} />
            <label className="flex items-center justify-between gap-3">
                <span className="text-xs text-[var(--user-ink)]">开启自动转入</span>
                <Switch aria-label="开启自动转入" checked={draft.enabled} onChange={(enabled) => setDraft((current) => ({ ...current, enabled }))} />
            </label>
            <div className="mt-3 grid gap-3 sm:grid-cols-3">
                <AutoTopupField label="可用积分低于">
                    <InputNumber aria-label="自动转入触发阈值" min={0} precision={2} value={draft.thresholdUsd} onChange={(value) => setDraft((current) => ({ ...current, thresholdUsd: typeof value === "number" ? value : null }))} className="w-full" />
                </AutoTopupField>
                <AutoTopupField label="每次转入">
                    <InputNumber
                        aria-label="每次自动转入金额（美元）"
                        prefix="$"
                        min={0}
                        precision={2}
                        value={draft.amountUsd}
                        onChange={(value) => setDraft((current) => ({ ...current, amountUsd: typeof value === "number" ? value : null }))}
                        className="w-full"
                    />
                </AutoTopupField>
                <AutoTopupField label="每日最多">
                    <InputNumber
                        aria-label="每日自动转入上限（美元）"
                        prefix="$"
                        min={0}
                        precision={2}
                        value={draft.dailyLimitUsd}
                        onChange={(value) => setDraft((current) => ({ ...current, dailyLimitUsd: typeof value === "number" ? value : null }))}
                        className="w-full"
                    />
                </AutoTopupField>
            </div>
            <div className="mt-3 flex flex-wrap items-center justify-between gap-2">
                <span className="text-[11px] text-[var(--user-ink-muted)]">{dirty && validation ? validation : "自动转入同样计入每人每日转入上限。"}</span>
                <Button type="primary" loading={saving} disabled={!dirty || Boolean(validation)} onClick={() => void save()}>
                    保存
                </Button>
            </div>
        </section>
    );
}

function AutoTopupField({ label, children }: { label: string; children: ReactNode }) {
    return (
        <label className="grid min-w-0 gap-1.5">
            <span className="text-[11px] text-[var(--user-ink-muted)]">{label}</span>
            {children}
        </label>
    );
}

function HajimiTransferHistory({ transfers, loading, error, page, onPageChange }: { transfers?: HajimiTransferPage; loading?: boolean; error?: unknown; page: number; onPageChange?: (page: number) => void }) {
    const totalPages = Math.max(1, Math.ceil((transfers?.total || 0) / TRANSFER_PAGE_SIZE));
    return (
        <section className="workspace-wallet-section" aria-label="最近划转">
            <SectionHeading title="最近划转" description="hajimi 与影策之间的转入、转回记录。" icon={<History />} />
            {error ? (
                <div className="workspace-wallet-inline-state is-error">
                    <CircleAlert />
                    <div>
                        <strong>划转记录加载失败</strong>
                        <span>{error instanceof Error ? error.message : "请稍后重试"}</span>
                    </div>
                </div>
            ) : loading && !transfers ? (
                <Skeleton active paragraph={{ rows: 3 }} />
            ) : transfers?.items.length ? (
                <div className="workspace-wallet-ledger">
                    {transfers.items.map((transfer) => (
                        <HajimiTransferRow key={transfer.id} transfer={transfer} />
                    ))}
                </div>
            ) : (
                <div className="workspace-wallet-empty">
                    <ArrowLeftRight />
                    <strong>还没有划转记录</strong>
                </div>
            )}
            {transfers && transfers.total > TRANSFER_PAGE_SIZE ? (
                <div className="workspace-wallet-pagination">
                    <span>
                        第 {page} / {totalPages} 页
                    </span>
                    <button type="button" disabled={page <= 1 || loading} aria-label="上一页" onClick={() => onPageChange?.(page - 1)}>
                        <ChevronLeft />
                    </button>
                    <button type="button" disabled={page >= totalPages || loading} aria-label="下一页" onClick={() => onPageChange?.(page + 1)}>
                        <ChevronRight />
                    </button>
                </div>
            ) : null}
        </section>
    );
}

const STATUS_TEXT_CLASS: Record<string, string> = {
    success: "text-status-success",
    error: "text-status-error",
    warning: "text-status-warning",
    info: "text-[var(--user-accent)]",
    neutral: "text-[var(--user-ink-muted)]",
};

export function HajimiTransferRow({ transfer }: { transfer: HajimiTransfer }) {
    const incoming = transfer.direction === "in";
    const hint = hajimiTransferStatusHint(transfer);
    return (
        <article className="workspace-wallet-ledger-row" data-status={transfer.status}>
            <span className={cn("workspace-wallet-ledger-icon", incoming ? "is-income" : "is-consume")}>{incoming ? <ArrowDownToLine /> : <ArrowUpFromLine />}</span>
            <div>
                <strong>{hajimiTransferDirectionLabel(transfer.direction)}</strong>
                <span>
                    <b className={cn("font-medium", STATUS_TEXT_CLASS[hajimiTransferStatusTone(transfer.status)])}>{hajimiTransferStatusLabel(transfer.status)}</b>
                    {" · "}
                    {[hajimiTransferSourceLabel(transfer.source), hint].filter(Boolean).join(" · ")}
                </span>
            </div>
            <time>{new Date(transfer.createdAt).toLocaleString("zh-CN", { hour12: false })}</time>
            <b className={incoming ? "is-income" : "is-consume"}>
                {incoming ? "+" : "-"}
                {formatHajimiUsd(transfer.amountMicro)}
            </b>
        </article>
    );
}
