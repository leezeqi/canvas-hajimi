import { http } from "@/services/api/request";

/**
 * hajimi × 影策 余额互通（fork 专有）。金额单位统一为微美元：1 USD = 1,000,000 micro，
 * 与影策微积分 1:1（1 积分 = 1 美元），不受用户分组倍率影响。
 */

export type HajimiGrantStatus = "active" | "revoked" | "none";

export type HajimiBridgeLimits = {
    minMicro: number;
    maxMicro: number;
    userDailyMicro: number;
    userDailyUsedMicro: number;
};

export type HajimiAutoTopup = {
    enabled: boolean;
    thresholdMicro: number;
    amountMicro: number;
    dailyLimitMicro: number;
};

/** hajimi 不可达时 reachable=false，余额、净转入、授权状态和限额为 null；接口本身不因 hajimi 故障报错。 */
export type HajimiBridgeWallet = {
    enabled: boolean;
    linked: boolean;
    reachable: boolean;
    hajimiPublicUrl: string;
    hajimiBalanceMicro: number | null;
    netInMicro: number | null;
    grant: HajimiGrantStatus | null;
    limits: HajimiBridgeLimits | null;
    localNetInMicro: number;
    autoTopup: HajimiAutoTopup;
    pendingCount: number;
};

/** in = hajimi → 影策；out = 影策 → hajimi（转回）。 */
export type HajimiTransferDirection = "in" | "out";
export type HajimiTransferStatus = "pending" | "credited" | "completed" | "rejected" | "failed" | "refunded" | "attention";
/** reconcile 只出现在对账补录的待人工处理记录上。 */
export type HajimiTransferSource = "manual" | "prompt" | "auto" | "reconcile";
export type HajimiUserTransferSource = "manual" | "prompt";

export type HajimiTransfer = {
    id: string;
    direction: HajimiTransferDirection;
    amountMicro: number;
    status: HajimiTransferStatus;
    reason: string;
    source: HajimiTransferSource;
    createdAt: string;
    completedAt: string | null;
};

export type CreateHajimiTransferInput = {
    direction: HajimiTransferDirection;
    amountMicro: number;
    /** 16–80 位 [A-Za-z0-9_-]；同一用户同一键只会产生一笔划转。 */
    idempotencyKey: string;
    source?: HajimiUserTransferSource;
};

export type HajimiTransferPage = { items: HajimiTransfer[]; total: number };

export type HajimiBridgeAdminConfig = {
    enabled: boolean;
    urlHost: string;
    keyId: string;
    expectedIdpHost: string;
    idpHostMatches: boolean;
};

export type HajimiBridgeReconcileResult = {
    at: string;
    ok: boolean;
    /** 是否为每日一次的完整对账。 */
    full?: boolean;
    localInMicro: number;
    localOutMicro: number;
    remoteToCanvasMicro: number;
    remoteToHajimiMicro: number;
    mismatches: string[];
};

export type HajimiBridgeAttentionTransfer = HajimiTransfer & { userId: string; username: string; hajimiUserId?: number; hajimiSeq?: number; attempts?: number; lastError?: string };

export type HajimiBridgeAdminOverview = {
    config: HajimiBridgeAdminConfig;
    reachable: boolean;
    /** hajimi 对账汇总原样透传；字段命名以后端为准，页面按键名展示。 */
    remoteSummary: Record<string, unknown> | null;
    lastReconcile: HajimiBridgeReconcileResult | null;
    attention: HajimiBridgeAttentionTransfer[];
    pendingCount: number;
    linkedUsers: number;
};

export function getHajimiBridgeWallet(signal?: AbortSignal) {
    return http.get<HajimiBridgeWallet>("/hajimi-bridge/wallet", { signal });
}

/** 服务端会同步尝试 hajimi（≤ 8 秒），返回的状态可能仍是 pending。失败必须抛出，由调用方保留幂等键重试。 */
export function createHajimiTransfer(input: CreateHajimiTransferInput) {
    return http.post<HajimiTransfer>("/hajimi-bridge/transfers", input);
}

export function listHajimiTransfers(query: { page: number; pageSize: number }, signal?: AbortSignal) {
    return http.get<HajimiTransferPage>("/hajimi-bridge/transfers", { params: { page: query.page, pageSize: query.pageSize }, signal });
}

export function updateHajimiAutoTopup(input: HajimiAutoTopup) {
    return http.put<HajimiAutoTopup>("/hajimi-bridge/auto-topup", input);
}

export function getAdminHajimiBridgeOverview(signal?: AbortSignal) {
    return http.get<HajimiBridgeAdminOverview>("/admin/hajimi-bridge/overview", { signal });
}

export function retryAdminHajimiTransfer(id: string) {
    return http.post<unknown>(`/admin/hajimi-bridge/transfers/${encodeURIComponent(id)}/retry`);
}

/** 毛利报表（合同 §6）。金额均为微美元；hajimi 成本拿不到时相关字段为 null。 */
export type HajimiMarginHajimiStatus = "ok" | "disabled" | "not_configured" | "unavailable";

export type HajimiMarginDay = {
    date: string;
    hajimiOrders: number;
    hajimiRevenueMicro: number;
    hajimiRequests: number | null;
    hajimiChargedMicro: number | null;
    hajimiUpstreamCostMicro: number | null;
    /** 走 hajimi 渠道的收入 − hajimi 扣费。 */
    grossProfitMicro: number | null;
    /** 走 hajimi 渠道的收入 − hajimi 上游成本（两边合并看）。 */
    combinedProfitMicro: number | null;
    otherOrders: number;
    otherRevenueMicro: number;
    otherEstimatedCostMicro: number;
    otherUncostedOrders: number;
};

export type HajimiMarginModel = { model: string; viaHajimi: boolean; orders: number; revenueMicro: number; estimatedCostMicro: number; uncostedOrders: number };

export type HajimiMarginChannel = { id: string; name: string; host: string; system: boolean; viaHajimi: boolean; deleted: boolean; orders: number; revenueMicro: number };

export type HajimiMarginCostAmounts = { requests: number; chargedMicro: number; upstreamCostMicro: number };

export type HajimiMarginReport = {
    from: string;
    to: string;
    utcOffsetMinutes: number;
    channelRule: string[];
    days: HajimiMarginDay[];
    totals: HajimiMarginDay;
    models: HajimiMarginModel[];
    channels: HajimiMarginChannel[];
    hajimi: {
        status: HajimiMarginHajimiStatus;
        message: string;
        scopeUserIds: number[];
        scopeApiKeyIds: number[];
        keys: (HajimiMarginCostAmounts & { apiKeyId: number; name: string; userId: number })[];
        models: (HajimiMarginCostAmounts & { model: string })[];
        accounts: { userId: number; balanceMicro: number }[];
    };
    openOrders: number;
    openReservedMicro: number;
    generatedAt: string;
};

export type HajimiMarginQuery = { from: string; to: string; utcOffsetMinutes: number };

export function getAdminHajimiBridgeMargin(query: HajimiMarginQuery, signal?: AbortSignal) {
    return http.get<HajimiMarginReport>("/admin/hajimi-bridge/margin", { params: { from: query.from, to: query.to, utcOffsetMinutes: query.utcOffsetMinutes }, signal });
}
