# SD2.5PP 全能参考视频

该目录是独立官方协议插件源码，对接在售视频通道 `sd2.5pp`：一次请求生成一条 5–30 秒视频，参考素材统一放进 `references` 数组。后端从生成的 `sd25pp-video.yingce-plugin` 包加载，不依赖系统内置 `host:` 适配器。

插件声明 `requiresPublicMediaUrls`：宿主先把站内素材换成签名 URL 再提交，内嵌数据会被拒绝；上游必须能从公网访问这些地址。本插件不负责图床上传。

完整接口见 [docs/interface.md](docs/interface.md)。
