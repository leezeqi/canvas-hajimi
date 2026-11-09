import { useMutation, useQuery, type QueryClient } from "@tanstack/react-query";
import { useRef } from "react";

import { createHajimiTransferKeyring, submitHajimiTransfer, type HajimiTransferIntent, type HajimiTransferKeyring } from "@/lib/hajimi-bridge";
import { createHajimiTransfer, getHajimiBridgeWallet, listHajimiTransfers, type HajimiBridgeWallet } from "@/services/api/hajimi-bridge";
import { useUserStore } from "@/stores/use-user-store";

// 查询键按用户隔离，切换账号后不会串数据。
export const hajimiBridgeQueryRoot = (userId: string) => ["hajimi-bridge", userId] as const;
export const hajimiBridgeWalletQueryKey = (userId: string) => ["hajimi-bridge", userId, "wallet"] as const;
export const hajimiTransfersQueryKey = (userId: string, page: number, pageSize: number) => ["hajimi-bridge", userId, "transfers", page, pageSize] as const;

const WALLET_STALE_TIME_MS = 15_000;
const PENDING_POLL_MS = 5_000;

export function useHajimiBridgeWallet(enabled = true) {
    const userId = useUserStore((state) => state.user?.id) || "";
    return useQuery({
        queryKey: hajimiBridgeWalletQueryKey(userId),
        queryFn: ({ signal }) => getHajimiBridgeWallet(signal),
        enabled: enabled && Boolean(userId),
        staleTime: WALLET_STALE_TIME_MS,
        retry: false,
        refetchOnWindowFocus: false,
        // 有处理中的划转时短轮询，确认后余额和限额随之刷新。
        refetchInterval: (query) => (query.state.data?.pendingCount ? PENDING_POLL_MS : false),
    });
}

/** 积分不足提示等非组件场景按需读取（复用同一缓存）。 */
export function fetchHajimiBridgeWallet(queryClient: QueryClient, userId: string): Promise<HajimiBridgeWallet> {
    return queryClient.fetchQuery({ queryKey: hajimiBridgeWalletQueryKey(userId), queryFn: ({ signal }) => getHajimiBridgeWallet(signal), staleTime: WALLET_STALE_TIME_MS, retry: false });
}

export function useHajimiTransfers(page: number, pageSize: number, enabled = true) {
    const userId = useUserStore((state) => state.user?.id) || "";
    return useQuery({
        queryKey: hajimiTransfersQueryKey(userId, page, pageSize),
        queryFn: ({ signal }) => listHajimiTransfers({ page, pageSize }, signal),
        enabled: enabled && Boolean(userId),
        retry: false,
        refetchOnWindowFocus: false,
        placeholderData: (previous) => previous,
        refetchInterval: (query) => (query.state.data?.items.some((item) => item.status === "pending") ? PENDING_POLL_MS : false),
    });
}

/**
 * 划转后刷新：hajimi 互通的全部查询，以及工作区共享的积分余额（wallet:updated 由
 * useWalletBalance 等监听）。积分中心弹窗自己的余额由调用方另行刷新。
 */
export async function announceHajimiCreditsChanged(queryClient: QueryClient, userId: string) {
    window.dispatchEvent(new CustomEvent("wallet:updated"));
    await Promise.all([queryClient.invalidateQueries({ queryKey: hajimiBridgeQueryRoot(userId) }), queryClient.invalidateQueries({ queryKey: ["wallet-balance"] })]);
}

/** 一次划转操作：失败后重试同一金额复用幂等键，成功后下一次操作换新键。 */
export function useHajimiTransferAction() {
    const keyring = useRef<HajimiTransferKeyring | null>(null);
    keyring.current ??= createHajimiTransferKeyring();
    return useMutation({
        mutationFn: (intent: HajimiTransferIntent) => submitHajimiTransfer(keyring.current!, intent, createHajimiTransfer),
    });
}
