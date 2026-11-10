import { describe, expect, test } from "bun:test";
import { clampPromptEditorModalSize, clampPromptEditorModalWidth, insertPromptReferenceAtSelection } from "../src/lib/canvas/canvas-prompt-editor-size";

describe("prompt editor window bounds", () => {
    test("allows independent width dragging within the viewport", () => {
        expect(clampPromptEditorModalWidth(900, 1440)).toBe(900);
        expect(clampPromptEditorModalWidth(2000, 1440)).toBe(1416);
        expect(clampPromptEditorModalWidth(100, 390)).toBe(366);
        expect(clampPromptEditorModalSize({ width: 900, height: 520 }, { width: 1440, height: 900 })).toEqual({ width: 900, height: 520 });
    });

    test("inserts a reference at the caret and replaces selected text", () => {
        expect(insertPromptReferenceAtSelection("左边右边", { start: 2, end: 2 }, "@[node:image-1]")).toEqual({
            value: "左边 @[node:image-1] 右边",
            selection: { start: 19, end: 19 },
        });
        expect(insertPromptReferenceAtSelection("前面替换后面", { start: 2, end: 4 }, "@[node:image-1]")).toEqual({
            value: "前面 @[node:image-1] 后面",
            selection: { start: 19, end: 19 },
        });
        expect(insertPromptReferenceAtSelection("前面  后面", { start: 4, end: 4 }, "@[node:image-1]").value).toBe("前面  @[node:image-1] 后面");
    });

    test("leaves a separator after a reference inserted at the end", () => {
        expect(insertPromptReferenceAtSelection("镜头里他说：你好", { start: 8, end: 8 }, "@音频1")).toEqual({
            value: "镜头里他说：你好 @音频1 ",
            selection: { start: 14, end: 14 },
        });
        expect(insertPromptReferenceAtSelection("", { start: 0, end: 0 }, "@音频1").value).toBe("@音频1 ");
    });

    test("replaces a half-typed mention but keeps a completed reference and the text typed after it", () => {
        expect(insertPromptReferenceAtSelection("镜头 @图", { start: 5, end: 5 }, "@图片1").value).toBe("镜头 @图片1 ");
        const prompt = "@图片1 走过来 @音频1张三";
        const completed = (index: number) => prompt.startsWith("@音频1", index);
        expect(insertPromptReferenceAtSelection(prompt, { start: prompt.length, end: prompt.length }, "@音频2", { isCompletedMentionAt: completed }).value).toBe("@图片1 走过来 @音频1张三 @音频2 ");
        expect(insertPromptReferenceAtSelection("@图片1", { start: 4, end: 4 }, "@音频1", { isCompletedMentionAt: (index) => index === 0 }).value).toBe("@图片1 @音频1 ");
    });

    test("keeps desktop minimums and leaves a viewport margin", () => {
        const viewport = { width: 1440, height: 900 };
        expect(clampPromptEditorModalSize({ width: 100, height: 100 }, viewport)).toEqual({ width: 560, height: 320 });
        expect(clampPromptEditorModalSize({ width: 2000, height: 2000 }, viewport)).toEqual({ width: 1416, height: 876 });
    });

    test("a narrow or short viewport takes precedence over desktop minimums", () => {
        expect(clampPromptEditorModalSize({ width: 1200, height: 800 }, { width: 390, height: 844 })).toEqual({ width: 366, height: 800 });
        expect(clampPromptEditorModalSize({ width: 1200, height: 800 }, { width: 844, height: 300 })).toEqual({ width: 820, height: 276 });
    });

    test("reconstrains a previously resized window after viewport shrink", () => {
        const desktop = clampPromptEditorModalSize({ width: 1200, height: 800 }, { width: 1440, height: 900 });
        const mobile = clampPromptEditorModalSize(desktop, { width: 390, height: 300 });
        expect(mobile).toEqual({ width: 366, height: 276 });
        expect(clampPromptEditorModalSize(mobile, { width: 390, height: 300 })).toEqual(mobile);
    });
});
