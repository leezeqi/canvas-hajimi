import { describe, expect, test } from "bun:test";
import { CONTENT_MODERATION_MESSAGE, generationErrorMessage, generationFailureMetadata } from "../src/lib/generation-error";

describe("safe upstream error details", () => {
    const detail = "非常抱歉，生成的图片可能违反了关于裸露、色情或情色内容的防护限制。请重试或修改提示语。";
    test("keeps a safe upstream reason on the card", () => {
        const message = `模型服务拒绝了请求，请检查模型和参数；上游：${detail}`;
        expect(generationErrorMessage(new Error(message))).toBe(message);
        const english = "模型服务拒绝了请求，请检查模型和参数；上游：Invalid size '1536x2048'. Supported sizes are 1024x1024, 1024x1536";
        expect(generationErrorMessage(english)).toBe(english);
    });
    test("does not replace a safe upstream message with a generic HTTP hint", () => {
        const message = `模型服务暂时不可用（HTTP 500）；上游：${detail}`;
        expect(generationErrorMessage(message)).toBe(`模型服务暂时不可用，请稍后再试；上游：${detail}`);
        expect(generationErrorMessage("模型服务暂时不可用（HTTP 500）")).toBe("模型服务暂时不可用，请稍后再试。");
    });
    test("showing the reason again does not change it", () => {
        const shown = generationErrorMessage("模型服务暂时不可用（HTTP 500）；上游：Upstream gateway error");
        expect(shown).toBe("模型服务暂时不可用，请稍后再试；上游：Upstream gateway error");
        expect(generationErrorMessage(shown)).toBe(shown);
    });
    test("filters sensitive upstream reasons", () => {
        expect(generationErrorMessage("模型服务鉴权失败，请检查 API Key 和模型权限；上游：Incorrect API key provided: sk-live-abc123")).toBe("模型服务鉴权失败，请检查 API Key 和模型权限");
        expect(generationErrorMessage("模型服务请求失败；上游：You exceeded your current quota")).toBe("模型服务请求失败");
        expect(generationErrorMessage("模型服务暂时不可用（HTTP 502）；上游：<html><body>502 Bad Gateway</body></html>")).toBe("模型服务暂时不可用，请稍后再试。");
    });
    test("extracts historical JSON reasons and hides addresses", () => {
        const message = generationErrorMessage('模型服务拒绝了请求；上游：{"error":{"message":"duration must be 5 or 10 seconds, see https://docs.example/video for task_abc123"}}');
        expect(message).toBe("模型服务拒绝了请求；上游：duration must be 5 or 10 seconds, see [链接已隐藏] for [任务标识已隐藏]");
    });
    test("truncates long upstream reasons", () => {
        const message = generationErrorMessage(`模型服务拒绝了请求；上游：${"x".repeat(500)}`);
        expect(message.startsWith("模型服务拒绝了请求；上游：x")).toBe(true);
        expect(message.endsWith("...")).toBe(true);
        expect(Array.from(message).length).toBe(Array.from("模型服务拒绝了请求；上游：").length + 300 + 3);
    });
    test("keeps details when moderation metadata is stored", () => {
        const message = `内容审核未通过；上游：${detail}`;
        const metadata = generationFailureMetadata(message, "test prompt");
        expect(metadata.errorDetails).toBe(CONTENT_MODERATION_MESSAGE);
        expect(metadata.generationErrorCode).toBe("sensitive_words_detected");
    });
    test("gives actionable hints for known upstream failures", () => {
        expect(generationErrorMessage(`模型服务暂时不可用（HTTP 503）；上游：model_not_found request id abc`)).toContain("更换模型");
        expect(generationErrorMessage(`模型服务拒绝请求；上游：image file too large 25MB`)).toContain("压缩图片");
    });
    test("upstream marker does not bypass URL hiding", () => {
        expect(generationErrorMessage("模型服务请求失败；上游：https://private.test")).toBe("模型服务请求失败");
    });
});
