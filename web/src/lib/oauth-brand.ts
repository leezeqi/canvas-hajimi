/**
 * 第三方登录入口的品牌名（二改：hajimi 互通）。
 *
 * 后端仍沿用 `linuxdo` 这个 OAuth 槽位（接口路径、身份 provider 键、设置项名称都不变），
 * 但本仓库把它对接到 hajimi。界面文案里的“Linux.do”由 i18n/fork-overrides.ts 统一替换，
 * 页面代码里需要品牌名时从这里取，不要再写死第三方名称。
 */
export const OAUTH_PROVIDER_NAME = "hajimi";

// 后端 OAuth 槽位沿用 linuxdo，部分错误文案仍写“Linux.do”。只替换展示名，
// 不动小写的路径段（如 /auth/linuxdo/start）。
const LEGACY_PROVIDER_LABEL = /Linux(?:\.do|\.DO|DO|Do)(?![A-Za-z0-9_])/g;

/** 展示后端 OAuth 文案（oauth_error 等）或界面目录文案前，统一换成当前品牌名。 */
export function brandOAuthMessage(message: string) {
    return message.replace(LEGACY_PROVIDER_LABEL, OAUTH_PROVIDER_NAME);
}
