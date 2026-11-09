import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { renderToStaticMarkup } from "react-dom/server";

import { HajimiBridgeWalletView, HajimiTransferRow } from "../src/components/layout/hajimi-bridge-wallet-panel";
import type { HajimiBridgeWallet, HajimiTransfer } from "../src/services/api/hajimi-bridge";

const baseWallet: HajimiBridgeWallet = {
    enabled: true,
    linked: true,
    reachable: true,
    hajimiPublicUrl: "https://hajimi.example.test",
    hajimiBalanceMicro: 30_000_000,
    netInMicro: 12_000_000,
    grant: "active",
    limits: { minMicro: 1_000_000, maxMicro: 200_000_000, userDailyMicro: 200_000_000, userDailyUsedMicro: 5_000_000 },
    localNetInMicro: 10_000_000,
    autoTopup: { enabled: false, thresholdMicro: 1_000_000, amountMicro: 5_000_000, dailyLimitMicro: 20_000_000 },
    pendingCount: 0,
};

const transfer: HajimiTransfer = { id: "yct_0123456789abcdef0123456789abcdef", direction: "in", amountMicro: 5_000_000, status: "rejected", reason: "user_daily_limit", source: "prompt", createdAt: "2026-11-08T00:00:00Z", completedAt: null };

const noop = async () => {
    throw new Error("not used in static render");
};

function render(wallet: HajimiBridgeWallet | null, availableMicro: number | null = 50_000_000) {
    return renderToStaticMarkup(<HajimiBridgeWalletView wallet={wallet} availableMicro={availableMicro} transfers={{ items: [transfer], total: 1 }} onTransfer={noop} onSaveAutoTopup={noop} />);
}

describe("hajimi wallet card", () => {
    test("renders nothing when the bridge is disabled", () => {
        expect(render({ ...baseWallet, enabled: false })).toBe("");
        expect(render(null)).toBe("");
    });

    test("shows only the login hint when the account is not linked", () => {
        const html = render({ ...baseWallet, linked: false });
        expect(html).toContain("使用 hajimi 账号登录的影策账号才能转入 hajimi 余额");
        expect(html).toContain('data-hajimi-state="unlinked"');
        expect(html).not.toContain("转回 hajimi");
        expect(html).not.toContain("自动转入");
    });

    test("shows the unreachable notice and disables money movement when hajimi is down", () => {
        const html = render({ ...baseWallet, reachable: false, hajimiBalanceMicro: null, netInMicro: null, grant: null, limits: null });
        expect(html).toContain('data-hajimi-state="unreachable"');
        expect(html).toContain("hajimi 暂时无法连接，稍后再试");
        expect(html.match(/<input[^>]*aria-label="转入金额（美元）"[^>]*>/)?.[0]).toContain('disabled=""');
        expect(html.match(/<input[^>]*aria-label="转回金额（美元）"[^>]*>/)?.[0]).toContain('disabled=""');
        // hajimi 不可达时余额未知，不能显示成 $0。
        expect(html).not.toContain("$0.00");
    });

    test("linked card shows balance, presets, withdrawable, auto top-up, history and the recharge link", () => {
        const html = render(baseWallet);
        expect(html).toContain('data-hajimi-state="linked"');
        expect(html).toContain("hajimi 余额");
        expect(html).toContain("$30.00");
        for (const preset of ["$5", "$10", "$20", "$50"]) expect(html).toContain(`>${preset}</button>`);
        expect(html).toContain("转回 hajimi");
        // 可转回 = min(本地净转入 10, hajimi 净转入 12, 可用 50)
        expect(html).toContain("可转回 $10.00");
        expect(html).toContain("自动转入");
        expect(html).toContain('aria-label="开启自动转入"');
        expect(html).toContain("去 hajimi 充值");
        expect(html).toContain('href="https://hajimi.example.test"');
        expect(html).toContain('rel="noopener noreferrer"');
        expect(html).toContain("被拒绝");
        expect(html).toContain("已达今日转入上限");
        expect(html).toContain("积分不足提示");
    });

    test("revoked grant asks the user to log in with hajimi again", () => {
        const html = render({ ...baseWallet, grant: "revoked" });
        expect(html).toContain("请重新使用 hajimi 账号登录授权");
    });

    test("withdrawable falls back to unknown until local credits load", () => {
        const html = render(baseWallet, null);
        expect(html).toContain("可转回 --");
    });

    test("transfer rows label direction, status and amount", () => {
        const html = renderToStaticMarkup(<HajimiTransferRow transfer={{ ...transfer, direction: "out", status: "completed", reason: "", source: "manual" }} />);
        expect(html).toContain("转回 hajimi");
        expect(html).toContain("已转回");
        expect(html).toContain("-$5.00");
    });
});

describe("wallet modal and prompt wiring", () => {
    test("wallet modal adds the hajimi tab only when the bridge is enabled and refreshes credits after transfers", () => {
        const modal = readFileSync(resolve(import.meta.dir, "../src/components/layout/workspace-wallet-modal.tsx"), "utf8");
        expect(modal).toContain("const hajimiTabVisible = Boolean(hajimiBridge.data?.enabled);");
        expect(modal).toContain("{hajimiTabVisible ? (");
        expect(modal).toContain("<HajimiBridgeWalletPanel availableMicro={wallet ? available : null} onCreditsChanged={announceWalletUpdated} />");
        expect(modal).toContain("hajimiLedgerTitle(entry.type) || title");
    });

    test("the insufficient-credits prompt is mounted in the workspace shell and transfers with source prompt", () => {
        const shell = readFileSync(resolve(import.meta.dir, "../src/components/layout/app-top-nav.tsx"), "utf8");
        const notice = readFileSync(resolve(import.meta.dir, "../src/components/layout/hajimi-bridge-credits-notice.tsx"), "utf8");
        expect(shell).toContain("<HajimiBridgeCreditsNotice />");
        expect(notice).toContain('source: "prompt"');
        expect(notice).toContain("hajimiPromptOffer(wallet)");
        expect(notice).toContain("return Promise.reject(error);");
        expect(notice).toContain("apiClient.interceptors.response.eject(interceptor)");
    });
});
