import { useQueryClient } from "@tanstack/react-query";
import { App, Button } from "antd";
import axios from "axios";
import { useEffect, useRef, useState } from "react";

import { announceHajimiCreditsChanged, fetchHajimiBridgeWallet } from "@/hooks/use-hajimi-bridge";
import { createHajimiTransferKeyring, formatHajimiUsd, hajimiPromptOffer, hajimiTransferErrorMessage, hajimiTransferOutcome, isInsufficientCreditsResponse, submitHajimiTransfer } from "@/lib/hajimi-bridge";
import { openWorkspaceWallet } from "@/lib/workspace-wallet";
import { createHajimiTransfer } from "@/services/api/hajimi-bridge";
import { apiClient } from "@/services/api/request";
import { useUserStore } from "@/stores/use-user-store";

const NOTICE_KEY = "hajimi-bridge-insufficient-credits";
const NOTICE_INTERVAL_MS = 20_000;

/**
 * 任一生成类请求因影策积分不足被拒时，若当前账号已关联 hajimi 且互通可用，
 * 补充一个“从 hajimi 余额转入”的一键入口。调用方原有的错误提示保持不变；
 * 未开启、未关联或 hajimi 不可达时什么也不做。
 */
export function HajimiBridgeCreditsNotice() {
    const { message, notification } = App.useApp();
    const queryClient = useQueryClient();
    const userId = useUserStore((state) => state.user?.id) || "";
    const lastOfferAt = useRef(0);
    // 同一次提示里的重试复用幂等键；转入成功后才换新键。
    const keyring = useRef(createHajimiTransferKeyring());

    useEffect(() => {
        if (!userId) return;

        const transfer = async (amountMicro: number) => {
            try {
                const result = await submitHajimiTransfer(keyring.current, { direction: "in", amountMicro, source: "prompt" }, createHajimiTransfer);
                notification.destroy(NOTICE_KEY);
                const outcome = hajimiTransferOutcome(result);
                message[outcome.tone](outcome.tone === "success" ? `${outcome.text}，请重新提交` : outcome.text);
                await announceHajimiCreditsChanged(queryClient, userId);
            } catch (error) {
                message.error(hajimiTransferErrorMessage(error));
            }
        };

        const offer = async () => {
            const now = Date.now();
            if (now - lastOfferAt.current < NOTICE_INTERVAL_MS) return;
            lastOfferAt.current = now;
            let wallet;
            try {
                wallet = await fetchHajimiBridgeWallet(queryClient, userId);
            } catch {
                // 读取失败等同于不可用：保持原有积分不足提示，不额外打扰。
                return;
            }
            const prompt = hajimiPromptOffer(wallet);
            if (!prompt) return;
            notification.info({
                key: NOTICE_KEY,
                title: "积分不足",
                description: `可从 hajimi 余额（${formatHajimiUsd(prompt.balanceMicro)}）转入影策积分，1 美元 = 1 积分。转入后请重新提交。`,
                duration: 0,
                actions: (
                    <HajimiPromptActions
                        amountMicro={prompt.amountMicro}
                        onTransfer={() => transfer(prompt.amountMicro)}
                        onMore={() => {
                            notification.destroy(NOTICE_KEY);
                            openWorkspaceWallet({ tab: "hajimi" });
                        }}
                    />
                ),
            });
        };

        const inspect = (input: Parameters<typeof isInsufficientCreditsResponse>[0]) => {
            if (isInsufficientCreditsResponse(input)) void offer();
        };

        const interceptor = apiClient.interceptors.response.use(
            (response) => {
                // HTTP 200 但业务 code 非 0 的信封同样按失败处理（request.ts 在之后抛错）。
                const data = response.data as { code?: unknown; msg?: unknown; reason?: unknown } | undefined;
                if (data && typeof data.code === "number" && data.code !== 0) {
                    inspect({ method: response.config.method, url: response.config.url, status: data.code, reason: typeof data.reason === "string" ? data.reason : undefined, message: typeof data.msg === "string" ? data.msg : undefined });
                }
                return response;
            },
            (error: unknown) => {
                if (axios.isAxiosError<{ msg?: string; reason?: string }>(error) && error.response) {
                    inspect({ method: error.config?.method, url: error.config?.url, status: error.response.status, reason: error.response.data?.reason, message: error.response.data?.msg });
                }
                return Promise.reject(error);
            },
        );
        return () => apiClient.interceptors.response.eject(interceptor);
    }, [message, notification, queryClient, userId]);

    return null;
}

function HajimiPromptActions({ amountMicro, onTransfer, onMore }: { amountMicro: number; onTransfer: () => Promise<void>; onMore: () => void }) {
    const [transferring, setTransferring] = useState(false);
    return (
        <span style={{ display: "inline-flex", gap: "var(--space-2, 8px)" }}>
            <Button size="small" disabled={transferring} onClick={onMore}>
                其他金额
            </Button>
            <Button
                size="small"
                type="primary"
                loading={transferring}
                onClick={async () => {
                    setTransferring(true);
                    try {
                        await onTransfer();
                    } finally {
                        setTransferring(false);
                    }
                }}
            >
                从 hajimi 余额转入 {formatHajimiUsd(amountMicro)}
            </Button>
        </span>
    );
}
