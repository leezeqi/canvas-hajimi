import { createClientUUID } from "@/lib/client-id";
import { ApiError } from "@/services/api/request";
import type { CreateHajimiTransferInput, HajimiAutoTopup, HajimiBridgeLimits, HajimiBridgeWallet, HajimiTransfer, HajimiTransferDirection, HajimiTransferSource, HajimiTransferStatus, HajimiUserTransferSource } from "@/services/api/hajimi-bridge";

/** 1 USD = 1,000,000 micro = 1 影策积分。 */
export const HAJIMI_MICRO_PER_USD = 1_000_000;
export const HAJIMI_TRANSFER_PRESETS_USD = [5, 10, 20, 50] as const;
/** 转回 hajimi 的单笔下限（$0.01），与 hajimi to_hajimi 校验一致。 */
export const HAJIMI_TRANSFER_OUT_MIN_MICRO = 10_000;

const usdFormatter = new Intl.NumberFormat("en-US", { style: "currency", currency: "USD", minimumFractionDigits: 2, maximumFractionDigits: 6 });

export function formatHajimiUsd(micro: number | null | undefined) {
    if (micro === null || micro === undefined || !Number.isFinite(micro)) return "--";
    return usdFormatter.format(micro / HAJIMI_MICRO_PER_USD);
}

export function usdToMicro(usd: number | null | undefined) {
    if (usd === null || usd === undefined || !Number.isFinite(usd)) return 0;
    return Math.round(usd * HAJIMI_MICRO_PER_USD);
}

export function microToUsd(micro: number) {
    return micro / HAJIMI_MICRO_PER_USD;
}

// hajimi 划转拒绝原因（§3.4）及影策侧接口原因 → 用户可读文案。
const REASON_MESSAGES: Record<string, string> = {
    insufficient_balance: "hajimi 余额不足",
    user_inactive: "hajimi 账号已停用或不存在",
    grant_inactive: "请重新使用 hajimi 账号登录授权",
    canvas_user_mismatch: "该 hajimi 账号已绑定其他影策账号",
    amount_out_of_range: "金额超出单笔允许范围",
    user_daily_limit: "已达今日转入上限",
    global_daily_limit: "今日全站转入额度已用完，请明天再试",
    net_in_exceeded: "转回金额超过可转回额度（只能转回从 hajimi 转入的部分）",
    // 影策接口错误 reason（403/400/409/503）。
    hajimi_bridge_disabled: "hajimi 余额互通未开启",
    hajimi_not_linked: "使用 hajimi 账号登录的影策账号才能转入 hajimi 余额",
    hajimi_unavailable: "hajimi 暂时无法连接，稍后再试",
    hajimi_withdraw_exceeds_net_in: "转回金额超过可转回额度（只能转回从 hajimi 转入的部分）",
    hajimi_transfer_finished: "该划转已结束，无需重试",
    hajimi_needs_manual_review: "这笔划转需要管理员核对后处理",
    insufficient_credits: "影策积分不足",
    // 影策本地划转记录的结论原因。
    not_found_after_window: "hajimi 未收到这笔划转，hajimi 余额未扣除",
    remote_mismatch: "两边记录不一致，待管理员核对",
    transfer_conflict: "划转编号冲突，待管理员核对",
    missing_local_record: "影策缺少对应记录，待管理员核对",
};

// 对账发现 hajimi 已入账、但本地记录处于其他终态时的原因前缀（后接本地状态）。
const COMMITTED_BUT_LOCAL_PREFIX = "remote_committed_local_";

export function hajimiReasonMessage(reason: string | null | undefined) {
    const code = (reason || "").trim();
    if (!code) return "";
    if (code.startsWith(COMMITTED_BUT_LOCAL_PREFIX)) return `hajimi 已入账，但影策记录为「${hajimiTransferStatusLabel(code.slice(COMMITTED_BUT_LOCAL_PREFIX.length))}」，待管理员核对`;
    return REASON_MESSAGES[code] ?? `划转未完成（${code}）`;
}

/** 写操作失败时的提示：优先按机器可读 reason 映射，其次使用后端给出的可展示文案。 */
export function hajimiTransferErrorMessage(error: unknown, fallback = "划转失败，请稍后重试") {
    if (error instanceof ApiError && error.reason && REASON_MESSAGES[error.reason]) return REASON_MESSAGES[error.reason];
    if (error instanceof Error && error.message) return error.message;
    return fallback;
}

const STATUS_LABELS: Record<HajimiTransferStatus, string> = {
    pending: "处理中",
    credited: "已到账",
    completed: "已转回",
    rejected: "被拒绝",
    failed: "失败",
    refunded: "已退回",
    attention: "待人工处理",
};

export type HajimiStatusTone = "neutral" | "success" | "warning" | "error" | "info";

const STATUS_TONES: Record<HajimiTransferStatus, HajimiStatusTone> = {
    pending: "info",
    credited: "success",
    completed: "success",
    rejected: "error",
    failed: "error",
    refunded: "neutral",
    attention: "warning",
};

export function hajimiTransferStatusLabel(status: string) {
    return STATUS_LABELS[status as HajimiTransferStatus] ?? status;
}

export function hajimiTransferStatusTone(status: string): HajimiStatusTone {
    return STATUS_TONES[status as HajimiTransferStatus] ?? "neutral";
}

export function hajimiTransferDirectionLabel(direction: HajimiTransferDirection) {
    return direction === "in" ? "从 hajimi 转入" : "转回 hajimi";
}

export function hajimiTransferSourceLabel(source: HajimiTransferSource | string) {
    if (source === "prompt") return "积分不足提示";
    if (source === "auto") return "自动转入";
    if (source === "reconcile") return "对账补录";
    return "手动";
}

/** 列表里状态旁的补充说明；没有需要解释的内容时返回空串。 */
export function hajimiTransferStatusHint(transfer: Pick<HajimiTransfer, "status" | "reason" | "direction">) {
    const reason = hajimiReasonMessage(transfer.reason);
    switch (transfer.status) {
        case "pending":
            return "正在与 hajimi 确认";
        case "rejected":
            return reason;
        case "failed":
            return reason || "未能确认划转结果，hajimi 余额未扣除";
        case "refunded":
            return reason ? `积分已退回影策账户（${reason}）` : "积分已退回影策账户";
        case "attention":
            return "已提交管理员核对";
        default:
            return "";
    }
}

/** 提交划转后给用户的一句结果反馈。 */
export function hajimiTransferOutcome(transfer: Pick<HajimiTransfer, "status" | "reason" | "direction" | "amountMicro">): { tone: "success" | "info" | "warning" | "error"; text: string } {
    const amount = formatHajimiUsd(transfer.amountMicro);
    switch (transfer.status) {
        case "credited":
            return { tone: "success", text: `已从 hajimi 转入 ${amount}，积分已到账` };
        case "completed":
            return { tone: "success", text: `已转回 ${amount} 到 hajimi` };
        case "pending":
            return { tone: "info", text: transfer.direction === "in" ? "已提交，hajimi 确认后积分会自动到账" : "已提交，hajimi 确认后完成转回" };
        case "refunded":
            return { tone: "warning", text: hajimiTransferStatusHint(transfer) };
        case "attention":
            return { tone: "warning", text: "这笔划转需要管理员核对，处理后会更新状态" };
        default:
            return { tone: "error", text: hajimiTransferStatusHint(transfer) || "划转未完成" };
    }
}

const LEDGER_TITLES: Record<string, string> = {
    hajimi_transfer_in: "从 hajimi 转入",
    hajimi_transfer_out: "转回 hajimi",
    hajimi_transfer_out_refund: "转回 hajimi 退回",
};

/** 积分流水里的 hajimi 划转类型；非 hajimi 类型返回空串，由原有逻辑处理。 */
export function hajimiLedgerTitle(type: string) {
    return LEDGER_TITLES[type] ?? "";
}

export type HajimiWalletView = "hidden" | "unlinked" | "unreachable" | "linked";

/** 钱包卡片状态：未开启（或读取失败）不展示；开启后依次区分未绑定、hajimi 不可达、正常。 */
export function hajimiWalletView(wallet: HajimiBridgeWallet | null | undefined): HajimiWalletView {
    if (!wallet?.enabled) return "hidden";
    if (!wallet.linked) return "unlinked";
    if (!wallet.reachable) return "unreachable";
    return "linked";
}

export function hajimiDailyRemainingMicro(limits: HajimiBridgeLimits) {
    return Math.max(0, limits.userDailyMicro - limits.userDailyUsedMicro);
}

export function validateHajimiTransferIn(amountMicro: number, wallet: Pick<HajimiBridgeWallet, "limits" | "hajimiBalanceMicro">) {
    if (!Number.isFinite(amountMicro) || amountMicro <= 0) return "请输入转入金额";
    const limits = wallet.limits;
    if (!limits) return "hajimi 暂时无法连接，稍后再试";
    if (amountMicro < limits.minMicro) return `单笔至少转入 ${formatHajimiUsd(limits.minMicro)}`;
    if (amountMicro > limits.maxMicro) return `单笔最多转入 ${formatHajimiUsd(limits.maxMicro)}`;
    const remaining = hajimiDailyRemainingMicro(limits);
    if (remaining <= 0) return "已达今日转入上限";
    if (amountMicro > remaining) return `今日还可转入 ${formatHajimiUsd(remaining)}`;
    if (wallet.hajimiBalanceMicro !== null && amountMicro > wallet.hajimiBalanceMicro) return "hajimi 余额不足";
    return "";
}

/** 可转回额度 = min(本地净转入, hajimi 净转入, 当前可用积分)；任一未知时返回 null。 */
export function hajimiWithdrawableMicro(wallet: Pick<HajimiBridgeWallet, "localNetInMicro" | "netInMicro">, availableMicro: number | null | undefined) {
    if (wallet.netInMicro === null || availableMicro === null || availableMicro === undefined) return null;
    return Math.max(0, Math.min(wallet.localNetInMicro, wallet.netInMicro, availableMicro));
}

export function validateHajimiTransferOut(amountMicro: number, withdrawableMicro: number | null, limits: HajimiBridgeLimits | null) {
    if (!Number.isFinite(amountMicro) || amountMicro <= 0) return "请输入转回金额";
    if (amountMicro < HAJIMI_TRANSFER_OUT_MIN_MICRO) return `单笔至少转回 ${formatHajimiUsd(HAJIMI_TRANSFER_OUT_MIN_MICRO)}`;
    if (limits && amountMicro > limits.maxMicro) return `单笔最多转回 ${formatHajimiUsd(limits.maxMicro)}`;
    if (withdrawableMicro === null) return "暂时无法确认可转回额度，稍后再试";
    if (amountMicro > withdrawableMicro) return `最多可转回 ${formatHajimiUsd(withdrawableMicro)}`;
    return "";
}

/**
 * 与后端同口径：阈值、金额、每日上限的基本约束始终校验；hajimi 单笔限额只在开启时校验，
 * 关闭自动转入不能因为 hajimi 当前限额变化而被挡住。
 */
export function validateHajimiAutoTopup(input: HajimiAutoTopup, limits: HajimiBridgeLimits | null) {
    if (!Number.isFinite(input.thresholdMicro) || input.thresholdMicro < 0) return "触发阈值不能小于 0";
    if (!Number.isFinite(input.amountMicro) || input.amountMicro <= 0) return "每次转入金额必须大于 0";
    if (input.enabled && limits && (input.amountMicro < limits.minMicro || input.amountMicro > limits.maxMicro)) return `每次转入金额需在 ${formatHajimiUsd(limits.minMicro)}–${formatHajimiUsd(limits.maxMicro)} 之间`;
    if (!Number.isFinite(input.dailyLimitMicro) || input.dailyLimitMicro < input.amountMicro) return "每日上限不能小于每次转入金额";
    return "";
}

/**
 * 积分不足时一键转入的默认金额：max(缺口向上取整到整美元, 单笔下限)，再受单笔上限、
 * 今日剩余额度和 hajimi 余额约束。缺口未知时按 0 处理（即单笔下限）。无法转出下限时返回 null。
 */
export function hajimiPromptAmountMicro(input: { shortfallMicro?: number | null; limits: HajimiBridgeLimits; balanceMicro: number }) {
    const shortfall = input.shortfallMicro && input.shortfallMicro > 0 ? Math.ceil(input.shortfallMicro / HAJIMI_MICRO_PER_USD) * HAJIMI_MICRO_PER_USD : 0;
    const cap = Math.min(input.limits.maxMicro, hajimiDailyRemainingMicro(input.limits), input.balanceMicro);
    const amount = Math.min(Math.max(shortfall, input.limits.minMicro), cap);
    return amount >= input.limits.minMicro && amount > 0 ? amount : null;
}

/** 只有开启、已绑定、hajimi 可达且授权有效、并且能转出下限时才提供一键转入；否则保持原有积分不足处理。 */
export function hajimiPromptOffer(wallet: HajimiBridgeWallet | null | undefined, shortfallMicro?: number | null) {
    if (hajimiWalletView(wallet) !== "linked" || !wallet || wallet.grant !== "active" || !wallet.limits || wallet.hajimiBalanceMicro === null) return null;
    const amountMicro = hajimiPromptAmountMicro({ shortfallMicro, limits: wallet.limits, balanceMicro: wallet.hajimiBalanceMicro });
    return amountMicro === null ? null : { amountMicro, balanceMicro: wallet.hajimiBalanceMicro };
}

const IDEMPOTENCY_KEY_PATTERN = /^[A-Za-z0-9_-]{16,80}$/;

export function isValidHajimiIdempotencyKey(value: string) {
    return IDEMPOTENCY_KEY_PATTERN.test(value);
}

/** 每次用户操作一个新键（36 位）；randomUUID 不可用的 HTTP 内网环境由 createClientUUID 兜底。 */
export function newHajimiIdempotencyKey() {
    return `hjb_${createClientUUID().replaceAll("-", "")}`;
}

export type HajimiTransferIntent = { direction: HajimiTransferDirection; amountMicro: number; source: HajimiUserTransferSource };

/**
 * 同一操作的幂等键：请求失败（断网、超时、5xx）后重试同一方向、金额和来源时复用原键，
 * 服务端因此最多只产生一笔划转；拿到服务端结果或改了金额后才换新键。
 */
export function createHajimiTransferKeyring(generate: () => string = newHajimiIdempotencyKey) {
    let current: { key: string; fingerprint: string } | null = null;
    return {
        keyFor(intent: HajimiTransferIntent) {
            const fingerprint = `${intent.direction}|${intent.amountMicro}|${intent.source}`;
            if (!current || current.fingerprint !== fingerprint) current = { key: generate(), fingerprint };
            return current.key;
        },
        settle() {
            current = null;
        },
        get pendingKey() {
            return current?.key ?? "";
        },
    };
}

export type HajimiTransferKeyring = ReturnType<typeof createHajimiTransferKeyring>;

export async function submitHajimiTransfer(keyring: HajimiTransferKeyring, intent: HajimiTransferIntent, send: (input: CreateHajimiTransferInput) => Promise<HajimiTransfer>) {
    const idempotencyKey = keyring.keyFor(intent);
    const transfer = await send({ ...intent, idempotencyKey });
    keyring.settle();
    return transfer;
}

/**
 * 影策积分不足的识别。后端对积分不足返回 HTTP 400（reason 为通用 invalid_argument），
 * 没有专用 reason，只能和后端短剧编排一样按“积分不足”文案识别；若后端以后提供
 * `insufficient_credits` reason 则优先使用。后台调账、钱包和互通自身的接口不触发提示。
 */
export function isInsufficientCreditsResponse(input: { method?: string; url?: string; status?: number; reason?: string; message?: string }) {
    const method = (input.method || "get").toLowerCase();
    if (method === "get" || method === "head" || method === "options") return false;
    const path = requestPath(input.url);
    if (/^\/(?:admin|auth|wallet|payments|hajimi-bridge)(?:\/|$)/.test(path)) return false;
    if (input.reason === "insufficient_credits") return true;
    return input.status === 400 && /积分(?:余额)?不足/.test(input.message || "");
}

function requestPath(url: string | undefined) {
    const withoutOrigin = (url || "").replace(/^[a-z][a-z0-9+.-]*:\/\/[^/]+/i, "");
    const path = withoutOrigin.split(/[?#]/)[0] || "/";
    const normalized = path.startsWith("/") ? path : `/${path}`;
    return normalized.replace(/^\/api(?=\/|$)/, "") || "/";
}

const REMOTE_SUMMARY_LABELS: Record<string, string> = {
    tocanvascommittedmicro: "已转入影策（hajimi 记录）",
    tohajimicommittedmicro: "已转回 hajimi（hajimi 记录）",
    committedcount: "已提交笔数",
    maxseq: "最新序号",
};

// 管理端对账汇总：后端可能原样透传 hajimi 的 snake_case，也可能转成 camelCase；两种都识别。
function normalizeSummaryKey(key: string) {
    return key.replace(/[_-]/g, "").toLowerCase();
}

export function remoteSummaryLabel(key: string) {
    return REMOTE_SUMMARY_LABELS[normalizeSummaryKey(key)] ?? key;
}

export function formatRemoteSummaryValue(key: string, value: unknown) {
    if (typeof value === "number") return normalizeSummaryKey(key).endsWith("micro") ? formatHajimiUsd(value) : value.toLocaleString();
    if (value === null || value === undefined) return "--";
    return typeof value === "string" ? value : JSON.stringify(value);
}
