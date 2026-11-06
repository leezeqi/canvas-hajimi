# Navivo NewAPI 视频

该目录是 Navivo NewAPI（`https://db.navivo.cc`，`seedance.navivo.cc` 为同一服务）视频通道的官方声明式协议插件源码。后端从生成的 `navivo-video.yingce-plugin` 包加载，不依赖系统内置 `host:` 适配器。

同一插件覆盖两种请求体形状，按模型名区分：模型名去掉空格并转小写后包含 `sd933` 时按 SD 933 混合参考格式发送，其余模型（`dola-seedance-20-fast-text-video`、`dola-seedance-25-text-video`）按 OpenAI 兼容视频格式发送。模型名原样发送，以 NewAPI `/v1/models` 返回的名称为准。

插件声明 `requiresPublicMediaUrls`：宿主先把站内素材换成签名 URL 再提交，内嵌数据会被拒绝；上游必须能从公网访问这些地址。本插件只发送 JSON，不使用 multipart `input_reference` 文件。

完整接口见 [docs/interface.md](docs/interface.md)。
