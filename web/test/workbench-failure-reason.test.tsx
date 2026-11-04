import { expect, test } from "bun:test";

test("upstream public-link rejection is translated without exposing its raw diagnostic", async () => {
    const { generationErrorMessage } = await import("@/lib/generation-error");
    const message = generationErrorMessage('模型服务拒绝了请求；上游：{"message":"Channel 177 requires public HTTPS reference URLs at https://private.example/?token=secret"}');
    expect(message).toContain("公网 HTTPS 链接");
    expect(message).not.toContain("private.example");
    expect(message).not.toContain("token=secret");
});
