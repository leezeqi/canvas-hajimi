import type { i18n as I18nInstance } from "i18next";

import { brandOAuthMessage } from "@/lib/oauth-brand";
import { SUPPORTED_LOCALES, type AppLocale } from "./locales";

/**
 * 二改（hajimi 互通）的界面文案，集中在这一个文件里，不改上游各语言目录：
 * 1. 第三方登录槽位沿用上游的 linuxdo 键名，但本仓库对接的是 hajimi，所以把所有语言里展示用的
 *    “Linux.do / LinuxDO” 统一换成 hajimi（规则与 oauth_error 展示相同，见 brandOAuthMessage）；
 * 2. 追加 forkHajimi 命名空间下的少量新文案。{{brand}} 是站点品牌名，{{provider}} 是 hajimi。
 */
const FORK_MESSAGES: Record<AppLocale, Record<string, string>> = {
    "zh-CN": {
        accountNote: "{{brand}}账号与 {{provider}} 账号相互独立；使用 {{provider}} 账号登录后，可将 {{provider}} 余额转入{{brand}}积分。",
        walletTab: "{{provider}} 余额",
    },
    "en-US": {
        accountNote: "Your {{brand}} account and your {{provider}} account are separate. After signing in with {{provider}}, you can transfer your {{provider}} balance into {{brand}} credits.",
        walletTab: "{{provider}} balance",
    },
    "fr-FR": {
        accountNote: "Votre compte {{brand}} et votre compte {{provider}} sont distincts. Après une connexion avec {{provider}}, vous pouvez transférer votre solde {{provider}} en crédits {{brand}}.",
        walletTab: "Solde {{provider}}",
    },
    "de-DE": {
        accountNote: "Ihr {{brand}}-Konto und Ihr {{provider}}-Konto sind voneinander getrennt. Nach der Anmeldung mit {{provider}} können Sie Ihr {{provider}}-Guthaben in {{brand}}-Credits übertragen.",
        walletTab: "{{provider}}-Guthaben",
    },
    "ja-JP": {
        accountNote: "{{brand}} アカウントと {{provider}} アカウントは別々のアカウントです。{{provider}} でログインすると、{{provider}} の残高を {{brand}} のクレジットに移せます。",
        walletTab: "{{provider}} 残高",
    },
    "ko-KR": {
        accountNote: "{{brand}} 계정과 {{provider}} 계정은 서로 별개입니다. {{provider}}(으)로 로그인하면 {{provider}} 잔액을 {{brand}} 크레딧으로 옮길 수 있습니다.",
        walletTab: "{{provider}} 잔액",
    },
};

function rebrand(value: unknown): unknown {
    if (typeof value === "string") return brandOAuthMessage(value);
    if (Array.isArray(value)) return value.map(rebrand);
    if (value && typeof value === "object") return Object.fromEntries(Object.entries(value).map(([key, item]) => [key, rebrand(item)]));
    return value;
}

/** 在上游资源加载后调用一次；只覆盖文案，不改键名，未改动的条目保持上游原样。 */
export function applyForkI18nOverrides(instance: I18nInstance) {
    for (const { code } of SUPPORTED_LOCALES) {
        const bundle = instance.getResourceBundle(code, "translation") as Record<string, unknown> | undefined;
        if (bundle) instance.addResourceBundle(code, "translation", rebrand(bundle), true, true);
        instance.addResourceBundle(code, "translation", { forkHajimi: FORK_MESSAGES[code] }, true, true);
    }
}
