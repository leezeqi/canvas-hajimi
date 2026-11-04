# 傲天 Seedance 视频

该目录是独立官方协议插件源码。后端从生成的 `aotian-video.yingce-plugin` 包加载，不依赖系统内置 `host:` 适配器。

傲天 AoTian 网关（以及按原样转发其 JSON 的 NewAPI 中转）要求参考素材以公网 HTTPS URL 提交。部分渠道（例如特价 Seedance 线路）拒绝 multipart 文件上传，因此本插件只发送 JSON，参考图放入 `images` 数组，不再使用 `newapi`（OpenAI Videos）协议的 `input_reference` 文件。

完整接口见 [docs/interface.md](docs/interface.md)。
