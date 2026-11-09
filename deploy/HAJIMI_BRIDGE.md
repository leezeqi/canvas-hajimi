# hajimi 互通：影策侧部署

完整说明（架构、hajimi 侧配置、上线顺序、回滚）在 hajimi 仓库的 `deploy/YINGCE_BRIDGE.md`。本文只列影策这边要做的事。

## 1. 前提

- 服务器上已创建内部网络：`sudo docker network create --internal --subnet 172.31.250.0/24 yingce_bridge`
- hajimi 已按其文档上线，并把 app 以别名 `hajimi-bridge` 接入 `yingce_bridge`。
- 已拿到两份密钥：OAuth 客户端密钥（明文，只填影策后台）和桥接 HMAC 密钥（两边共用，≥ 32 位）。

## 2. `/srv/apps/open-ai-canvas/.env` 追加

```dotenv
HAJIMI_BRIDGE_ENABLED=false          # 登录验证通过后再改为 true
HAJIMI_BRIDGE_URL=http://hajimi-bridge:8080
HAJIMI_BRIDGE_KEY_ID=k1
HAJIMI_BRIDGE_SECRET=<BRIDGE_SECRET>
HAJIMI_BRIDGE_EXPECTED_IDP_HOST=hajimi.chat
HAJIMI_PUBLIC_URL=https://hajimi.chat
# HAJIMI_BRIDGE_COST_CHANNEL_HOSTS=api.hajimi.chat   # 可选，毛利报表的渠道识别；留空按 hajimi.chat 及其子域名
CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS=hajimi-bridge   # OAuth token/userinfo 走内网时需要
```

任一 `HAJIMI_BRIDGE_*` 无效时，划转功能保持关闭，日志只写出出错的变量名，不会打印密钥。

## 3. Compose 追加文件（一次性）

```bash
cd /srv/apps/open-ai-canvas
sudo cp <仓库>/deploy/docker-compose.hajimi-bridge.example.yml docker-compose.hajimi-bridge.yml
# 以 web 容器当前使用的 compose 文件列表为准，在末尾追加 bridge 文件
FILES=$(sudo docker inspect -f '{{index .Config.Labels "com.docker.compose.project.config_files"}}' open-ai-canvas-web-1),$PWD/docker-compose.hajimi-bridge.yml
ARGS=""; IFS=,; for f in $FILES; do ARGS="$ARGS -f $f"; done; unset IFS
for svc in backend web; do
  sudo docker compose --project-name open-ai-canvas --project-directory $PWD --env-file $PWD/.env $ARGS \
    up -d --no-deps --force-recreate --wait $svc </dev/null
done
```

线上 backend / web 用的是一长串 compose 文件（基础文件 + 历次源码发布的 `compose.source.json` 等覆盖），postgres / redis 只用基础文件。**不要**只带 `docker-compose.deploy.yml` 和追加文件去重建全部服务，那会丢掉这些覆盖。`deploy/remote-deploy.py` 以 web 容器的文件列表为准，并要求其他容器的文件列表是它的子集，所以 backend 和 web 要用同一组文件重建；数据库、Redis 和 yingce-agent 不用动。之后的部署会自动沿用这组文件。

脚本通过 stdin 传给远端执行时，`docker compose up/exec` 要加 `</dev/null`，否则会把剩余脚本当作输入读走。

## 4. 数据库

新表由 fork 自有的迁移步骤创建（记录在 `fork_schema_migrations`，不改上游的 schema 版本号），随正常部署里的 `migrate-schema up` 一起执行：`hajimi_bridge_transfers`、`hajimi_bridge_user_settings`、`hajimi_bridge_user_state`、`hajimi_bridge_state`。

## 5. 后台配置

管理后台 → 系统设置 → 登录与注册 → "OAuth 登录（当前对接 hajimi）"，字段见 hajimi 仓库 `deploy/YINGCE_BRIDGE.md` 第 5 节。影策原有的账号密码登录和注册不受影响，两种账号并存。

## 6. 运维入口

管理后台 → "hajimi 互通"：连通状态、最近一次对账、待人工处理的划转（可重试）。两边记录矛盾时不会自动动钱。

## 7. 毛利报表

管理后台 → "hajimi 互通" → "毛利报表"：按天对比影策已结算订单的实收积分（1 积分 = 1 美元）和 hajimi 对运营 key 的实际扣费，可选近 7 天 / 近 30 天 / 本月 / 上月或自定义（最多 92 天），按北京时间或 UTC 切日，可导出 CSV。

- **哪些渠道算"走 hajimi"**：系统渠道里 Base URL 主机名是 `HAJIMI_BRIDGE_EXPECTED_IDP_HOST` 或它的子域名（如 `api.hajimi.chat`）。渠道改走内网地址（如 `hajimi-bridge`）时，在 `HAJIMI_BRIDGE_COST_CHANNEL_HOSTS` 里写明。报表"渠道识别"页签会列出每个渠道的归类，上线后先核对一遍。归类按渠道当前地址判断，改过渠道地址后历史订单也会跟着重新归类；想保持历史口径，换上游时新建渠道而不是改旧渠道的地址。
- **hajimi 成本从哪来**：hajimi 的 `YINGCE_COST_USER_IDS` / `YINGCE_COST_API_KEY_IDS`（见 hajimi 仓库 `deploy/YINGCE_BRIDGE.md` 第 9 节）。没配置时报表只显示影策收入。
- 其他上游渠道只能用影策后台给渠道配置的成本价估算；没配置成本价的订单会单独计数。
- 报表只读，不改任何数据；hajimi 连不上时影策收入照常显示。
