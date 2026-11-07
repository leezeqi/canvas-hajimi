# canvas-hajimi 同步上游方法

这份文档说明 `canvas-hajimi`（影策二改仓库）如何持续吸收上游 `open-ai-canvas` 的更新：二改功能要保留，上游修复要按时吸收，每次同步都要能核对、能回滚。

本文件只属于本仓库，所以放在 `docs/` 根目录，没有放进 `docs/content/docs/`，也没有登记到上游维护的 `docs/index.md`，免得每次同步都在这两处产生冲突。上游的 `.gitignore` 忽略了 `docs/*`（只放行部分子目录），所以本文件是用 `git add -f` 加入版本库的。以后新增本仓库专属的文档也这样处理，不要为此修改 `.gitignore`。

## 一、仓库关系

| 角色 | 本地目录 | 远程 | 说明 |
| --- | --- | --- | --- |
| 本仓库 | `D:\canvas-hajimi` | `origin` = `https://github.com/leezeqi/canvas-hajimi.git` | 独立公开仓库，不是 GitHub fork，没有 “Sync fork” 按钮 |
| 上游 | `D:\open-ai-canvas` | 该目录的 `origin` = `https://github.com/ddcat-ai/open-ai-canvas.git` | 只用来拉取，不在这里开发二改 |

两边共享真实的提交历史：本仓库的 `main` 就是上游 `main` 加上二改提交。因此日常同步用普通 `git merge` 即可，**不需要** hajimi（sub2api 二改）那种从上游基线重建桥接历史的流程。

每次同步前先确认共同祖先还在：

```powershell
git merge-base main upstream/main
```

如果没有输出或者报错，说明历史已经断开（例如有人压扁导入过上游代码）。这时停下来，不要继续执行本文流程，先评估是否要按 hajimi 文档重建桥接历史。

## 二、完成标准

同时满足以下全部条件，一次同步才算完成：

1. 本地 `main` 包含选定的上游基线（`git merge-base --is-ancestor <上游基线> main` 退出码为 0），第四节二改清单中的功能逐项保留。
2. 派生文件都由生成器产生且与源码一致，包括画布生成契约 `web/src/lib/canvas/generation-contract.generated.ts`，以及受影响插件的 `manifest.json`、`docs/interface.md` 和 `.yingce-plugin` 包。
3. `bun scripts/verify-before-push.mjs` 按默认范围运行通过，交付记录里写下脚本的总结行和“本地未覆盖”项。本机缺少 Bun、cgo 或 LF 检出时，按第三节的容器方式做等价检查，并在同步记录里逐项写明。
4. `git ls-remote origin refs/heads/main` 返回本地目标提交的哈希。
5. 推送触发的 GitHub Actions「Publish container images」运行通过（它内部会调用「Quality checks」）。
6. 工作区干净，同步分支已删除或说明了保留原因，同步记录已追加到第十节。

`git push` 输出 `Everything up-to-date` 并不能证明推送成功。只要出现过 `RPC failed`、`curl 55` 或 `unexpected disconnect`，都必须用远程哈希核对。

## 三、一次性配置

同步需要以下工具。缺少 Go 或 Bun 时，派生文件无法重新生成，`verify-before-push.mjs` 也跑不完，这种情况下不能推送。

| 工具 | 版本依据 | 用途 |
| --- | --- | --- |
| Git | 2.38 或更高 | `merge-tree --write-tree` 试合并 |
| Go | `backend/go.mod` 的 `go` 行（当前为 1.25.0） | `go generate`、后端测试 |
| Bun | `web/package.json` 的 `packageManager`（当前为 `bun@1.3.9`） | Web 依赖、测试、验证脚本 |
| Node.js、npm | 无固定版本 | 插件生成脚本、Agent runtime 依赖 |

```powershell
git remote add upstream D:/open-ai-canvas
git remote set-url --push upstream DISABLED
git config remote.upstream.tagOpt --no-tags
git fetch upstream --prune
git remote -v
```

- `--push DISABLED`：防止误推到上游目录。
- `--no-tags`：上游的发布标签格式是 `v*`。本仓库的 `.github/workflows/publish-images.yml` 收到推送的 `v*` 标签后会正式发版：校验 `VERSION` 和 `CHANGELOG.md`，推广镜像标签，创建 GitHub Release。上游标签一旦进入本仓库，又被 `git push --tags` 或 `--follow-tags` 带出去，就会在本仓库误发版。需要查上游版本时，一律在上游目录执行 `git -C D:/open-ai-canvas describe --tags`。
- `upstream` 指向本地上游目录时，`upstream/main` 等于该目录的本地 `main`，所以每次同步前要先在上游目录执行 `pull --ff-only`。也可以把 `upstream` 改成 `https://github.com/ddcat-ai/open-ai-canvas.git`，省掉这一步，其余流程不变。

### 本机缺少工具时的容器验证

本机（Windows）有三个限制：PATH 里没有 Go 和 Bun；没有 C 编译器，`go-sqlite3` 报 `requires cgo`，依赖 SQLite 的用例都跑不起来；检出用的是 `core.autocrlf=true`，工作区全是 CRLF，`gofmt -l .` 会把几乎所有 Go 文件都列出来。这时 `verify-before-push.mjs` 跑不完，改用下面的等价方式，并在同步记录里写明：

1. `go generate` 这类不需要 cgo 的命令，可以直接用模块缓存里的工具链：`$env:Path = "$env:USERPROFILE\go\pkg\mod\golang.org\toolchain@v0.0.1-go<版本>.windows-amd64\bin;$env:Path"`，再设 `$env:GOTOOLCHAIN = "local"`。
2. 先在同步分支提交合并，再导出成 LF 换行的目录：`git -c core.autocrlf=false archive <提交> | tar -x -C <临时目录>`。容器挂载这个目录，不挂载工作区。
3. 后端：用 `local/go-cgo:1.25` 镜像（Alpine，带 gcc，没有 git），挂载 `hajimi-gomod` 和 `hajimi-gocache` 卷，在 `backend/` 下依次运行 `gofmt -l .`、非 `internal/app` 包的 `go test`，以及 `internal/app` 按 `go test -list .` 的结果分 4 片执行，和 `verify-before-push.mjs` 的口径一致。
4. Web：用 `oven/bun:1.3.9` 镜像，把 `hajimi-web-node-modules` 卷挂到 `web/node_modules`，把 `hajimi-bun-cache` 卷挂到 `/root/.bun/install/cache`。依次运行 `bun install --frozen-lockfile`、`bun run typecheck`、`bun run lint`、`bun run test`。容器里没有 git，格式检查改为：在宿主机算出 `git diff --relative=web --name-only --diff-filter=ACMR origin/main <提交> -- web` 的文件列表，并导出 `origin/main` 的 `web/` 目录，在容器里用 `web/node_modules/prettier` 逐个检查。基线版本本来就没有格式化的文件跳过，规则与 `web/scripts/check-changed-formatting.mjs` 相同。
5. 这种方式覆盖不到 `golangci-lint`、预演台 E2E（只有 `previs` 相关改动时才需要）以及 PostgreSQL / Redis 用例，这些由推送后的 CI 兜底。

## 四、二改边界与清单

### 1. 当前二改清单

截至 2026-11-07（上游 `v1.8.2` 同步之后），与上游的共同祖先是 `cd2512eb`。二改按功能分组如下，提交按时间先后排列：

| 功能 | 提交 | 内容 | 改动过的上游文件（冲突热点） |
| --- | --- | --- | --- |
| 聚创视频、失败原因、Responses、渠道模型删除 | `4f6eb045`、`088c95c7`、`0871bc6b` | 新增聚创引擎视频插件 `juchuang-video`；任务列表返回真实失败原因，过滤后的上游原因接在分类后面展示，工作台失败卡片可以展开；Responses 协议包升级到 2.0.2，新增 `$join` 表达式；删除渠道模型时，引用检查排除已归档的前台模型 | `backend/internal/app/` 下的 `cloud_agent_user_errors.go`、`provider_errors.go`、`provider_protocol_async.go`、`provider_protocol_result.go`、`task_output.go`；`backend/internal/protocol/expression.go`、`manifest_adapter.go`；`backend/internal/repository/finance_channel_models.go`、`repository_tasks.go`；`plugin-packages/generate-catalog.mjs`；`plugin-packages/openai-responses/*`（含 `.yingce-plugin`）；`web/src/lib/generation-error.ts`；`web/src/services/api/task-center.ts`；`web/src/pages/workbench/workbench-gallery.tsx`、`workbench.css` |
| 第三方视频声明式插件 | `4c647a9a`、`ea88698d`、`9ea0ce2e`、`350d4980`、`24ac73a1`、`1d11ab7d`、`e445a9bb`，以及新增 `xianfeng-video` 的提交 | `aotian-video`、`sd25pp-video`（含比例类失败原因提示）、`nineyapi-video`、`navivo-video`、`nskeven-video`、`uutoken-video`、`xianfeng-video`，都是新增的手写插件目录 | 基本是新增文件；`backend/internal/protocol/official_catalog_test.go` |
| 插件对用户开放开关 | `111cdc15`、`a41a4bf3`、`8867bb58`、`68efbefe`、`69b04c69`、`880ffc0d`，以及改成后台开关的提交 | 管理员在 `/admin/plugins` 把协议插件设为仅管理员可见：普通用户的插件中心、插件状态、协议目录、插件包下载和自定义渠道都看不到它，管理员配置的系统渠道照常可用。状态存 `plugin_user_visibilities`（`fork_schema.go` 建表，旧库建表时把原先写死的 6 个视频插件写成隐藏）；生成任务按协议反查所属插件。二改逻辑集中在新文件 `backend/internal/app/plugin_user_visibility.go`、`plugin_user_visibility_test.go`、`backend/internal/repository/plugin_visibility.go`、`backend/internal/model/models_plugin_visibility.go` | `backend/internal/app/plugin_management.go`、`protocol_registry.go`、`provider.go`、`feature_availability_test.go`、`plugin_management_test.go`（测试建表各加一个模型）；`backend/internal/handler/plugin.go`；`backend/internal/database/schema.go`、`fork_schema.go`；`backend/cmd/migrate-sqlite-postgres/main.go`；`web/src/services/api/plugins.ts`；`web/src/pages/admin/plugins/plugins-page.tsx`、`plugin-detail-modal.tsx` |
| 作品删除 | `f8789d3c`、`83a10123` | 创作页可以删除单条图像 / 视频生成记录；生图与视频工作台可以删除作品记录（`DELETE /api/tasks/:id`，写 `tasks.user_deleted_at`） | `web/src/pages/create/index.tsx`、`creation-workspace-messages.tsx`；`web/src/pages/workbench/image-workbench-page.tsx`；`web/src/styles/globals.css`；`backend/internal/app/task_lifecycle.go`；`backend/internal/handler/routes.go`；`backend/internal/model/models_task.go`；`backend/internal/database/migrations.go`（只在 `MigrateSchema` 里加了两行调用，见第四节第 2 小节） |
| 作品保存多候选地址 | `51432ae1` | 结果地址按换行拆成多个候选，依次下载；地址解析失败不再当成网络错误空等重试 | `backend/internal/app/provider_protocol_result.go`、`task_media_recovery.go`、`task_media_storage.go`；`backend/internal/storage/errors.go` |
| Windows 一键源码部署 | `ac0db8ac`、`7ef7441d`、`2dccf1e7`、`3641fef5`、`03de3aa1`、`b796cecb` | Windows 入口脚本、远端只读检查、源码构建、备份与回退 | `.gitattributes`、`.gitignore`、`Dockerfile` |

`docs/content/docs/progress/pending-test.mdx`、`overview/features.mdx`、`backend/http-api.mdx`、`backend/backend-database.mdx` 和 `docs/index.md` 几乎每个二改都会碰，按第六节的规则处理。

同步后用下面的命令回归二改功能：

```powershell
Push-Location backend
go test ./internal/protocol -run 'Juchuang|Aotian|SD25PP|NineYAPI|Navivo|Nskeven|UUToken|Xianfeng|OfficialResponsesNestedOutputText|ManifestJoin'
go test ./internal/app -run 'Juchuang|Aotian|PluginUserVisibility|ProviderProtocolPlugin|HiddenPlugins|TaskSummaryReturnsSafeFailureReason|TextResponseErrorsAreNotNetworkErrors|ResponsesSynchronousGenerationReadsNestedOutput|DeleteTask|MediaURLCandidates|DownloadTaskMedia'
go test ./internal/repository -run TestDeleteChannelModelsArchivedReferences
go test ./internal/storage -run TestClassifyErrorURLParseIsNotTransient
go test ./internal/database -run 'ForkTaskDeletion|ForkV79|ReleaseForkMigration|LegacyAdminOnlyPlugins'
Pop-Location
Push-Location web
bun test test/workbench-failure-reason.test.tsx test/generation-error-details.test.ts
Pop-Location
```

这些用例大多依赖 SQLite（cgo）。本机没有 C 编译器时按第三节的容器方式运行。

清单要随实际情况维护：新的二改落地后补一行；二改被上游吸收后删掉对应行。本仓库独有的提交可以这样列出来：

```powershell
git log --oneline --no-merges upstream/main..main
```

如果某个二改以完全相同的补丁回馈到了上游，可以改用 `git log --oneline --no-merges --cherry-pick --right-only upstream/main...main`，把等价补丁过滤掉。上游用 squash 合并时补丁通常不相同，这种情况需要人工对照。

### 2. 二改约定（让后续同步保持简单）

- **优先新增，少改上游文件。** 新能力尽量做成独立插件目录（`plugin-packages/<id>/`）、新文件和新测试；必须改上游文件时，改动要小而集中。
- **不新增数据库迁移版本。** 迁移在 `backend/internal/database/migrations.go` 里按整数版本线性登记，版本号只跟随上游（2026-11-07 同步后为 91）。程序启动时会逐个版本核对名称和校验和，二改占用的版本号迟早会和上游撞号，部署时报“数据库迁移 N 名称不一致/校验和不一致”，拒绝启动。二改需要加列或建表时，写进 `backend/internal/database/fork_schema.go` 的 `applyForkSchema`：用 `HasTable` / `HasColumn` 判断后幂等补齐，不登记到 `schemaMigrations`，也不改 `CurrentSchemaVersion`。它在上游迁移全部执行完之后运行，所以只能做“补列、建新表”这类不依赖执行顺序的改动；需要改写数据、改列类型或删列时，先和上游商量。
  - 这条规则是吃过亏才定下的：`83a10123` 曾把 `tasks.user_deleted_at` 登记为迁移 79 `task_user_deletion`，结果和上游的 79 `ecommerce_agent_and_knowledge` 撞号。2026-11-07 同步时把它迁到了 `fork_schema.go`，同时用 `releaseForkMigrationVersions` 释放旧账本记录：账本里的 79 是这条二改记录（名称和校验和都匹配）、而且之后没有其他记录时，先删掉这条记录（列保留），再执行上游的 79–91。`migrations.go` 里只为此在 `MigrateSchema` 中加了两行调用，以后同步遇到冲突时保留这两行即可。
- **不改 `VERSION` 和 `CHANGELOG.md`，不打 `v*` 标签。** 版本号跟随上游；在本仓库打 `v*` 标签会触发正式发版。
- **目录插件只通过 `generate-catalog.mjs` 修改。** 由 `plugin-packages/generate-catalog.mjs` 生成的插件（例如 `openai-responses`），要改就改这个脚本本身，不要只手改 `manifest.json`，否则下一次重新生成会把手改的内容抹掉。手写插件（例如 `aotian-video`、`juchuang-video`）直接维护各自的 `manifest.json`。
- **依赖版本跟随上游。** `.github/dependabot.yml` 来自上游，本仓库也会收到 Dependabot PR。这些 PR 不单独合并，免得和上游的同类升级冲突；安全修复单独评估。
- **文档按 AGENTS.md 同步。** 二改仍要更新 `pending-test.mdx`、`http-api.mdx` 等专题文档。这些文件是可以预见的冲突点，按第六节的规则处理。

## 五、日常同步流程

下面的命令都在 PowerShell 中执行，工作目录为 `D:\canvas-hajimi`。

### 1. 更新上游目录，确定基线

```powershell
git -C D:/open-ai-canvas switch main
git -C D:/open-ai-canvas status --short --branch
git -C D:/open-ai-canvas pull --ff-only
git -C D:/open-ai-canvas log --oneline --decorate -n 5
git -C D:/open-ai-canvas describe --tags
```

如果上游目录有未提交的改动，不要动它，改用 GitHub URL 作为 `upstream`。

默认以上游 `main` 的最新提交为基线，`describe` 的输出（例如 `v1.7.3.5-12-gcc229fef`）记进同步记录。只想同步到某个正式发布时，用 `git -C D:/open-ai-canvas rev-parse "v1.7.3.5^{commit}"` 得到发布提交，下文的 `$base` 换成这个哈希即可。

### 2. 冻结现状并备份

```powershell
git switch main
git status --short --branch
git fetch origin --prune
git merge --ff-only origin/main
$date = Get-Date -Format yyyyMMdd
git tag "backup/canvas-hajimi-before-upstream-$date"
```

工作区不干净时，先单独处理未提交的内容。本地 `main` 如果有还没推送的二改提交，先推送，或者在同步记录里写明它们也会随这次同步一起推送。备份标签在远程核对和部署验证完成之前不要删除。

### 3. 获取上游并预检

```powershell
git fetch upstream --prune
$base = git rev-parse upstream/main
git merge-base main $base
git rev-list --count "main..$base"
git log --oneline --no-merges "$base..main"
git merge-tree --write-tree --name-only main $base
```

- `merge-base` 必须有输出（见第一节）。
- `rev-list --count` 得到这次要吸收的上游提交数。
- `merge-tree` 只在内存里试合并，不改动工作区。它输出的第一行是树对象，之后列出冲突文件；退出码 1 表示有冲突。这个命令需要 Git 2.38 或更高版本。

再看看高风险区域有没有变化：

```powershell
git show "main:backend/internal/database/migrations.go" | Select-String "CurrentSchemaVersion int64"
git show "${base}:backend/internal/database/migrations.go" | Select-String "CurrentSchemaVersion int64"
git diff --stat "main...$base" -- plugin-packages backend/internal/canvas/contract web/package.json web/bun.lock backend/go.mod
```

只要 `CurrentSchemaVersion` 有变化，这次同步就带有数据库迁移。部署前必须先备份数据库，见第九节。

### 4. 在同步分支上合并

```powershell
git switch -c "sync/upstream-$date"
git merge --no-ff --no-commit $base
git status --short
```

用 `--no-commit` 的原因是：即使合并没有冲突，也要先在合并状态下重新生成派生文件、跑二改回归，再把它们一起放进同一个合并提交。冲突按第六节的规则处理。处理时 `--ours` 指本仓库，`--theirs` 指上游。

### 5. 重新生成派生文件

画布生成契约（Go 源码会生成 TypeScript 声明）：

```powershell
Push-Location backend
go generate ./internal/canvas/contract
Pop-Location
git diff --stat -- web/src/lib/canvas/generation-contract.generated.ts
```

插件包：找出合并结果与两侧都不相同的插件文件。这些插件两边都改过，任何一侧原来的 `.yingce-plugin` 都不再对应合并后的源码：

```powershell
$fromFork = git diff --name-only HEAD -- plugin-packages
$fromUpstream = git diff --name-only MERGE_HEAD -- plugin-packages
$fromFork | Where-Object { $fromUpstream -contains $_ }
git diff --name-only --diff-filter=U -- plugin-packages
```

最后一条命令列出仍处于冲突状态的插件文件，`.yingce-plugin` 二进制冲突也会出现在这里。在重新生成之前运行上面这组命令，否则结果会把刚生成的文件也算进去。

对列出的每个插件 `<id>`（官方支付插件 `official-payment-*` 除外，见第六节）：

```powershell
node plugin-packages/generate-catalog.mjs <id>
```

如果脚本报 `unknown protocol package id(s)`，说明这是手写插件，改用下面的命令重新嵌入文档：

```powershell
node plugin-packages/embed-documentation.mjs <id>
```

然后重新打包，并确认二改内容还在：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File plugin-packages/package-selected.ps1 <id>
git diff HEAD -- plugin-packages/<id>/manifest.json
```

只重新打包上面列出的插件，不要整体执行 `build-packages.sh`。整体打包会让上百个二进制包产生无意义的字节差异，支付插件还需要交叉编译。

同样不要不带参数运行 `generate-catalog.mjs`。上游会直接手改部分目录插件的 `manifest.json`（例如 `dashscope-wan3-video` 的分辨率转大写、`anthropic-messages` 的输出 Token 键），已提交的文件和生成器输出本来就对不上，整体重新生成会把这些修复冲掉。如果列表里只有 `plugin-packages/generate-catalog.mjs` 本身，说明两侧改的是生成器里不同插件的定义，各插件目录和包只有一侧改过，直接沿用合并结果即可，不需要重新生成。重新生成某个插件后，要用 `git diff` 确认差异只包含预期的变化。

### 6. 二改回归并提交合并

先安装依赖（锁文件可能被上游更新过），再跑第四节的二改回归命令：

```powershell
Push-Location web
bun install --frozen-lockfile
Pop-Location
Push-Location backend/agent-runtime/pi
npm ci --omit=dev --ignore-scripts
Pop-Location
```

确认后提交。提交说明遵循 AGENTS.md 的格式 `<type>(<scope>): <业务模块> - <变更摘要>`，正文写清楚上游基线、吸收的提交数、冲突文件和保留的二改范围：

```powershell
git add -A
git status --short
git commit -m "chore(upstream): 上游同步 - 合并 open-ai-canvas 至 <短哈希>（<describe 输出>）"
```

`git add -A` 之前先确认 `git status` 里没有不该提交的文件，例如 `.local/`、数据库文件、`web/package-lock.json`。

### 7. 推送前验证

```powershell
bun scripts/verify-before-push.mjs
```

脚本以 `origin/main` 为基准挑选检查，所以这次合并带进来的全部上游改动都会计入，Web 和后端检查通常都会被选中。最后一次验证要用默认范围，不加 `--only`。各项输出在 `.local/cache/verify/`。

失败时在同步分支上追加修复提交，然后重跑脚本。

**不要 rebase 同步分支。** AGENTS.md 通用流程要求推送前 rebase 到最新远端，但这一条不适用于同步合并：rebase 会丢掉合并提交，把几十个上游提交改写后重放到二改之上。如果同步期间 `origin/main` 有了新提交，执行 `git fetch origin` 后用 `git merge origin/main` 合进同步分支，再重新验证。

### 8. 合入 main 并推送

```powershell
git switch main
git merge --ff-only "sync/upstream-$date"
$target = git rev-parse HEAD
git ls-remote origin refs/heads/main
git push origin main
git ls-remote origin refs/heads/main
```

第一次 `ls-remote` 的结果应该等于同步前的 `origin/main`，第二次必须等于 `$target`。这是普通的快进推送，不要使用 `--force`，也不要加 `--tags` 或 `--follow-tags`。

推送之后：

- 到 `https://github.com/leezeqi/canvas-hajimi/actions` 确认「Publish container images」通过。这个工作流会运行质量检查，并构建 `ghcr.io/leezeqi/open-ai-canvas-web`、`-backend`、`-yingce-agent` 三个镜像。
- 清理同步分支：

```powershell
git branch -d "sync/upstream-$date"
git status --short --branch
```

部署不属于同步流程。如果这次同步带有数据库迁移，部署前按第九节先备份数据库。

## 六、冲突处理规则

| 文件或类型 | 处理方式 |
| --- | --- |
| `VERSION`、`CHANGELOG.md` | 取上游：`git restore --theirs -- VERSION CHANGELOG.md`，然后 `git add`。本仓库不自定版本号 |
| `docs/content/docs/progress/pending-test.mdx`、`overview/features.mdx`、`backend/http-api.mdx` | 保留双方条目。不删上游条目，二改条目保持在原来的分节里 |
| `backend/internal/database/migrations.go` 和迁移函数 | 取上游（`git checkout --theirs`），然后补回 `MigrateSchema` 里的 `releaseForkMigrationVersions(tx)` 和 `applyForkSchema(tx)` 两行调用，并跑第四节的 `internal/database` 回归。二改的表结构变化只放在 `fork_schema.go`；如果发现二改又登记了迁移版本，先按第四节第 2 小节迁出，再继续同步 |
| `backend/internal/canvas/contract/*.go` | 正常合并代码，然后执行第五节第 5 步，重新生成 `generation-contract.generated.ts`；生成文件本身不要手改 |
| `plugin-packages/generate-catalog.mjs` | 文本合并两侧的插件定义，然后按第五节第 5 步重新生成 |
| `plugin-packages/<id>/manifest.json`、`docs/interface.md`、`README.md` | 目录插件：以合并后的 `generate-catalog.mjs` 重新生成，不手改。手写插件：合并 JSON，再执行 `embed-documentation.mjs <id>` |
| `plugin-packages/*.yingce-plugin` | 二进制文件，不要直接选用任何一侧。先解决源目录的冲突，再重新打包 |
| `plugin-packages/official-payment-*` | 一律取上游，二改不改支付插件。CI 的「Payment plugin artifacts」会校验它们 |
| `web/package.json`、`web/bun.lock` | 依赖版本跟随上游。二改新增了依赖时，在上游版本的基础上执行 `bun install`，重新生成 `bun.lock`。`web/` 只用 bun，不要留下 `package-lock.json`、`pnpm-lock.yaml` 或 `yarn.lock`（CI 会拒绝） |
| `backend/go.mod`、`backend/go.sum` | 取上游；二改有额外依赖时，再执行 `go mod tidy` |
| 业务代码 | 上游的通用修复优先。二改按第四节清单逐项确认保留。已经被上游吸收的二改改用上游实现，并从清单中删除 |

## 七、大批量推送的兜底

本仓库不是 GitHub fork，上游的对象不会自动出现在 GitHub 上的本仓库里，所以积压的上游提交越多，单次推送的数据包越大。普通推送失败时，先把上游提交单独推到临时分支，再推 `main`。临时分支不会触发任何工作流：发布工作流只响应 `main` 和 `v*` 标签，质量检查只在 PR 上运行。

```powershell
$ssl = @('-c', 'http.sslBackend=openssl', '-c', 'http.version=HTTP/1.1')
$staging = "sync-staging-$date"
& git @ssl push origin "${base}:refs/heads/$staging"
& git @ssl push origin main
& git @ssl ls-remote origin refs/heads/main
& git @ssl push origin --delete $staging
```

如果临时分支本身也推不上去，就按上游主线分批推进，每批 50 个提交：

```powershell
$from = git merge-base origin/main $base
$commits = @(git rev-list --first-parent --reverse "$from..$base")
for ($i = 49; $i -lt $commits.Count; $i += 50) {
    & git @ssl -c http.lowSpeedLimit=1 -c http.lowSpeedTime=600 push origin "$($commits[$i]):refs/heads/$staging"
    if ($LASTEXITCODE -ne 0) { throw "临时分支推送失败: $($commits[$i])" }
}
& git @ssl push origin "${base}:refs/heads/$staging"
```

已经推成功的批次会被 GitHub 保留，重试时只发送缺少的对象。远程核对完成之前，不要删除备份标签或临时分支。

## 八、故障速查

| 现象 | 判断 | 处理 |
| --- | --- | --- |
| `git merge-base main upstream/main` 没有输出 | 与上游的历史已经断开 | 停止同步，评估桥接历史方案 |
| `merge-tree` 列出 `.yingce-plugin` 冲突 | 两侧都改了同一个插件 | 先解决源目录冲突，再重新打包 |
| 启动报“数据库迁移 N 名称不一致”或“校验和不一致” | 二改迁移和上游迁移撞了版本号 | 停止部署，恢复备份，按第四节第 2 小节把二改迁移移到 `fork_schema.go` |
| `merge-tree` 预检列出 `migrations.go` 冲突，二改一侧有新的 `{version: N, ...}` | 有二改登记了迁移版本 | 合并前先把它迁出到 `fork_schema.go`，并在 `releaseForkMigrationVersions` 里释放旧记录 |
| `go test` 报 `go-sqlite3 requires cgo` | 本机没有 C 编译器 | 按第三节在 `local/go-cgo:1.25` 容器里运行 |
| `gofmt -l .` 列出几乎所有 Go 文件 | `core.autocrlf=true` 检出的 CRLF 换行 | 在 `git archive` 导出的 LF 目录里检查 |
| 启动报“数据库结构版本 X 高于程序支持的 Y” | 新迁移已经执行，程序却回滚到了旧版本 | 回滚程序时必须同时恢复同步前的数据库备份 |
| 启动报“数据库结构版本过旧……请先执行 migrate-schema up” | 新程序还没跑迁移 | 按部署文档运行迁移服务 |
| verify 的格式检查只在上游文件上失败 | 上游直接推送了未格式化的文件 | 只对失败的文件执行 prettier，作为单独的 `style` 提交，并在同步记录里注明 |
| CI 报 `forbidden lockfile` | `web/` 下残留了 npm、pnpm 或 yarn 的锁文件 | 删除该文件，用 `bun install` |
| `git push` 因 non-fast-forward 被拒 | 远程 `main` 有了新提交 | 不要强推。fetch 后把 `origin/main` 合进同步分支，重新验证 |
| `RPC failed` 或 `curl 55` 之后显示 `Everything up-to-date` | 结果未知，通常没有更新 | 立即用 `git ls-remote` 核对 |
| TLS 或 Schannel 握手失败 | Windows TLS 问题或网络瞬断 | 当前命令加 `-c http.sslBackend=openssl -c http.version=HTTP/1.1` |
| 误推了 `v*` 标签 | 已经触发发版工作流 | 立即在 Actions 中取消运行，然后核对并清理远程标签、Release 和 GHCR 镜像标签 |

## 九、备份与回滚

每次同步至少保留：同步前的 `backup/canvas-hajimi-before-upstream-<日期>` 标签、上游基线哈希、最终提交哈希。备份标签可以推到远程保存（`git push origin "refs/tags/backup/canvas-hajimi-before-upstream-$date"`），`backup/` 前缀不会触发发版。

**代码回滚：**

- 还没推送：把本地 `main` 指回备份提交，例如 `git switch --detach`、`git branch -f main "backup/canvas-hajimi-before-upstream-$date"`、`git switch main`。
- 已经推送：优先用 `git revert -m 1 <同步合并提交>` 生成回滚提交，这样不改写远程历史。注意，下次重新同步之前要先 revert 这个回滚提交，否则这批上游改动不会再次进入。
- 强制把远程 `main` 移回备份提交，只能在明确确认后执行，并且要带上 `--force-with-lease=refs/heads/main:<当前远程哈希>`，不能覆盖未知的新提交。

**数据库回滚：** 代码回滚不会回滚数据库。旧程序连接到已经执行过新迁移的数据库时，会报“数据库结构版本高于程序支持的版本”并拒绝启动。所以只要同步带有迁移（`CurrentSchemaVersion` 有变化），部署前就必须备份数据库，流程见 [在线更新与安全回退](content/docs/backend/system-update.mdx)。回滚时代码和数据库要一起恢复。

本地开发数据库 `.local/project-workbench-debug` 也是一样：同步带有迁移时，先复制一份，再用新代码启动后端。

## 十、同步记录

### 2026-11-07：v1.7.3.4 之后（`4d25e4a5`）→ v1.8.2

- 上游基线：`cd2512eb9ba0aa7fa97ac7a72cb0d97c44c36828`（`v1.8.2`），吸收 159 个上游提交。首次配置了 `upstream` 远程（指向 `D:/open-ai-canvas`，禁止推送，不拉标签）。
- 同步前 main：`880ffc0d`，包含 26 个二改提交；回滚标签：`backup/canvas-hajimi-before-upstream-20261107`（只在本地）。合并提交：`1914c1e1`（同步分支 `sync/upstream-20261107`）。
- 冲突文件及处理方式：`.gitignore` 两侧的放行规则都保留；`web/src/pages/create/index.tsx` 采用上游的格式化与多语言版本，补回 `onDeleteMessage`；`backend/internal/database/migrations.go` 取上游，二改迁移 79 迁到 `fork_schema.go`（见第四节第 2 小节）；`backend-database.mdx`、`pending-test.mdx` 保留双方条目，二改条目改写为不占用迁移版本的说明。
- 数据库结构版本：本仓库 79（含二改的 `task_user_deletion`）→ 91。新增的上游迁移是 79–91（电商 Agent、支付方式、方舟真人素材、缓存写入价、技能后台修改、Token 价格规则、姿势库、方舟素材组、灵感上新、参考视频计费 ×2、短剧草稿、邮件记录）。部署前必须先备份数据库。
- 重新生成：画布契约没有差异；只有 `generate-catalog.mjs` 两侧都改过，各插件目录只有一侧改动，沿用合并结果，没有重新打包插件。发现上游手改了部分目录插件的 manifest，整体重新生成会冲掉修复，这一点已写进第五节第 5 步。
- 二改保留情况：第四节清单的功能全部保留；没有被上游吸收的二改条目。
- 验证：本机没有 Bun、cgo 和 LF 检出环境，`verify-before-push.mjs` 无法运行，按第三节在容器里做了等价检查。Web：改动的 739 个文件格式检查通过（基线本来就没格式化的 149 个跳过），`typecheck`、`lint` 通过，`bun run test` 3259 个用例全部通过（其中 9 个格式门禁用例需要 git，在装了 git 的容器里重跑通过）。后端：在 `local/go-cgo:1.25`（Go 1.25.14）容器里，`gofmt -l .` 没有输出；非 `internal/app` 的 41 个包全部通过（`internal/database` 含新增的 3 个二改迁移用例）；`internal/app` 的 1624 个顶层用例分 4 片全部通过。
- 本地未覆盖：`golangci-lint`（Agent 静态检查）、PostgreSQL / Redis 集成用例、依赖 Node Agent runtime 的用例、支付插件双架构校验，由 CI 兜底。
- 未执行的事项：推送前要确认；推送后要确认 Actions「Publish container images」的结果；生产部署和真实数据库迁移都没有做。

### 2026-11-04：现状盘点（尚未同步）

- 上游 `main`：`cc229fef`（界面多语言 i18n 第一期），最近发布 `v1.7.3.5`（`7c14ffdd`）。
- 共同祖先：`4d25e4a5`（位于 `v1.7.3.4` 之后第 3 个提交）。
- 本仓库 `main`：`4c647a9a`，有 2 个二改提交，见第四节。
- 待吸收的上游提交 63 个；数据库结构版本 78 → 81（上游新增 3 个迁移）；`VERSION` 从 `v1.7.3.4` 变为 `v1.7.3.5`。
- `git merge-tree` 预检只有 `docs/content/docs/progress/pending-test.mdx` 冲突；`docs/content/docs/backend/http-api.mdx` 和 `web/src/lib/generation-error.ts` 可以自动合并。

### 记录模板

```markdown
### YYYY-MM-DD：<旧 describe> → <新 describe>

- 上游基线：`<完整哈希>`（`git describe --tags` 输出），吸收 N 个提交。
- 同步前 main：`<哈希>`；回滚标签：`backup/canvas-hajimi-before-upstream-<日期>`。
- 冲突文件及处理方式：……
- 数据库结构版本：A → B，新增迁移：……（无迁移则写“无”）。
- 重新生成：画布契约有无差异；重新打包的插件：……
- 二改保留情况：逐项列出第四节清单的回归结果；被上游吸收而删除的条目。
- 验证：`verify-before-push.mjs` 总结行；本地未覆盖项；推送后 Actions 结果。
- 未执行的事项：生产部署、真实数据库迁移等。
```
