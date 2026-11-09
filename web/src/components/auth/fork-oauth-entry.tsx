import { useTranslation } from "react-i18next";

import { OAUTH_PROVIDER_NAME } from "@/lib/oauth-brand";
import { useAppearanceStore } from "@/stores/use-appearance-store";

/** 二改：第三方（hajimi）登录按钮的图标，品牌首字母徽标，颜色走主题 token。 */
export function OAuthProviderIcon() {
    return (
        <span aria-hidden className="inline-flex size-5 shrink-0 items-center justify-center rounded-full text-[11px] font-semibold leading-none" style={{ background: "var(--foreground)", color: "var(--background)" }}>
            {OAUTH_PROVIDER_NAME.slice(0, 1)}
        </span>
    );
}

/** 二改：说明站内账号与 hajimi 账号相互独立，显示在 hajimi 登录按钮下方。 */
export function OAuthAccountExplanation() {
    const { t } = useTranslation();
    const brandName = useAppearanceStore((state) => state.appearance.brandName) || t("auth.register.platform");
    return <p className="auth-form-switch">{t("forkHajimi.accountNote", { brand: brandName, provider: OAUTH_PROVIDER_NAME })}</p>;
}
