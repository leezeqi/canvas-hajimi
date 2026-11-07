# nskeven 视频

该目录是 nskeven NewAPI 视频通道（`https://newapi.nskeven.uk`，接入文档 `https://docs.nskeven.uk/`）的官方声明式协议插件源码。后端从生成的 `nskeven-video.yingce-plugin` 包加载，不依赖系统内置 `host:` 适配器。

上游对所有模型使用同一组路径，但请求体按模型族分三种写法：扁平字段（A/B 系列、高清视频系列、满血 Seedance 系列）、豆包 Seedance 的 `metadata` 写法、快线系列的 `metadata.videos/audios` 写法。插件按模型名自动切换，模型名原样发送。

插件声明 `requiresPublicMediaUrls`：宿主先把站内素材换成签名 URL 再提交，上游必须能从公网访问这些地址，且响应头 `Content-Type` 与实际类型一致。图片模型（`/v1/images/generations`）不在本插件范围，使用 `openai-images` 协议另建渠道，并使用图片分组的令牌。

完整接口见 [docs/interface.md](docs/interface.md)。
