# 9Y API 视频

该目录是 9Y API（`https://9yapi.shop`）视频通道的官方声明式协议插件源码。后端从生成的 `nineyapi-video.yingce-plugin` 包加载，不依赖系统内置 `host:` 适配器。

覆盖使用同一请求体形状（整数 `seconds`、小写 `resolution`、`aspect_ratio`、`reference_images: [{url, role}]`）的模型：SD-2.0 / SD-2.5 系列（满血、官转、DB、mini）、`【官转】Minimax-H3`、wan3.0 特惠版（480p / 720p / 1080p）以及 `sd2.0-fast` / `【官转】sd2.0-fast`。模型名原样发送，在渠道模型列表里逐个配置。

kling-omni、JM 系列（参考素材为纯 URL 字符串数组）、`官转wan3.0-高速版`（`size` + `parameters`）和 `奥特曼h3-768p`（像素 `size`）的请求体不同，不在本插件范围内。

插件声明 `requiresPublicMediaUrls`：宿主先把站内素材换成签名 URL 再提交，内嵌数据会被拒绝；上游必须能从公网访问这些地址。本插件不调用 9Y 的 `/v1/reference-assets` 图床。

完整接口见 [docs/interface.md](docs/interface.md)。
