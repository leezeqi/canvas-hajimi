# 仙凤API 视频

该目录是仙凤API（`https://xianfengapi.xyz`）视频通道的官方声明式协议插件源码。后端从生成的 `xianfeng-video.yingce-plugin` 包加载，不依赖系统内置 `host:` 适配器。

插件只覆盖 `seedance-2.5` 与 `JD2.5满参`，两个模型共用同一个 JSON 请求体：参考图、参考视频、参考音频都发送对象数组，音视频的 `seconds` 取素材库记录的真实时长。`wan3（班车）` 的音视频必须先走 presign → PUT → commit 三步上传，声明式插件无法表达，提交前直接拒绝。

插件声明 `requiresPublicMediaUrls`：宿主先把站内素材换成签名 URL 再提交，上游必须能从公网访问这些地址。本插件只发送 JSON，不使用 multipart，也不使用上游 `/v1/files` 素材 ID。

完整接口见 [docs/interface.md](docs/interface.md)。
