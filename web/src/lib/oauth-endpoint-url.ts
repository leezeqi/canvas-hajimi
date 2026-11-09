/**
 * 后台 OAuth 槽位的地址校验（与后端规则同口径）：
 * - 授权地址由浏览器打开：必须 HTTPS，只有本机回环地址可用 HTTP（本地联调）。
 * - Token / 用户资料地址由后端直连：HTTPS 任意主机；HTTP 只允许回环地址或
 *   `CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS` 精确放行的主机（例如内部桥接网络）。
 *   放行名单只存在于服务端，前端无法得知，因此 HTTP 端点在这里放过，由保存接口最终裁决。
 */
export function isLoopbackHostname(hostname: string) {
    const host = hostname
        .trim()
        .toLowerCase()
        .replace(/^\[|\]$/g, "");
    if (host === "localhost" || host.endsWith(".localhost") || host === "::1") return true;
    return /^127(?:\.\d{1,3}){3}$/.test(host);
}

function parseURL(value: string) {
    try {
        const parsed = new URL(value.trim());
        return parsed.host ? parsed : null;
    } catch {
        return null;
    }
}

export function oauthAuthorizationURLAllowed(value: string) {
    const parsed = parseURL(value);
    if (!parsed) return false;
    if (parsed.protocol === "https:") return true;
    return parsed.protocol === "http:" && isLoopbackHostname(parsed.hostname);
}

export function oauthServerEndpointURLAllowed(value: string) {
    const parsed = parseURL(value);
    if (!parsed) return false;
    return parsed.protocol === "https:" || parsed.protocol === "http:";
}

/** 返回第一条不合规的说明；全部合规时返回空串。 */
export function oauthEndpointURLError(input: { authorizationUrl: string; tokenUrl: string; userInfoUrl: string }) {
    if (!oauthAuthorizationURLAllowed(input.authorizationUrl)) return "授权地址必须是有效的 HTTPS URL（仅本机回环地址可使用 HTTP）";
    if (!oauthServerEndpointURLAllowed(input.tokenUrl) || !oauthServerEndpointURLAllowed(input.userInfoUrl)) return "Token 和用户资料地址必须是有效的 HTTP(S) URL；HTTP 仅限回环地址或后端已放行的内网主机";
    return "";
}
