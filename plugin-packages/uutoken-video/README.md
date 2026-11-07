# UUToken 视频

该目录是 UUToken（`https://uutoken.sbs`）视频通道的官方声明式协议插件源码。后端从生成的 `uutoken-video.yingce-plugin` 包加载，不依赖系统内置 `host:` 适配器。

UUToken 的 `/v1/videos` 只统一了路径和任务生命周期，各模型的请求字段不同。插件按模型名切换请求体：Seedance、Wan、seadanco、dreamina、hss 等模型发送通用格式（整数 `seconds`、`aspect_ratio`、`resolution`、`images`/`videos`/`audios` URL 字符串数组）；`sd2.5-gf`、`kling`、`sora-v3`、`gemini-omni-flash`、`Seedance-2.0-full` 发送各自的专属字段。模型名原样发送，以 UUToken 模型广场为准。

插件声明 `requiresPublicMediaUrls`：宿主先把站内素材换成签名 URL 再提交，内嵌数据会被拒绝；上游必须能从公网访问这些地址。本插件只发送 JSON，不使用 multipart 文件。

插件仅管理员可见：普通用户看不到它，也不能在自定义渠道里选它；管理员配置成系统渠道模型后，普通用户可以正常使用。

完整接口见 [docs/interface.md](docs/interface.md)。
