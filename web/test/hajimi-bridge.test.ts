import { describe, expect, test } from "bun:test";

import {
    createHajimiTransferKeyring,
    formatHajimiUsd,
    formatRemoteSummaryValue,
    hajimiLedgerTitle,
    hajimiPromptAmountMicro,
    hajimiPromptOffer,
    hajimiReasonMessage,
    hajimiTransferErrorMessage,
    hajimiTransferOutcome,
    hajimiTransferStatusLabel,
    hajimiTransferStatusTone,
    hajimiWalletView,
    hajimiWithdrawableMicro,
    isInsufficientCreditsResponse,
    isValidHajimiIdempotencyKey,
    newHajimiIdempotencyKey,
    remoteSummaryLabel,
    submitHajimiTransfer,
    usdToMicro,
    validateHajimiAutoTopup,
    validateHajimiTransferIn,
    validateHajimiTransferOut,
} from "../src/lib/hajimi-bridge";
import { oauthAuthorizationURLAllowed, oauthEndpointURLError, oauthServerEndpointURLAllowed } from "../src/lib/oauth-endpoint-url";
import { brandOAuthMessage, OAUTH_PROVIDER_NAME } from "../src/lib/oauth-brand";
import {
    createHajimiTransfer,
    getAdminHajimiBridgeOverview,
    getHajimiBridgeWallet,
    listHajimiTransfers,
    retryAdminHajimiTransfer,
    updateHajimiAutoTopup,
    type CreateHajimiTransferInput,
    type HajimiBridgeWallet,
    type HajimiTransfer,
} from "../src/services/api/hajimi-bridge";
import { ApiError, apiClient } from "../src/services/api/request";

const limits = { minMicro: 1_000_000, maxMicro: 200_000_000, userDailyMicro: 200_000_000, userDailyUsedMicro: 0 };

function wallet(overrides: Partial<HajimiBridgeWallet> = {}): HajimiBridgeWallet {
    return {
        enabled: true,
        linked: true,
        reachable: true,
        hajimiPublicUrl: "https://hajimi.example.test",
        hajimiBalanceMicro: 30_000_000,
        netInMicro: 12_000_000,
        grant: "active",
        limits,
        localNetInMicro: 10_000_000,
        autoTopup: { enabled: false, thresholdMicro: 1_000_000, amountMicro: 5_000_000, dailyLimitMicro: 20_000_000 },
        pendingCount: 0,
        ...overrides,
    };
}

function transfer(overrides: Partial<HajimiTransfer> = {}): HajimiTransfer {
    return { id: "yct_0123456789abcdef0123456789abcdef", direction: "in", amountMicro: 5_000_000, status: "credited", reason: "", source: "manual", createdAt: "2026-11-08T00:00:00Z", completedAt: "2026-11-08T00:00:01Z", ...overrides };
}

describe("hajimi reason codes and status labels", () => {
    test("maps every hajimi rejection reason to Chinese copy", () => {
        expect(hajimiReasonMessage("insufficient_balance")).toBe("hajimi 余额不足");
        expect(hajimiReasonMessage("user_daily_limit")).toBe("已达今日转入上限");
        expect(hajimiReasonMessage("grant_inactive")).toBe("请重新使用 hajimi 账号登录授权");
        for (const reason of ["insufficient_balance", "user_inactive", "grant_inactive", "canvas_user_mismatch", "amount_out_of_range", "user_daily_limit", "global_daily_limit", "net_in_exceeded"]) {
            const text = hajimiReasonMessage(reason);
            expect(text).not.toBe("");
            expect(text).not.toContain(reason);
            expect(text).toMatch(/[一-龥]/);
        }
        expect(hajimiReasonMessage("")).toBe("");
        expect(hajimiReasonMessage("something_new")).toBe("划转未完成（something_new）");
        // 影策接口与本地记录的原因码（与后端 hajimibridge 常量一致）。
        expect(hajimiReasonMessage("hajimi_unavailable")).toBe("hajimi 暂时无法连接，稍后再试");
        expect(hajimiReasonMessage("hajimi_withdraw_exceeds_net_in")).toContain("可转回额度");
        expect(hajimiReasonMessage("not_found_after_window")).toBe("hajimi 未收到这笔划转，hajimi 余额未扣除");
        expect(hajimiReasonMessage("remote_committed_local_refunded")).toBe("hajimi 已入账，但影策记录为「已退回」，待管理员核对");
    });

    test("labels and tones every transfer status", () => {
        expect(["pending", "credited", "completed", "rejected", "failed", "refunded", "attention"].map(hajimiTransferStatusLabel)).toEqual(["处理中", "已到账", "已转回", "被拒绝", "失败", "已退回", "待人工处理"]);
        expect(hajimiTransferStatusTone("credited")).toBe("success");
        expect(hajimiTransferStatusTone("rejected")).toBe("error");
        expect(hajimiTransferStatusTone("attention")).toBe("warning");
    });

    test("prefers mapped API reasons over backend message and falls back to the message", () => {
        expect(hajimiTransferErrorMessage(new ApiError("forbidden", { status: 403, reason: "hajimi_not_linked" }))).toBe("使用 hajimi 账号登录的影策账号才能转入 hajimi 余额");
        expect(hajimiTransferErrorMessage(new ApiError("金额无效", { status: 400, reason: "invalid_argument" }))).toBe("金额无效");
        expect(hajimiTransferErrorMessage("boom")).toBe("划转失败，请稍后重试");
    });

    test("summarises a submitted transfer for the user", () => {
        expect(hajimiTransferOutcome(transfer())).toEqual({ tone: "success", text: "已从 hajimi 转入 $5.00，积分已到账" });
        expect(hajimiTransferOutcome(transfer({ status: "pending" })).tone).toBe("info");
        expect(hajimiTransferOutcome(transfer({ status: "rejected", reason: "insufficient_balance" }))).toEqual({ tone: "error", text: "hajimi 余额不足" });
        expect(hajimiTransferOutcome(transfer({ direction: "out", status: "refunded", reason: "net_in_exceeded" })).text).toContain("积分已退回影策账户");
    });

    test("labels hajimi ledger entry types and ignores others", () => {
        expect(hajimiLedgerTitle("hajimi_transfer_in")).toBe("从 hajimi 转入");
        expect(hajimiLedgerTitle("hajimi_transfer_out")).toBe("转回 hajimi");
        expect(hajimiLedgerTitle("hajimi_transfer_out_refund")).toBe("转回 hajimi 退回");
        expect(hajimiLedgerTitle("consume")).toBe("");
    });

    test("formats micro-USD and converts USD input without float drift", () => {
        expect(formatHajimiUsd(5_000_000)).toBe("$5.00");
        expect(formatHajimiUsd(12_345_678)).toBe("$12.345678");
        expect(formatHajimiUsd(-1_500_000)).toBe("-$1.50");
        expect(formatHajimiUsd(null)).toBe("--");
        expect(usdToMicro(0.29)).toBe(290_000);
        expect(usdToMicro(19.99)).toBe(19_990_000);
        expect(usdToMicro(null)).toBe(0);
    });
});

describe("hajimi wallet card states", () => {
    test("distinguishes disabled, not linked, unreachable and linked", () => {
        expect(hajimiWalletView(undefined)).toBe("hidden");
        expect(hajimiWalletView(wallet({ enabled: false }))).toBe("hidden");
        expect(hajimiWalletView(wallet({ linked: false }))).toBe("unlinked");
        expect(hajimiWalletView(wallet({ reachable: false, hajimiBalanceMicro: null, netInMicro: null, grant: null, limits: null }))).toBe("unreachable");
        expect(hajimiWalletView(wallet())).toBe("linked");
    });

    test("withdrawable is the minimum of local net-in, hajimi net-in and available credits", () => {
        expect(hajimiWithdrawableMicro(wallet(), 50_000_000)).toBe(10_000_000);
        expect(hajimiWithdrawableMicro(wallet({ netInMicro: 4_000_000 }), 50_000_000)).toBe(4_000_000);
        expect(hajimiWithdrawableMicro(wallet(), 2_500_000)).toBe(2_500_000);
        expect(hajimiWithdrawableMicro(wallet({ localNetInMicro: -3 }), 2_500_000)).toBe(0);
        expect(hajimiWithdrawableMicro(wallet({ netInMicro: null }), 2_500_000)).toBeNull();
        expect(hajimiWithdrawableMicro(wallet(), null)).toBeNull();
    });

    test("validates transfer-in against limits, daily remaining and hajimi balance", () => {
        expect(validateHajimiTransferIn(5_000_000, wallet())).toBe("");
        expect(validateHajimiTransferIn(0, wallet())).toBe("请输入转入金额");
        expect(validateHajimiTransferIn(500_000, wallet())).toBe("单笔至少转入 $1.00");
        expect(validateHajimiTransferIn(300_000_000, wallet())).toBe("单笔最多转入 $200.00");
        expect(validateHajimiTransferIn(50_000_000, wallet())).toBe("hajimi 余额不足");
        expect(validateHajimiTransferIn(5_000_000, wallet({ limits: { ...limits, userDailyUsedMicro: 198_000_000 } }))).toBe("今日还可转入 $2.00");
        expect(validateHajimiTransferIn(5_000_000, wallet({ limits: { ...limits, userDailyUsedMicro: 200_000_000 } }))).toBe("已达今日转入上限");
        expect(validateHajimiTransferIn(5_000_000, wallet({ limits: null }))).toBe("hajimi 暂时无法连接，稍后再试");
    });

    test("validates transfer-out and auto top-up settings", () => {
        expect(validateHajimiTransferOut(5_000_000, 10_000_000, limits)).toBe("");
        expect(validateHajimiTransferOut(5_000, 10_000_000, limits)).toBe("单笔至少转回 $0.01");
        expect(validateHajimiTransferOut(11_000_000, 10_000_000, limits)).toBe("最多可转回 $10.00");
        expect(validateHajimiTransferOut(5_000_000, null, limits)).toContain("暂时无法确认");
        const auto = { enabled: true, thresholdMicro: 1_000_000, amountMicro: 5_000_000, dailyLimitMicro: 20_000_000 };
        expect(validateHajimiAutoTopup(auto, limits)).toBe("");
        expect(validateHajimiAutoTopup({ ...auto, thresholdMicro: -1 }, limits)).toBe("触发阈值不能小于 0");
        expect(validateHajimiAutoTopup({ ...auto, amountMicro: 500_000 }, limits)).toContain("每次转入金额需在");
        expect(validateHajimiAutoTopup({ ...auto, dailyLimitMicro: 4_000_000 }, limits)).toBe("每日上限不能小于每次转入金额");
        expect(validateHajimiAutoTopup({ ...auto, amountMicro: 500_000 }, null)).toBe("");
        // 关闭自动转入时不受 hajimi 当前单笔限额约束，但基本约束（与后端一致）仍然生效。
        expect(validateHajimiAutoTopup({ ...auto, enabled: false, amountMicro: 500_000, dailyLimitMicro: 500_000 }, limits)).toBe("");
        expect(validateHajimiAutoTopup({ ...auto, enabled: false, amountMicro: 300_000_000, dailyLimitMicro: 300_000_000 }, limits)).toBe("");
        expect(validateHajimiAutoTopup({ ...auto, enabled: false, dailyLimitMicro: 4_000_000 }, limits)).toBe("每日上限不能小于每次转入金额");
        expect(validateHajimiAutoTopup({ ...auto, enabled: false, thresholdMicro: -1 }, limits)).toBe("触发阈值不能小于 0");
        expect(validateHajimiAutoTopup({ ...auto, enabled: false, amountMicro: 0 }, limits)).toBe("每次转入金额必须大于 0");
    });
});

describe("insufficient-credits one-click prompt", () => {
    test("default amount rounds the shortfall up to whole USD, at least the minimum, capped by limits and balance", () => {
        expect(hajimiPromptAmountMicro({ shortfallMicro: 2_300_000, limits, balanceMicro: 30_000_000 })).toBe(3_000_000);
        expect(hajimiPromptAmountMicro({ shortfallMicro: 200_000, limits, balanceMicro: 30_000_000 })).toBe(1_000_000);
        expect(hajimiPromptAmountMicro({ shortfallMicro: undefined, limits, balanceMicro: 30_000_000 })).toBe(1_000_000);
        expect(hajimiPromptAmountMicro({ shortfallMicro: 500_000_000, limits, balanceMicro: 900_000_000 })).toBe(200_000_000);
        expect(hajimiPromptAmountMicro({ shortfallMicro: 9_000_000, limits: { ...limits, userDailyUsedMicro: 195_000_000 }, balanceMicro: 30_000_000 })).toBe(5_000_000);
        expect(hajimiPromptAmountMicro({ shortfallMicro: 9_000_000, limits, balanceMicro: 4_500_000 })).toBe(4_500_000);
        expect(hajimiPromptAmountMicro({ shortfallMicro: 9_000_000, limits, balanceMicro: 400_000 })).toBeNull();
    });

    test("only linked, enabled, reachable users with an active grant get the offer", () => {
        expect(hajimiPromptOffer(wallet())).toEqual({ amountMicro: 1_000_000, balanceMicro: 30_000_000 });
        expect(hajimiPromptOffer(wallet(), 3_200_000)?.amountMicro).toBe(4_000_000);
        expect(hajimiPromptOffer(wallet({ enabled: false }))).toBeNull();
        expect(hajimiPromptOffer(wallet({ linked: false }))).toBeNull();
        expect(hajimiPromptOffer(wallet({ reachable: false }))).toBeNull();
        expect(hajimiPromptOffer(wallet({ grant: "revoked" }))).toBeNull();
        expect(hajimiPromptOffer(wallet({ hajimiBalanceMicro: 0 }))).toBeNull();
        expect(hajimiPromptOffer(null)).toBeNull();
    });

    test("recognises canvas insufficient-credits rejections on generation writes only", () => {
        const insufficient = { method: "post", url: "/tasks", status: 400, reason: "invalid_argument", message: "积分不足，请先使用兑换码充值" };
        expect(isInsufficientCreditsResponse(insufficient)).toBe(true);
        expect(isInsufficientCreditsResponse({ ...insufficient, url: "/api/tasks/task_1/retry" })).toBe(true);
        expect(isInsufficientCreditsResponse({ ...insufficient, url: "/creation-runs/run_1/submit", message: "积分不足，请充值后重试" })).toBe(true);
        expect(isInsufficientCreditsResponse({ ...insufficient, message: "模型服务价格发生变化，当前积分余额不足" })).toBe(true);
        expect(isInsufficientCreditsResponse({ method: "post", url: "/agent/runs", reason: "insufficient_credits", status: 402 })).toBe(true);
        expect(isInsufficientCreditsResponse({ ...insufficient, method: "get" })).toBe(false);
        expect(isInsufficientCreditsResponse({ ...insufficient, url: "/admin/users/u1/credits", message: "用户可用积分不足，不能执行本次扣减" })).toBe(false);
        expect(isInsufficientCreditsResponse({ ...insufficient, url: "/hajimi-bridge/transfers" })).toBe(false);
        expect(isInsufficientCreditsResponse({ ...insufficient, message: "同时排队或运行的任务已达到上限" })).toBe(false);
        expect(isInsufficientCreditsResponse({ ...insufficient, status: 403, reason: "quota_exceeded", message: "账号素材数量已达到 100 个上限" })).toBe(false);
    });
});

describe("transfer idempotency keys", () => {
    test("generates 16-80 char keys from the allowed alphabet", () => {
        const first = newHajimiIdempotencyKey();
        const second = newHajimiIdempotencyKey();
        expect(isValidHajimiIdempotencyKey(first)).toBe(true);
        expect(first).not.toBe(second);
        expect(isValidHajimiIdempotencyKey("short")).toBe(false);
        expect(isValidHajimiIdempotencyKey("has space in it 1234")).toBe(false);
    });

    test("reuses the same key when the same action is retried after a failure, and rotates after success or a changed amount", async () => {
        let counter = 0;
        const keyring = createHajimiTransferKeyring(() => `key_${String(++counter).padStart(16, "0")}`);
        const sent: CreateHajimiTransferInput[] = [];
        let failNext = true;
        const send = async (input: CreateHajimiTransferInput) => {
            sent.push(input);
            if (failNext) {
                failNext = false;
                throw new ApiError("网络异常", { retryable: true });
            }
            return transfer({ amountMicro: input.amountMicro });
        };
        const intent = { direction: "in" as const, amountMicro: 5_000_000, source: "manual" as const };

        await expect(submitHajimiTransfer(keyring, intent, send)).rejects.toThrow("网络异常");
        await submitHajimiTransfer(keyring, intent, send);
        expect(sent[0].idempotencyKey).toBe(sent[1].idempotencyKey);
        expect(sent[1]).toMatchObject({ direction: "in", amountMicro: 5_000_000, source: "manual" });

        // 成功后同样金额的下一次点击是新的用户操作。
        await submitHajimiTransfer(keyring, intent, send);
        expect(sent[2].idempotencyKey).not.toBe(sent[1].idempotencyKey);

        // 失败后改了金额，不能复用旧键（服务端会把“同键不同单”判为冲突）。
        failNext = true;
        await expect(submitHajimiTransfer(keyring, intent, send)).rejects.toThrow();
        await submitHajimiTransfer(keyring, { ...intent, amountMicro: 6_000_000 }, send);
        expect(sent[4].idempotencyKey).not.toBe(sent[3].idempotencyKey);
        expect(sent.every((item) => isValidHajimiIdempotencyKey(item.idempotencyKey))).toBe(true);
    });
});

describe("hajimi bridge API contract", () => {
    test("uses the §4.4 paths, methods and bodies through the shared http client", async () => {
        const original = apiClient.request;
        const requests: Array<Record<string, unknown>> = [];
        apiClient.request = (async (config: Record<string, unknown>) => {
            requests.push(config);
            return { data: { code: 0, data: {}, msg: "ok" }, status: 200 };
        }) as typeof apiClient.request;
        try {
            const signal = new AbortController().signal;
            await getHajimiBridgeWallet(signal);
            await createHajimiTransfer({ direction: "in", amountMicro: 5_000_000, idempotencyKey: "hjb_0123456789abcdef", source: "prompt" });
            await listHajimiTransfers({ page: 2, pageSize: 20 });
            await updateHajimiAutoTopup({ enabled: true, thresholdMicro: 1_000_000, amountMicro: 5_000_000, dailyLimitMicro: 20_000_000 });
            await getAdminHajimiBridgeOverview();
            await retryAdminHajimiTransfer("yct_0123456789abcdef0123456789abcdef");
            expect(requests[0]).toMatchObject({ method: "get", url: "/hajimi-bridge/wallet", signal });
            expect(requests[1]).toMatchObject({ method: "post", url: "/hajimi-bridge/transfers", data: { direction: "in", amountMicro: 5_000_000, idempotencyKey: "hjb_0123456789abcdef", source: "prompt" } });
            expect(requests[2]).toMatchObject({ method: "get", url: "/hajimi-bridge/transfers", params: { page: 2, pageSize: 20 } });
            expect(requests[3]).toMatchObject({ method: "put", url: "/hajimi-bridge/auto-topup", data: { enabled: true, thresholdMicro: 1_000_000, amountMicro: 5_000_000, dailyLimitMicro: 20_000_000 } });
            expect(requests[4]).toMatchObject({ method: "get", url: "/admin/hajimi-bridge/overview" });
            expect(requests[5]).toMatchObject({ method: "post", url: "/admin/hajimi-bridge/transfers/yct_0123456789abcdef0123456789abcdef/retry" });
        } finally {
            apiClient.request = original;
        }
    });

    test("transfer writes throw on a failed envelope instead of returning a default", async () => {
        const original = apiClient.request;
        apiClient.request = (async () => ({ data: { code: 403, data: null, msg: "未关联 hajimi 账号", reason: "hajimi_not_linked" }, status: 200 })) as unknown as typeof apiClient.request;
        try {
            const error = await createHajimiTransfer({ direction: "in", amountMicro: 5_000_000, idempotencyKey: "hjb_0123456789abcdef" }).catch((reason: unknown) => reason);
            expect(error).toBeInstanceOf(ApiError);
            expect((error as ApiError).reason).toBe("hajimi_not_linked");
        } finally {
            apiClient.request = original;
        }
    });
});

describe("admin overview helpers", () => {
    test("labels hajimi reconcile summary keys in either casing and formats micro amounts as USD", () => {
        expect(remoteSummaryLabel("to_canvas_committed_micro")).toBe("已转入影策（hajimi 记录）");
        expect(remoteSummaryLabel("toCanvasCommittedMicro")).toBe("已转入影策（hajimi 记录）");
        expect(remoteSummaryLabel("max_seq")).toBe("最新序号");
        expect(remoteSummaryLabel("unknown_field")).toBe("unknown_field");
        expect(formatRemoteSummaryValue("to_hajimi_committed_micro", 2_500_000)).toBe("$2.50");
        expect(formatRemoteSummaryValue("committedCount", 1234)).toBe((1234).toLocaleString());
        expect(formatRemoteSummaryValue("x", null)).toBe("--");
    });
});

describe("OAuth brand and endpoint rules", () => {
    test("the OAuth slot is branded hajimi", () => {
        expect(OAUTH_PROVIDER_NAME).toBe("hajimi");
    });

    test("backend OAuth messages shown on the login page carry the hajimi brand", () => {
        expect(brandOAuthMessage("Linux.do 登录失败，请重试")).toBe("hajimi 登录失败，请重试");
        expect(brandOAuthMessage("LinuxDO 账号已被禁用")).toBe("hajimi 账号已被禁用");
        expect(brandOAuthMessage("请使用已绑定的Linux.do账号；Linux.do 未返回用户")).toBe("请使用已绑定的hajimi账号；hajimi 未返回用户");
        expect(brandOAuthMessage("已取消 hajimi 授权")).toBe("已取消 hajimi 授权");
        expect(brandOAuthMessage("登录状态已失效，请重新登录")).toBe("登录状态已失效，请重新登录");
        // 小写路径段不是展示名，保持原样。
        expect(brandOAuthMessage("回调地址应为 /oauth/linuxdo/callback")).toBe("回调地址应为 /oauth/linuxdo/callback");
    });

    test("authorization URL may be http only on loopback; server endpoints may be internal http", () => {
        expect(oauthAuthorizationURLAllowed("https://hajimi.chat/yingce/authorize")).toBe(true);
        expect(oauthAuthorizationURLAllowed("http://127.0.0.1:5173/yingce/authorize")).toBe(true);
        expect(oauthAuthorizationURLAllowed("http://localhost/yingce/authorize")).toBe(true);
        expect(oauthAuthorizationURLAllowed("http://[::1]:8080/yingce/authorize")).toBe(true);
        expect(oauthAuthorizationURLAllowed("http://hajimi-bridge:8080/yingce/authorize")).toBe(false);
        expect(oauthAuthorizationURLAllowed("ftp://hajimi.chat/")).toBe(false);
        expect(oauthServerEndpointURLAllowed("http://hajimi-bridge:8080/api/v1/yingce/oauth/token")).toBe(true);
        expect(oauthServerEndpointURLAllowed("not a url")).toBe(false);
        expect(oauthEndpointURLError({ authorizationUrl: "https://hajimi.chat/yingce/authorize", tokenUrl: "http://hajimi-bridge:8080/api/v1/yingce/oauth/token", userInfoUrl: "http://hajimi-bridge:8080/api/v1/yingce/oauth/userinfo" })).toBe("");
        expect(oauthEndpointURLError({ authorizationUrl: "http://hajimi.chat/yingce/authorize", tokenUrl: "https://a.test/t", userInfoUrl: "https://a.test/u" })).toContain("授权地址");
    });
});
