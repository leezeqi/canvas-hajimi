type Size = { width: number; height: number };

export type PromptTextSelection = { start: number; end: number };

export type InsertPromptReferenceOptions = {
    /** 光标前那段 “@…” 从这个位置起已经是一个完整引用（编辑器里的胶囊）时返回 true：保留它，新引用接在后面。 */
    isCompletedMentionAt?: (index: number) => boolean;
};

export function insertPromptReferenceAtSelection(value: string, selection: PromptTextSelection, token: string, options: InsertPromptReferenceOptions = {}) {
    let start = Math.min(value.length, Math.max(0, Math.min(selection.start, selection.end)));
    let end = Math.min(value.length, Math.max(start, Math.max(selection.start, selection.end)));
    if (start === end) {
        // 只有手打到一半的 “@图” 才会被新引用替换；已插入的引用后面紧跟文字（@音频1张三）不算没写完。
        const pendingMention = /(^|\s)@[^\s@，。！？、,.!?;:]*$/u.exec(value.slice(0, start));
        const pendingStart = pendingMention ? start - (pendingMention[0].length - pendingMention[1].length) : start;
        if (pendingMention && !options.isCompletedMentionAt?.(pendingStart)) start = pendingStart;
    }
    const before = value.slice(0, start);
    const after = value.slice(end);
    const prefix = before && !/\s$/u.test(before) ? " " : "";
    // 插在末尾也补空格，与 @ 菜单选中引用一致：光标不贴着引用，接着输入的文字不会和它连在一起。
    const suffix = /^\s/u.test(after) ? "" : " ";
    const insertText = `${prefix}${token}${suffix}`;
    const nextValue = `${before}${insertText}${after}`;
    const caret = before.length + insertText.length;
    return { value: nextValue, selection: { start: caret, end: caret } };
}

export const PROMPT_EDITOR_VIEWPORT_MARGIN = 24;

export function clampPromptEditorModalWidth(width: number, viewportWidth: number) {
    const maxWidth = Math.max(1, viewportWidth - PROMPT_EDITOR_VIEWPORT_MARGIN);
    const minWidth = Math.min(560, maxWidth);
    return Math.min(maxWidth, Math.max(minWidth, Math.round(width)));
}

export function clampPromptEditorModalSize(size: Size, viewport: Size): Size {
    const maxWidth = Math.max(1, viewport.width - PROMPT_EDITOR_VIEWPORT_MARGIN);
    const maxHeight = Math.max(1, viewport.height - PROMPT_EDITOR_VIEWPORT_MARGIN);
    const minWidth = Math.min(560, maxWidth);
    const minHeight = Math.min(320, maxHeight);
    return {
        width: Math.min(maxWidth, Math.max(minWidth, Math.round(size.width))),
        height: Math.min(maxHeight, Math.max(minHeight, Math.round(size.height))),
    };
}
