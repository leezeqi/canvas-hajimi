import { afterAll, beforeAll, describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { Window } from "happy-dom";
import { renderToStaticMarkup } from "react-dom/server";

import { IdentityProviderBadge } from "../src/components/layout/identity-provider-badge";
import { i18n } from "../src/i18n";
import { SUPPORTED_LOCALES } from "../src/i18n/locales";
import { useLocaleStore } from "../src/stores/use-locale-store";

const source = (path: string) => readFileSync(resolve(import.meta.dir, path), "utf8");
const LEGACY = /Linux(?:\.do|\.DO|DO|Do)(?![A-Za-z0-9_])/;

function strings(value: unknown, path = ""): [string, string][] {
    if (typeof value === "string") return [[path, value]];
    if (value && typeof value === "object") return Object.entries(value).flatMap(([key, item]) => strings(item, path ? `${path}.${key}` : key));
    return [];
}

beforeAll(async () => {
    useLocaleStore.getState().setLocale("zh-CN");
    await i18n.changeLanguage("zh-CN");
});

describe("二改 i18n：OAuth 槽位以 hajimi 展示", () => {
    test("所有界面语言里都不再出现 Linux.do，并提供二改新增文案", () => {
        for (const { code } of SUPPORTED_LOCALES) {
            const bundle = i18n.getResourceBundle(code, "translation");
            const leftovers = strings(bundle).filter(([, text]) => LEGACY.test(text));
            expect(leftovers).toEqual([]);
            const note = i18n.getFixedT(code)("forkHajimi.accountNote", { brand: "影策", provider: "hajimi" });
            expect(note).toContain("hajimi");
            expect(note).not.toContain("{{");
            expect(i18n.getFixedT(code)("forkHajimi.walletTab", { provider: "hajimi" })).toContain("hajimi");
        }
        expect(i18n.t("auth.login.linuxdo")).toBe("使用 hajimi 登录");
        expect(i18n.t("account.settings.profile.verifyLinuxdo")).toBe("使用 hajimi 验证");
        expect(i18n.t("account.layout.identity.linuxdoUser")).toBe("hajimi 用户");
    });

    test("身份徽标显示 hajimi", () => {
        const html = renderToStaticMarkup(<IdentityProviderBadge user={{ identityProvider: "linuxdo", identityUsername: "neko" }} />);
        expect(html).toContain('title="hajimi · @neko"');
        expect(html).toContain(">hajimi</span>");
        expect(renderToStaticMarkup(<IdentityProviderBadge user={{ identityProvider: "linuxdo" }} compact />)).toContain(">h</span>");
        expect(renderToStaticMarkup(<IdentityProviderBadge user={{ identityProvider: "github" }} />)).toBe("");
    });

    test("oauth_error 在独立登录页和弹窗登录入口都先做品牌替换再展示", () => {
        expect(source("../src/pages/auth/auth-route-panel.tsx")).toContain("message.error(brandOAuthMessage(oauthError))");
        expect(source("../src/pages/auth/auth-scene.tsx")).toContain("message.error(brandOAuthMessage(oauthError))");
    });
});

// 整页渲染：设置接口开启第三方登录后，账号密码表单与 hajimi 入口同时出现。
const dom = new Window({ url: "http://localhost/login?next=/canvas" });
const originals = new Map<string, PropertyDescriptor | undefined>();
for (const [key, value] of Object.entries({
    window: dom,
    document: dom.document,
    navigator: dom.navigator,
    localStorage: dom.localStorage,
    sessionStorage: dom.sessionStorage,
    HTMLElement: dom.HTMLElement,
    Element: dom.Element,
    Node: dom.Node,
    SVGElement: dom.SVGElement,
    getComputedStyle: dom.getComputedStyle.bind(dom),
    matchMedia: dom.matchMedia.bind(dom),
    IS_REACT_ACT_ENVIRONMENT: true,
})) {
    originals.set(key, Object.getOwnPropertyDescriptor(globalThis, key));
    Object.defineProperty(globalThis, key, { configurable: true, writable: true, value });
}

afterAll(() => {
    dom.happyDOM.abort();
    for (const [key, value] of originals) {
        if (value) Object.defineProperty(globalThis, key, value);
        else Reflect.deleteProperty(globalThis, key);
    }
});

async function renderLoginPage(path: string) {
    const { act } = await import("react");
    const { createRoot } = await import("react-dom/client");
    const { MemoryRouter } = await import("react-router");
    const { App } = await import("antd");
    const { apiClient } = await import("../src/services/api/request");
    const Page = (await import("../src/pages/auth/login")).default;
    const original = apiClient.request;
    const requests: string[] = [];
    apiClient.request = (async (config: { url?: string }) => {
        requests.push(String(config.url));
        if (config.url === "/auth/settings") {
            return {
                status: 200,
                data: {
                    code: 0,
                    msg: "ok",
                    data: {
                        firstUser: false,
                        registrationEnabled: true,
                        linuxdoEnabled: true,
                        emailEnabled: true,
                        emailCodeRequired: true,
                        smsBindingAvailable: false,
                        emailBindingAvailable: true,
                        agreementTitle: "《测试服务协议》",
                        agreementContent: "第一条\n\n第二条",
                        loginMode: "page",
                    },
                },
            };
        }
        throw new Error(`unexpected request ${config.url}`);
    }) as unknown as typeof apiClient.request;
    const host = document.createElement("div");
    document.body.append(host);
    const root = createRoot(host);
    try {
        await act(async () => {
            root.render(
                <MemoryRouter initialEntries={[path]}>
                    <App>
                        <Page />
                    </App>
                </MemoryRouter>,
            );
        });
        await act(async () => {
            await new Promise((resolveTimer) => setTimeout(resolveTimer, 0));
        });
        return { html: host.innerHTML, requests };
    } finally {
        await act(async () => root.unmount());
        host.remove();
        apiClient.request = original;
    }
}

describe("登录页：原生账号表单与 hajimi 入口并存", () => {
    test("账号密码表单保留，hajimi 按钮在其后，并说明两种账号相互独立", async () => {
        const { html, requests } = await renderLoginPage("/login?next=/canvas");
        expect(requests).toContain("/auth/settings");
        expect(html).toContain('autocomplete="current-password"');
        expect(html).toContain("使用 hajimi 登录");
        expect(html).toContain("账号与 hajimi 账号相互独立");
        expect(html).not.toMatch(LEGACY);
        expect(html.indexOf('autocomplete="current-password"')).toBeLessThan(html.indexOf("使用 hajimi 登录"));
        // 沿用上游的同意规则：未勾选协议时不携带同意标志，新账号需先勾选协议。
        expect(html).toContain('href="/api/auth/linuxdo/start?next=%2Fcanvas"');
    });
});
