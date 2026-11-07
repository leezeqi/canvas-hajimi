# UUToken 视频接口字段

## 协议身份

- 插件 ID：`uutoken-video`。
- Provider ID：`uutoken-video`（与插件 ID 相同）。
- 可见性：仅管理员可见，与 `aotian-video`、`sd25pp-video` 相同。普通用户的插件中心、插件状态和协议目录不返回该插件，自定义渠道也不能使用；管理员把它配置到系统渠道模型后，普通用户可以通过该系统渠道正常生成，插件被平台停用时一并失败。
- 能力：`video`。
- 默认 Base URL：`https://uutoken.sbs`，末尾不要带 `/v1`（接口路径已包含 `/v1`）。
- 鉴权：`Authorization: Bearer <apiKey>`，使用 UUToken 令牌（`sk-` 开头）。
- 创建：`POST /v1/videos`（`application/json`）。
- 查询：`GET /v1/videos/{task_id}`，任务 ID 取创建响应的 `id`（兼容 `task_id`）。
- 下载：成功后优先使用查询响应中的 `video_url` / `url` / `result_url`；没有直链时，`GET /v1/videos/{task_id}/content` 带同一个 Bearer Key 下载。
- 取消：上游未提供，插件不声明取消操作。
- 轮询：声明 `detachedPolling`，提交后释放 worker，由任务调度按间隔回查原任务。

## 配置字段

| 字段 | 类型 | 必填 | 含义 |
| --- | --- | --- | --- |
| `apiKey` | secret | 是 | UUToken 令牌，以 `Authorization: Bearer` 发送 |

## 模型族与请求体

模型名去掉首尾空白并转小写后按下表顺序匹配，命中第一项即使用该格式；分组调用名（如 `mg-sd25-1-pro@g-...`）同样按包含关系匹配。

| 模型族 | 匹配规则 | 时长 | 画幅 | 分辨率 | 参考素材 |
| --- | --- | --- | --- | --- | --- |
| sd2.5-gf | 含 `sd2.5-gf` | 数字 `duration` | `metadata.payload.aspectRatio` | 不发送（由模型名固定） | 全部写入 `metadata.payload` JSON 字符串，见下文 |
| kling | 含 `kling-` | 数字 `seconds` | `aspect_ratio` | 转大写（`720P`） | `start_frame` / `end_frame`；`kling-o3` 另有 `reference_images`、`input_video` |
| sora-v3 | 含 `sora-v3` | 字符串 `seconds`（`"10"`/`"15"`） | `aspect_ratio`（必填） | 转小写，缺省发送 `720p` | `image_urls` / `video_urls` / `audio_urls` |
| gemini-omni-flash | 含 `gemini-omni` | 数字 `seconds` | `aspect_ratio` | 不发送（固定 720p） | 首帧 `images`（≤1），其他图片 `reference_images`，第 1 个视频 `input_video` |
| Seedance-2.0-full | 含 `seedance-2.0-full` | 数字 `seconds` | `ratio` | 转小写 | 首尾帧模式发送 `first_frame` / `last_frame`；否则 `images`/`videos`/`audios` |
| 通用 | 其余模型 | 数字 `seconds` | `aspect_ratio`；含 `hss-` 时改发 `ratio` | 转小写；含 `seadanco` 或 `dreamina-seedance` 时不发送 | `images`/`videos`/`audios` URL 字符串数组 |

通用格式覆盖 `seedance-2.0-g3`、`seedance-2.5-G`、`seedance2.5高并发`、`seedance2.5-Stable-1080p`、`seedance2.5-special2-1080p`、`seedance2.0-fast-l`、`seedance2.0-mini-l`、`Seedance-2.5-super`、`sd2.5pp`、`mg-sd25-1-pro`、`nai-seedance-2.5`、`hss-2.0-933-渠道A`、`dreamina-seedance-2-5`、`seadanco2.5-480-second`、`seadanco2.5-720-second`、`wan-3.0-prime`、`wan3-Prime`、`wan3.0-超限`、`Wan3快速版` 等。各模型的时长范围、分辨率档位和素材数量上限不同，由管理员在渠道模型能力里配置；插件只做共有的宽校验和少数模型族的专属校验，超出单个模型限制时由上游返回原因。

首尾帧由图片的 `role` 决定：`first_frame` 为首帧，`last_frame` 为尾帧，其余图片视为参考图。

不发送 `size`、`generate_audio`、`audio`、`watermark`：UUToken 文档没有为通用格式声明这些字段，`gemini-omni-flash` 明确要求不要传音频开关。

## 统一字段映射

| 统一字段 | 类型 | 必填 | 上游映射 | 说明 |
| --- | --- | --- | --- | --- |
| `model` | string | 是 | `model` | 公共模型名或分组调用名，原样发送。 |
| `prompt` | string | 是 | `prompt` | 视频提示词，不能为空。 |
| `images` | media[] | 否 | 见模型族表 | 按 `order` 排序的公网 URL。 |
| `videos` | media[] | 否 | 见模型族表 | 按 `order` 排序的公网 URL。 |
| `audios` | media[] | 否 | 见模型族表 | 按 `order` 排序的公网 URL。 |
| `duration` | integer | 否 | `seconds` / `duration` | 为 0 时省略，由上游按模型默认值生成（`mg-sd25-1-pro` 默认 30 秒）。 |
| `aspectRatio` | string | 否 | `aspect_ratio` / `ratio` / `payload.aspectRatio` | 为空时省略。 |
| `resolution` | string | 否 | `resolution` | 为空时省略。 |

## 请求示例

通用格式：

```json
{
  "model": "seedance-2.0-g3",
  "prompt": "海边日落，镜头缓慢推进",
  "seconds": 5,
  "resolution": "720p",
  "aspect_ratio": "16:9",
  "images": ["https://example.com/reference.jpg"]
}
```

sd2.5-gf 参考模式（无首尾帧时）：

```json
{
  "model": "sd2.5-gf-720p",
  "prompt": "保持参考主体一致，参考动作与音乐节奏生成流畅短片",
  "duration": 5,
  "metadata": {
    "payload": "{\"aspectRatio\":\"16:9\",\"audioUrls\":[\"https://example.com/music.mp3\"],\"imageUrls\":[\"https://example.com/person.jpg\"],\"mode\":\"references\",\"videoUrls\":[\"https://example.com/motion.mp4\"]}"
  }
}
```

sd2.5-gf 有首帧或尾帧时 `mode` 为 `frames`，只发送 `firstFrameUrl` / `lastFrameUrl`，不发送参考素材数组；`bypassCopyrightReferenceLevel` 不发送，使用上游默认值 0.7。

kling：

```json
{
  "model": "kling-v3",
  "prompt": "Continue the motion smoothly from the first frame to the last frame",
  "seconds": 10,
  "aspect_ratio": "16:9",
  "resolution": "720P",
  "start_frame": "https://example.com/first-frame.jpg",
  "end_frame": "https://example.com/last-frame.jpg"
}
```

`kling-v3` 没有首帧角色的单张图片作为 `start_frame`；`kling-o3` 只把 `first_frame` 角色图作为 `start_frame`，其他图片进入 `reference_images`，第 1 个视频作为 `input_video`。

sora-v3：

```json
{
  "model": "sora-v3-933-pro",
  "prompt": "雨夜霓虹街道，镜头缓慢推进，电影感光影",
  "seconds": "10",
  "aspect_ratio": "16:9",
  "resolution": "720p",
  "image_urls": ["https://example.com/input-image.jpg"]
}
```

空的素材数组、空的画幅和空的分辨率不会发送。

## 提交前校验

| 规则 | 提示 |
| --- | --- |
| 提示词去掉首尾空白后非空 | 视频描述不能为空 |
| 时长为 0 或 2–30 | UUToken 视频时长必须是 2–30 秒的整数，具体范围以所选模型为准 |
| 画幅为空或为 16:9、9:16、1:1、4:3、3:4、21:9、3:2、2:3、adaptive | UUToken 视频画幅只支持 … |
| 分辨率为空或为 480p、720p、1080p、2160p（大小写不敏感） | UUToken 视频分辨率只支持 … |
| 有尾帧时必须有首帧；首帧、尾帧各最多 1 张 | 对应提示 |
| Seedance-2.0-full / sd2.5-gf 首尾帧模式不带其他参考素材 | 首尾帧模式不能同时携带参考图、参考视频或参考音频 |
| kling 不带参考音频；kling-v3 只有首帧和尾帧，不带参考视频 | 对应提示 |
| kling-o3 参考图 ≤ 7（带源视频时 ≤ 4），源视频 ≤ 1，首尾帧不与参考图或源视频混用 | 对应提示 |
| sora-v3 时长为 10 或 15，必须指定画幅；图 ≤ 9、视频 ≤ 3、音频 ≤ 3，音频须搭配图片 | 对应提示 |
| gemini-omni-flash 不带音频和尾帧；首帧 ≤ 1、参考图 ≤ 3、源视频 ≤ 1，合计 ≤ 4；时长 3–10；画幅 16:9 / 9:16 | 对应提示 |

素材 URL 必须能被上游从公网访问：本机、内网或需要登录的地址会被上游拒收，部署需要对外可访问的 HTTPS 资源地址。

## 响应映射

| 统一字段 | 来源 | 说明 |
| --- | --- | --- |
| 任务 ID | `id` → `task_id` → `data.id` → `data.task_id` | 创建响应同时返回 `id` 与 `task_id`。 |
| 状态 | `status` → `data.status` | `NOT_START`、`queued`、`pending` 为排队；`IN_PROGRESS`、`in_progress`、`processing`、`running` 为生成中；`SUCCESS`、`succeeded`、`completed` 为成功；`FAILURE`、`failed`、`error` 为失败。 |
| 阶段 | `status` | 排队显示“上游排队中”，生成中显示“上游生成中”。 |
| 错误信息 | `error.message` → `error` → `fail_reason` → `message` | 失败时展示。 |
| 视频 | 成功状态下 `video_url` → `url` → `result_url` → `data.video_url` → `data.url` | 临时链接（`resultEphemeral`），由宿主下载并持久化；全部缺失时走 `/content`。 |
| 错误码 | `error.code` | 非空即判失败。 |

`sd2.5-gf` 系列成功后的 `video_url` 是官方对象存储的签名下载链接，上游要求不要把 Bearer Key 转发到该域名。插件优先把直链交给宿主持久化；走 `/content` 时上游返回 302 跳转到同一链接，宿主出站客户端跟随跨域跳转时按 Go 标准库规则丢弃 `Authorization` 头。

## 已知边界

- UUToken 文档没有给出查询响应示例，响应字段按文档文字说明映射；首次接入请用真实 Key 跑一次文生视频和参考生视频，确认状态值与成片地址。
- 文档没有说明多余字段是被忽略还是返回 400。插件只发送各模型族文档中出现的字段。
- 失败任务由上游全额退款；HTTP 200 只表示任务已受理，不等于生成成功。

<!-- YINGCE_MANIFEST_CONTRACT_START -->
## Manifest 完整接口定义

以下 JSON 与插件包内实际 `manifest.json` 逐字段一致，覆盖插件身份、权限、配置、鉴权、参数、校验、创建、Agent、查询、取消、结果下载、响应和 Agent 响应映射。`documentation` 字段的值就是当前完整文档；为避免文档在自身内部无限递归，JSON 中仅用等义占位文本表示正文。

```json
{
  "apiVersion": "yingce.plugin/v2",
  "id": "uutoken-video",
  "name": "UUToken 视频",
  "version": "1.0.0",
  "author": "影策",
  "description": "UUToken（uutoken.sbs）统一视频协议：JSON 创建 /v1/videos，整数 seconds、aspect_ratio、小写 resolution 与 images/videos/audios URL 字符串数组；sd2.5-gf、kling、sora-v3、gemini-omni-flash、Seedance-2.0-full 按模型名切换专属字段；查询原任务，成片优先取直链，缺失时走 /content 鉴权下载。",
  "permissions": [
    "generation.run",
    "media.read"
  ],
  "configuration": {
    "fields": [
      {
        "name": "apiKey",
        "type": "secret",
        "label": "API Key",
        "required": true
      }
    ]
  },
  "contributes": {
    "providers": [
      {
        "id": "uutoken-video",
        "label": "UUToken 视频",
        "capabilities": [
          "video"
        ],
        "scopes": [
          "admin.system-channel",
          "user.custom-channel",
          "canvas",
          "creation",
          "agent"
        ],
        "baseUrl": "https://uutoken.sbs",
        "requiresPublicMediaUrls": true,
        "detachedPolling": true,
        "auth": {
          "type": "bearer",
          "field": "apiKey"
        },
        "parameters": [
          {
            "name": "model",
            "type": "string",
            "required": true,
            "mapping": "model",
            "description": "UUToken 模型广场的公共模型名或账户有权使用的分组调用名，大小写、连字符和中文原样发送。"
          },
          {
            "name": "prompt",
            "type": "string",
            "required": true,
            "mapping": "prompt",
            "description": "视频提示词，不能为空。"
          },
          {
            "name": "images",
            "type": "media[]",
            "required": false,
            "mapping": "images / first_frame / start_frame / image_urls / reference_images / metadata.payload",
            "description": "按 order 排序的公网 URL。通用模型发送 images 字符串数组；首尾帧（role=first_frame/last_frame）与参考图按模型族映射到各自字段。"
          },
          {
            "name": "videos",
            "type": "media[]",
            "required": false,
            "mapping": "videos / video_urls / input_video / metadata.payload",
            "description": "按 order 排序的公网 URL；kling-o3 与 gemini-omni-flash 只取第 1 个作为 input_video。"
          },
          {
            "name": "audios",
            "type": "media[]",
            "required": false,
            "mapping": "audios / audio_urls / metadata.payload",
            "description": "按 order 排序的公网 URL；kling 与 gemini-omni-flash 不支持，提交前拒绝。"
          },
          {
            "name": "duration",
            "type": "integer",
            "required": false,
            "mapping": "seconds / duration",
            "description": "整数秒；通用模型发送数字 seconds，sora-v3 发送字符串 seconds，sd2.5-gf 发送 duration；为 0 时省略。"
          },
          {
            "name": "aspectRatio",
            "type": "string",
            "required": false,
            "values": [
              "16:9",
              "9:16",
              "1:1",
              "4:3",
              "3:4",
              "21:9",
              "3:2",
              "2:3",
              "adaptive"
            ],
            "mapping": "aspect_ratio / ratio / metadata.payload.aspectRatio",
            "description": "画幅原样发送；hss 与 Seedance-2.0-full 发送 ratio，sd2.5-gf 写入 payload.aspectRatio；为空时省略。"
          },
          {
            "name": "resolution",
            "type": "string",
            "required": false,
            "values": [
              "480p",
              "720p",
              "1080p",
              "2160p"
            ],
            "mapping": "resolution",
            "description": "通用模型转小写发送，kling 转大写发送；seadanco、dreamina、sd2.5-gf、gemini-omni-flash 的分辨率由模型名固定，不发送。"
          }
        ],
        "validations": [
          {
            "assert": {
              "$gt": [
                {
                  "$len": {
                    "$trim": {
                      "$ref": "request.prompt"
                    }
                  }
                },
                0
              ]
            },
            "message": "视频描述不能为空"
          },
          {
            "assert": {
              "$or": [
                {
                  "$eq": [
                    {
                      "$ref": "request.duration"
                    },
                    0
                  ]
                },
                {
                  "$and": [
                    {
                      "$gte": [
                        {
                          "$ref": "request.duration"
                        },
                        2
                      ]
                    },
                    {
                      "$lte": [
                        {
                          "$ref": "request.duration"
                        },
                        30
                      ]
                    }
                  ]
                }
              ]
            },
            "message": "UUToken 视频时长必须是 2–30 秒的整数，具体范围以所选模型为准"
          },
          {
            "assert": {
              "$or": [
                {
                  "$eq": [
                    {
                      "$trim": {
                        "$ref": "request.aspectRatio"
                      }
                    },
                    ""
                  ]
                },
                {
                  "$in": [
                    {
                      "$trim": {
                        "$ref": "request.aspectRatio"
                      }
                    },
                    [
                      "16:9",
                      "9:16",
                      "1:1",
                      "4:3",
                      "3:4",
                      "21:9",
                      "3:2",
                      "2:3",
                      "adaptive"
                    ]
                  ]
                }
              ]
            },
            "message": "UUToken 视频画幅只支持 16:9、9:16、1:1、4:3、3:4、21:9、3:2、2:3、adaptive"
          },
          {
            "assert": {
              "$or": [
                {
                  "$eq": [
                    {
                      "$omitEmpty": {
                        "$lower": {
                          "$trim": {
                            "$ref": "request.resolution"
                          }
                        }
                      }
                    },
                    null
                  ]
                },
                {
                  "$in": [
                    {
                      "$omitEmpty": {
                        "$lower": {
                          "$trim": {
                            "$ref": "request.resolution"
                          }
                        }
                      }
                    },
                    [
                      "480p",
                      "720p",
                      "1080p",
                      "2160p"
                    ]
                  ]
                }
              ]
            },
            "message": "UUToken 视频分辨率只支持 480p、720p、1080p、2160p，具体档位以所选模型为准"
          },
          {
            "assert": {
              "$or": [
                {
                  "$eq": [
                    {
                      "$len": {
                        "$filter": {
                          "from": {
                            "$sortByOrder": {
                              "$ref": "request.images"
                            }
                          },
                          "as": "media",
                          "where": {
                            "$eq": [
                              {
                                "$ref": "media.role"
                              },
                              "last_frame"
                            ]
                          }
                        }
                      }
                    },
                    0
                  ]
                },
                {
                  "$gt": [
                    {
                      "$len": {
                        "$filter": {
                          "from": {
                            "$sortByOrder": {
                              "$ref": "request.images"
                            }
                          },
                          "as": "media",
                          "where": {
                            "$eq": [
                              {
                                "$ref": "media.role"
                              },
                              "first_frame"
                            ]
                          }
                        }
                      }
                    },
                    0
                  ]
                }
              ]
            },
            "message": "UUToken 视频尾帧必须搭配首帧"
          },
          {
            "assert": {
              "$and": [
                {
                  "$lte": [
                    {
                      "$len": {
                        "$filter": {
                          "from": {
                            "$sortByOrder": {
                              "$ref": "request.images"
                            }
                          },
                          "as": "media",
                          "where": {
                            "$eq": [
                              {
                                "$ref": "media.role"
                              },
                              "first_frame"
                            ]
                          }
                        }
                      }
                    },
                    1
                  ]
                },
                {
                  "$lte": [
                    {
                      "$len": {
                        "$filter": {
                          "from": {
                            "$sortByOrder": {
                              "$ref": "request.images"
                            }
                          },
                          "as": "media",
                          "where": {
                            "$eq": [
                              {
                                "$ref": "media.role"
                              },
                              "last_frame"
                            ]
                          }
                        }
                      }
                    },
                    1
                  ]
                }
              ]
            },
            "message": "UUToken 视频首帧和尾帧各只能有 1 张"
          },
          {
            "assert": {
              "$or": [
                {
                  "$not": {
                    "$or": [
                      {
                        "$gt": [
                          {
                            "$len": {
                              "$split": [
                                {
                                  "$lower": {
                                    "$trim": {
                                      "$ref": "request.model"
                                    }
                                  }
                                },
                                "seedance-2.0-full"
                              ]
                            }
                          },
                          1
                        ]
                      },
                      {
                        "$gt": [
                          {
                            "$len": {
                              "$split": [
                                {
                                  "$lower": {
                                    "$trim": {
                                      "$ref": "request.model"
                                    }
                                  }
                                },
                                "sd2.5-gf"
                              ]
                            }
                          },
                          1
                        ]
                      }
                    ]
                  }
                },
                {
                  "$not": {
                    "$gt": [
                      {
                        "$add": [
                          {
                            "$len": {
                              "$filter": {
                                "from": {
                                  "$sortByOrder": {
                                    "$ref": "request.images"
                                  }
                                },
                                "as": "media",
                                "where": {
                                  "$eq": [
                                    {
                                      "$ref": "media.role"
                                    },
                                    "first_frame"
                                  ]
                                }
                              }
                            }
                          },
                          {
                            "$len": {
                              "$filter": {
                                "from": {
                                  "$sortByOrder": {
                                    "$ref": "request.images"
                                  }
                                },
                                "as": "media",
                                "where": {
                                  "$eq": [
                                    {
                                      "$ref": "media.role"
                                    },
                                    "last_frame"
                                  ]
                                }
                              }
                            }
                          }
                        ]
                      },
                      0
                    ]
                  }
                },
                {
                  "$and": [
                    {
                      "$eq": [
                        {
                          "$len": {
                            "$filter": {
                              "from": {
                                "$sortByOrder": {
                                  "$ref": "request.images"
                                }
                              },
                              "as": "media",
                              "where": {
                                "$not": {
                                  "$in": [
                                    {
                                      "$ref": "media.role"
                                    },
                                    [
                                      "first_frame",
                                      "last_frame"
                                    ]
                                  ]
                                }
                              }
                            }
                          }
                        },
                        0
                      ]
                    },
                    {
                      "$eq": [
                        {
                          "$len": {
                            "$ref": "request.videos"
                          }
                        },
                        0
                      ]
                    },
                    {
                      "$eq": [
                        {
                          "$len": {
                            "$ref": "request.audios"
                          }
                        },
                        0
                      ]
                    }
                  ]
                }
              ]
            },
            "message": "Seedance-2.0-full 与 sd2.5-gf 的首尾帧模式不能同时携带参考图、参考视频或参考音频"
          },
          {
            "assert": {
              "$or": [
                {
                  "$not": {
                    "$gt": [
                      {
                        "$len": {
                          "$split": [
                            {
                              "$lower": {
                                "$trim": {
                                  "$ref": "request.model"
                                }
                              }
                            },
                            "kling-"
                          ]
                        }
                      },
                      1
                    ]
                  }
                },
                {
                  "$eq": [
                    {
                      "$len": {
                        "$ref": "request.audios"
                      }
                    },
                    0
                  ]
                }
              ]
            },
            "message": "kling 视频不支持参考音频"
          },
          {
            "assert": {
              "$or": [
                {
                  "$not": {
                    "$gt": [
                      {
                        "$len": {
                          "$split": [
                            {
                              "$lower": {
                                "$trim": {
                                  "$ref": "request.model"
                                }
                              }
                            },
                            "kling-"
                          ]
                        }
                      },
                      1
                    ]
                  }
                },
                {
                  "$gt": [
                    {
                      "$len": {
                        "$split": [
                          {
                            "$lower": {
                              "$trim": {
                                "$ref": "request.model"
                              }
                            }
                          },
                          "kling-o3"
                        ]
                      }
                    },
                    1
                  ]
                },
                {
                  "$and": [
                    {
                      "$eq": [
                        {
                          "$len": {
                            "$ref": "request.videos"
                          }
                        },
                        0
                      ]
                    },
                    {
                      "$lte": [
                        {
                          "$add": [
                            {
                              "$len": {
                                "$filter": {
                                  "from": {
                                    "$sortByOrder": {
                                      "$ref": "request.images"
                                    }
                                  },
                                  "as": "media",
                                  "where": {
                                    "$not": {
                                      "$in": [
                                        {
                                          "$ref": "media.role"
                                        },
                                        [
                                          "first_frame",
                                          "last_frame"
                                        ]
                                      ]
                                    }
                                  }
                                }
                              }
                            },
                            {
                              "$len": {
                                "$filter": {
                                  "from": {
                                    "$sortByOrder": {
                                      "$ref": "request.images"
                                    }
                                  },
                                  "as": "media",
                                  "where": {
                                    "$eq": [
                                      {
                                        "$ref": "media.role"
                                      },
                                      "first_frame"
                                    ]
                                  }
                                }
                              }
                            }
                          ]
                        },
                        1
                      ]
                    }
                  ]
                }
              ]
            },
            "message": "kling-v3 只支持首帧和尾帧各 1 张，不支持多图参考和参考视频"
          },
          {
            "assert": {
              "$or": [
                {
                  "$not": {
                    "$gt": [
                      {
                        "$len": {
                          "$split": [
                            {
                              "$lower": {
                                "$trim": {
                                  "$ref": "request.model"
                                }
                              }
                            },
                            "kling-o3"
                          ]
                        }
                      },
                      1
                    ]
                  }
                },
                {
                  "$and": [
                    {
                      "$lte": [
                        {
                          "$len": {
                            "$ref": "request.videos"
                          }
                        },
                        1
                      ]
                    },
                    {
                      "$lte": [
                        {
                          "$len": {
                            "$filter": {
                              "from": {
                                "$sortByOrder": {
                                  "$ref": "request.images"
                                }
                              },
                              "as": "media",
                              "where": {
                                "$not": {
                                  "$in": [
                                    {
                                      "$ref": "media.role"
                                    },
                                    [
                                      "first_frame",
                                      "last_frame"
                                    ]
                                  ]
                                }
                              }
                            }
                          }
                        },
                        7
                      ]
                    },
                    {
                      "$or": [
                        {
                          "$eq": [
                            {
                              "$len": {
                                "$ref": "request.videos"
                              }
                            },
                            0
                          ]
                        },
                        {
                          "$lte": [
                            {
                              "$len": {
                                "$filter": {
                                  "from": {
                                    "$sortByOrder": {
                                      "$ref": "request.images"
                                    }
                                  },
                                  "as": "media",
                                  "where": {
                                    "$not": {
                                      "$in": [
                                        {
                                          "$ref": "media.role"
                                        },
                                        [
                                          "first_frame",
                                          "last_frame"
                                        ]
                                      ]
                                    }
                                  }
                                }
                              }
                            },
                            4
                          ]
                        }
                      ]
                    },
                    {
                      "$or": [
                        {
                          "$not": {
                            "$gt": [
                              {
                                "$add": [
                                  {
                                    "$len": {
                                      "$filter": {
                                        "from": {
                                          "$sortByOrder": {
                                            "$ref": "request.images"
                                          }
                                        },
                                        "as": "media",
                                        "where": {
                                          "$eq": [
                                            {
                                              "$ref": "media.role"
                                            },
                                            "first_frame"
                                          ]
                                        }
                                      }
                                    }
                                  },
                                  {
                                    "$len": {
                                      "$filter": {
                                        "from": {
                                          "$sortByOrder": {
                                            "$ref": "request.images"
                                          }
                                        },
                                        "as": "media",
                                        "where": {
                                          "$eq": [
                                            {
                                              "$ref": "media.role"
                                            },
                                            "last_frame"
                                          ]
                                        }
                                      }
                                    }
                                  }
                                ]
                              },
                              0
                            ]
                          }
                        },
                        {
                          "$and": [
                            {
                              "$eq": [
                                {
                                  "$len": {
                                    "$filter": {
                                      "from": {
                                        "$sortByOrder": {
                                          "$ref": "request.images"
                                        }
                                      },
                                      "as": "media",
                                      "where": {
                                        "$not": {
                                          "$in": [
                                            {
                                              "$ref": "media.role"
                                            },
                                            [
                                              "first_frame",
                                              "last_frame"
                                            ]
                                          ]
                                        }
                                      }
                                    }
                                  }
                                },
                                0
                              ]
                            },
                            {
                              "$eq": [
                                {
                                  "$len": {
                                    "$ref": "request.videos"
                                  }
                                },
                                0
                              ]
                            }
                          ]
                        }
                      ]
                    }
                  ]
                }
              ]
            },
            "message": "kling-o3 参考图最多 7 张（带源视频时最多 4 张），源视频最多 1 个，首尾帧不能与参考图或源视频同时使用"
          },
          {
            "assert": {
              "$or": [
                {
                  "$not": {
                    "$gt": [
                      {
                        "$len": {
                          "$split": [
                            {
                              "$lower": {
                                "$trim": {
                                  "$ref": "request.model"
                                }
                              }
                            },
                            "sora-v3"
                          ]
                        }
                      },
                      1
                    ]
                  }
                },
                {
                  "$in": [
                    {
                      "$ref": "request.duration"
                    },
                    [
                      10,
                      15
                    ]
                  ]
                }
              ]
            },
            "message": "sora-v3 视频时长只支持 10 秒或 15 秒"
          },
          {
            "assert": {
              "$or": [
                {
                  "$not": {
                    "$gt": [
                      {
                        "$len": {
                          "$split": [
                            {
                              "$lower": {
                                "$trim": {
                                  "$ref": "request.model"
                                }
                              }
                            },
                            "sora-v3"
                          ]
                        }
                      },
                      1
                    ]
                  }
                },
                {
                  "$not": {
                    "$eq": [
                      {
                        "$trim": {
                          "$ref": "request.aspectRatio"
                        }
                      },
                      ""
                    ]
                  }
                }
              ]
            },
            "message": "sora-v3 视频必须指定画幅"
          },
          {
            "assert": {
              "$or": [
                {
                  "$not": {
                    "$gt": [
                      {
                        "$len": {
                          "$split": [
                            {
                              "$lower": {
                                "$trim": {
                                  "$ref": "request.model"
                                }
                              }
                            },
                            "sora-v3"
                          ]
                        }
                      },
                      1
                    ]
                  }
                },
                {
                  "$and": [
                    {
                      "$lte": [
                        {
                          "$len": {
                            "$ref": "request.images"
                          }
                        },
                        9
                      ]
                    },
                    {
                      "$lte": [
                        {
                          "$len": {
                            "$ref": "request.videos"
                          }
                        },
                        3
                      ]
                    },
                    {
                      "$lte": [
                        {
                          "$len": {
                            "$ref": "request.audios"
                          }
                        },
                        3
                      ]
                    },
                    {
                      "$or": [
                        {
                          "$eq": [
                            {
                              "$len": {
                                "$ref": "request.audios"
                              }
                            },
                            0
                          ]
                        },
                        {
                          "$gt": [
                            {
                              "$len": {
                                "$ref": "request.images"
                              }
                            },
                            0
                          ]
                        }
                      ]
                    }
                  ]
                }
              ]
            },
            "message": "sora-v3 参考图最多 9 张、参考视频和参考音频各最多 3 个，参考音频必须搭配参考图"
          },
          {
            "assert": {
              "$or": [
                {
                  "$not": {
                    "$gt": [
                      {
                        "$len": {
                          "$split": [
                            {
                              "$lower": {
                                "$trim": {
                                  "$ref": "request.model"
                                }
                              }
                            },
                            "gemini-omni"
                          ]
                        }
                      },
                      1
                    ]
                  }
                },
                {
                  "$and": [
                    {
                      "$eq": [
                        {
                          "$len": {
                            "$ref": "request.audios"
                          }
                        },
                        0
                      ]
                    },
                    {
                      "$eq": [
                        {
                          "$len": {
                            "$filter": {
                              "from": {
                                "$sortByOrder": {
                                  "$ref": "request.images"
                                }
                              },
                              "as": "media",
                              "where": {
                                "$eq": [
                                  {
                                    "$ref": "media.role"
                                  },
                                  "last_frame"
                                ]
                              }
                            }
                          }
                        },
                        0
                      ]
                    },
                    {
                      "$lte": [
                        {
                          "$len": {
                            "$ref": "request.videos"
                          }
                        },
                        1
                      ]
                    },
                    {
                      "$lte": [
                        {
                          "$len": {
                            "$filter": {
                              "from": {
                                "$sortByOrder": {
                                  "$ref": "request.images"
                                }
                              },
                              "as": "media",
                              "where": {
                                "$not": {
                                  "$in": [
                                    {
                                      "$ref": "media.role"
                                    },
                                    [
                                      "first_frame",
                                      "last_frame"
                                    ]
                                  ]
                                }
                              }
                            }
                          }
                        },
                        3
                      ]
                    },
                    {
                      "$lte": [
                        {
                          "$add": [
                            {
                              "$len": {
                                "$ref": "request.images"
                              }
                            },
                            {
                              "$len": {
                                "$ref": "request.videos"
                              }
                            }
                          ]
                        },
                        4
                      ]
                    }
                  ]
                }
              ]
            },
            "message": "gemini-omni-flash 不支持参考音频和尾帧；首帧最多 1 张、参考图最多 3 张、源视频最多 1 个，合计最多 4 项"
          },
          {
            "assert": {
              "$or": [
                {
                  "$not": {
                    "$gt": [
                      {
                        "$len": {
                          "$split": [
                            {
                              "$lower": {
                                "$trim": {
                                  "$ref": "request.model"
                                }
                              }
                            },
                            "gemini-omni"
                          ]
                        }
                      },
                      1
                    ]
                  }
                },
                {
                  "$eq": [
                    {
                      "$ref": "request.duration"
                    },
                    0
                  ]
                },
                {
                  "$and": [
                    {
                      "$gte": [
                        {
                          "$ref": "request.duration"
                        },
                        3
                      ]
                    },
                    {
                      "$lte": [
                        {
                          "$ref": "request.duration"
                        },
                        10
                      ]
                    }
                  ]
                }
              ]
            },
            "message": "gemini-omni-flash 视频时长必须是 3–10 秒"
          },
          {
            "assert": {
              "$or": [
                {
                  "$not": {
                    "$gt": [
                      {
                        "$len": {
                          "$split": [
                            {
                              "$lower": {
                                "$trim": {
                                  "$ref": "request.model"
                                }
                              }
                            },
                            "gemini-omni"
                          ]
                        }
                      },
                      1
                    ]
                  }
                },
                {
                  "$eq": [
                    {
                      "$trim": {
                        "$ref": "request.aspectRatio"
                      }
                    },
                    ""
                  ]
                },
                {
                  "$in": [
                    {
                      "$trim": {
                        "$ref": "request.aspectRatio"
                      }
                    },
                    [
                      "16:9",
                      "9:16"
                    ]
                  ]
                }
              ]
            },
            "message": "gemini-omni-flash 画幅只支持 16:9 和 9:16"
          }
        ],
        "create": {
          "method": "POST",
          "path": "/v1/videos",
          "contentType": "application/json",
          "body": {
            "$switch": {
              "cases": [
                {
                  "when": {
                    "$gt": [
                      {
                        "$len": {
                          "$split": [
                            {
                              "$lower": {
                                "$trim": {
                                  "$ref": "request.model"
                                }
                              }
                            },
                            "sd2.5-gf"
                          ]
                        }
                      },
                      1
                    ]
                  },
                  "then": {
                    "model": {
                      "$ref": "request.model"
                    },
                    "prompt": {
                      "$ref": "request.prompt"
                    },
                    "duration": {
                      "$if": {
                        "condition": {
                          "$gt": [
                            {
                              "$ref": "request.duration"
                            },
                            0
                          ]
                        },
                        "then": {
                          "$toInt": {
                            "$ref": "request.duration"
                          }
                        },
                        "else": null
                      }
                    },
                    "metadata": {
                      "payload": {
                        "$json": {
                          "aspectRatio": {
                            "$omitEmpty": {
                              "$trim": {
                                "$ref": "request.aspectRatio"
                              }
                            }
                          },
                          "mode": {
                            "$if": {
                              "condition": {
                                "$gt": [
                                  {
                                    "$add": [
                                      {
                                        "$len": {
                                          "$filter": {
                                            "from": {
                                              "$sortByOrder": {
                                                "$ref": "request.images"
                                              }
                                            },
                                            "as": "media",
                                            "where": {
                                              "$eq": [
                                                {
                                                  "$ref": "media.role"
                                                },
                                                "first_frame"
                                              ]
                                            }
                                          }
                                        }
                                      },
                                      {
                                        "$len": {
                                          "$filter": {
                                            "from": {
                                              "$sortByOrder": {
                                                "$ref": "request.images"
                                              }
                                            },
                                            "as": "media",
                                            "where": {
                                              "$eq": [
                                                {
                                                  "$ref": "media.role"
                                                },
                                                "last_frame"
                                              ]
                                            }
                                          }
                                        }
                                      }
                                    ]
                                  },
                                  0
                                ]
                              },
                              "then": "frames",
                              "else": "references"
                            }
                          },
                          "imageUrls": {
                            "$if": {
                              "condition": {
                                "$gt": [
                                  {
                                    "$add": [
                                      {
                                        "$len": {
                                          "$filter": {
                                            "from": {
                                              "$sortByOrder": {
                                                "$ref": "request.images"
                                              }
                                            },
                                            "as": "media",
                                            "where": {
                                              "$eq": [
                                                {
                                                  "$ref": "media.role"
                                                },
                                                "first_frame"
                                              ]
                                            }
                                          }
                                        }
                                      },
                                      {
                                        "$len": {
                                          "$filter": {
                                            "from": {
                                              "$sortByOrder": {
                                                "$ref": "request.images"
                                              }
                                            },
                                            "as": "media",
                                            "where": {
                                              "$eq": [
                                                {
                                                  "$ref": "media.role"
                                                },
                                                "last_frame"
                                              ]
                                            }
                                          }
                                        }
                                      }
                                    ]
                                  },
                                  0
                                ]
                              },
                              "then": null,
                              "else": {
                                "$omitEmpty": {
                                  "$map": {
                                    "from": {
                                      "$sortByOrder": {
                                        "$ref": "request.images"
                                      }
                                    },
                                    "as": "media",
                                    "in": {
                                      "$ref": "media.value"
                                    }
                                  }
                                }
                              }
                            }
                          },
                          "videoUrls": {
                            "$if": {
                              "condition": {
                                "$gt": [
                                  {
                                    "$add": [
                                      {
                                        "$len": {
                                          "$filter": {
                                            "from": {
                                              "$sortByOrder": {
                                                "$ref": "request.images"
                                              }
                                            },
                                            "as": "media",
                                            "where": {
                                              "$eq": [
                                                {
                                                  "$ref": "media.role"
                                                },
                                                "first_frame"
                                              ]
                                            }
                                          }
                                        }
                                      },
                                      {
                                        "$len": {
                                          "$filter": {
                                            "from": {
                                              "$sortByOrder": {
                                                "$ref": "request.images"
                                              }
                                            },
                                            "as": "media",
                                            "where": {
                                              "$eq": [
                                                {
                                                  "$ref": "media.role"
                                                },
                                                "last_frame"
                                              ]
                                            }
                                          }
                                        }
                                      }
                                    ]
                                  },
                                  0
                                ]
                              },
                              "then": null,
                              "else": {
                                "$omitEmpty": {
                                  "$map": {
                                    "from": {
                                      "$sortByOrder": {
                                        "$ref": "request.videos"
                                      }
                                    },
                                    "as": "media",
                                    "in": {
                                      "$ref": "media.value"
                                    }
                                  }
                                }
                              }
                            }
                          },
                          "audioUrls": {
                            "$if": {
                              "condition": {
                                "$gt": [
                                  {
                                    "$add": [
                                      {
                                        "$len": {
                                          "$filter": {
                                            "from": {
                                              "$sortByOrder": {
                                                "$ref": "request.images"
                                              }
                                            },
                                            "as": "media",
                                            "where": {
                                              "$eq": [
                                                {
                                                  "$ref": "media.role"
                                                },
                                                "first_frame"
                                              ]
                                            }
                                          }
                                        }
                                      },
                                      {
                                        "$len": {
                                          "$filter": {
                                            "from": {
                                              "$sortByOrder": {
                                                "$ref": "request.images"
                                              }
                                            },
                                            "as": "media",
                                            "where": {
                                              "$eq": [
                                                {
                                                  "$ref": "media.role"
                                                },
                                                "last_frame"
                                              ]
                                            }
                                          }
                                        }
                                      }
                                    ]
                                  },
                                  0
                                ]
                              },
                              "then": null,
                              "else": {
                                "$omitEmpty": {
                                  "$map": {
                                    "from": {
                                      "$sortByOrder": {
                                        "$ref": "request.audios"
                                      }
                                    },
                                    "as": "media",
                                    "in": {
                                      "$ref": "media.value"
                                    }
                                  }
                                }
                              }
                            }
                          },
                          "firstFrameUrl": {
                            "$if": {
                              "condition": {
                                "$gt": [
                                  {
                                    "$add": [
                                      {
                                        "$len": {
                                          "$filter": {
                                            "from": {
                                              "$sortByOrder": {
                                                "$ref": "request.images"
                                              }
                                            },
                                            "as": "media",
                                            "where": {
                                              "$eq": [
                                                {
                                                  "$ref": "media.role"
                                                },
                                                "first_frame"
                                              ]
                                            }
                                          }
                                        }
                                      },
                                      {
                                        "$len": {
                                          "$filter": {
                                            "from": {
                                              "$sortByOrder": {
                                                "$ref": "request.images"
                                              }
                                            },
                                            "as": "media",
                                            "where": {
                                              "$eq": [
                                                {
                                                  "$ref": "media.role"
                                                },
                                                "last_frame"
                                              ]
                                            }
                                          }
                                        }
                                      }
                                    ]
                                  },
                                  0
                                ]
                              },
                              "then": {
                                "$first": {
                                  "$map": {
                                    "from": {
                                      "$filter": {
                                        "from": {
                                          "$sortByOrder": {
                                            "$ref": "request.images"
                                          }
                                        },
                                        "as": "media",
                                        "where": {
                                          "$eq": [
                                            {
                                              "$ref": "media.role"
                                            },
                                            "first_frame"
                                          ]
                                        }
                                      }
                                    },
                                    "as": "media",
                                    "in": {
                                      "$ref": "media.value"
                                    }
                                  }
                                }
                              },
                              "else": null
                            }
                          },
                          "lastFrameUrl": {
                            "$if": {
                              "condition": {
                                "$gt": [
                                  {
                                    "$add": [
                                      {
                                        "$len": {
                                          "$filter": {
                                            "from": {
                                              "$sortByOrder": {
                                                "$ref": "request.images"
                                              }
                                            },
                                            "as": "media",
                                            "where": {
                                              "$eq": [
                                                {
                                                  "$ref": "media.role"
                                                },
                                                "first_frame"
                                              ]
                                            }
                                          }
                                        }
                                      },
                                      {
                                        "$len": {
                                          "$filter": {
                                            "from": {
                                              "$sortByOrder": {
                                                "$ref": "request.images"
                                              }
                                            },
                                            "as": "media",
                                            "where": {
                                              "$eq": [
                                                {
                                                  "$ref": "media.role"
                                                },
                                                "last_frame"
                                              ]
                                            }
                                          }
                                        }
                                      }
                                    ]
                                  },
                                  0
                                ]
                              },
                              "then": {
                                "$first": {
                                  "$map": {
                                    "from": {
                                      "$filter": {
                                        "from": {
                                          "$sortByOrder": {
                                            "$ref": "request.images"
                                          }
                                        },
                                        "as": "media",
                                        "where": {
                                          "$eq": [
                                            {
                                              "$ref": "media.role"
                                            },
                                            "last_frame"
                                          ]
                                        }
                                      }
                                    },
                                    "as": "media",
                                    "in": {
                                      "$ref": "media.value"
                                    }
                                  }
                                }
                              },
                              "else": null
                            }
                          }
                        }
                      }
                    }
                  }
                },
                {
                  "when": {
                    "$gt": [
                      {
                        "$len": {
                          "$split": [
                            {
                              "$lower": {
                                "$trim": {
                                  "$ref": "request.model"
                                }
                              }
                            },
                            "kling-"
                          ]
                        }
                      },
                      1
                    ]
                  },
                  "then": {
                    "model": {
                      "$ref": "request.model"
                    },
                    "prompt": {
                      "$ref": "request.prompt"
                    },
                    "seconds": {
                      "$if": {
                        "condition": {
                          "$gt": [
                            {
                              "$ref": "request.duration"
                            },
                            0
                          ]
                        },
                        "then": {
                          "$toInt": {
                            "$ref": "request.duration"
                          }
                        },
                        "else": null
                      }
                    },
                    "aspect_ratio": {
                      "$omitEmpty": {
                        "$trim": {
                          "$ref": "request.aspectRatio"
                        }
                      }
                    },
                    "resolution": {
                      "$omitEmpty": {
                        "$upper": {
                          "$trim": {
                            "$ref": "request.resolution"
                          }
                        }
                      }
                    },
                    "start_frame": {
                      "$if": {
                        "condition": {
                          "$gt": [
                            {
                              "$len": {
                                "$split": [
                                  {
                                    "$lower": {
                                      "$trim": {
                                        "$ref": "request.model"
                                      }
                                    }
                                  },
                                  "kling-o3"
                                ]
                              }
                            },
                            1
                          ]
                        },
                        "then": {
                          "$first": {
                            "$map": {
                              "from": {
                                "$filter": {
                                  "from": {
                                    "$sortByOrder": {
                                      "$ref": "request.images"
                                    }
                                  },
                                  "as": "media",
                                  "where": {
                                    "$eq": [
                                      {
                                        "$ref": "media.role"
                                      },
                                      "first_frame"
                                    ]
                                  }
                                }
                              },
                              "as": "media",
                              "in": {
                                "$ref": "media.value"
                              }
                            }
                          }
                        },
                        "else": {
                          "$coalesce": [
                            {
                              "$first": {
                                "$map": {
                                  "from": {
                                    "$filter": {
                                      "from": {
                                        "$sortByOrder": {
                                          "$ref": "request.images"
                                        }
                                      },
                                      "as": "media",
                                      "where": {
                                        "$eq": [
                                          {
                                            "$ref": "media.role"
                                          },
                                          "first_frame"
                                        ]
                                      }
                                    }
                                  },
                                  "as": "media",
                                  "in": {
                                    "$ref": "media.value"
                                  }
                                }
                              }
                            },
                            {
                              "$first": {
                                "$map": {
                                  "from": {
                                    "$filter": {
                                      "from": {
                                        "$sortByOrder": {
                                          "$ref": "request.images"
                                        }
                                      },
                                      "as": "media",
                                      "where": {
                                        "$not": {
                                          "$in": [
                                            {
                                              "$ref": "media.role"
                                            },
                                            [
                                              "first_frame",
                                              "last_frame"
                                            ]
                                          ]
                                        }
                                      }
                                    }
                                  },
                                  "as": "media",
                                  "in": {
                                    "$ref": "media.value"
                                  }
                                }
                              }
                            }
                          ]
                        }
                      }
                    },
                    "end_frame": {
                      "$first": {
                        "$map": {
                          "from": {
                            "$filter": {
                              "from": {
                                "$sortByOrder": {
                                  "$ref": "request.images"
                                }
                              },
                              "as": "media",
                              "where": {
                                "$eq": [
                                  {
                                    "$ref": "media.role"
                                  },
                                  "last_frame"
                                ]
                              }
                            }
                          },
                          "as": "media",
                          "in": {
                            "$ref": "media.value"
                          }
                        }
                      }
                    },
                    "reference_images": {
                      "$if": {
                        "condition": {
                          "$gt": [
                            {
                              "$len": {
                                "$split": [
                                  {
                                    "$lower": {
                                      "$trim": {
                                        "$ref": "request.model"
                                      }
                                    }
                                  },
                                  "kling-o3"
                                ]
                              }
                            },
                            1
                          ]
                        },
                        "then": {
                          "$omitEmpty": {
                            "$map": {
                              "from": {
                                "$filter": {
                                  "from": {
                                    "$sortByOrder": {
                                      "$ref": "request.images"
                                    }
                                  },
                                  "as": "media",
                                  "where": {
                                    "$not": {
                                      "$in": [
                                        {
                                          "$ref": "media.role"
                                        },
                                        [
                                          "first_frame",
                                          "last_frame"
                                        ]
                                      ]
                                    }
                                  }
                                }
                              },
                              "as": "media",
                              "in": {
                                "$ref": "media.value"
                              }
                            }
                          }
                        },
                        "else": null
                      }
                    },
                    "input_video": {
                      "$if": {
                        "condition": {
                          "$gt": [
                            {
                              "$len": {
                                "$split": [
                                  {
                                    "$lower": {
                                      "$trim": {
                                        "$ref": "request.model"
                                      }
                                    }
                                  },
                                  "kling-o3"
                                ]
                              }
                            },
                            1
                          ]
                        },
                        "then": {
                          "$first": {
                            "$map": {
                              "from": {
                                "$sortByOrder": {
                                  "$ref": "request.videos"
                                }
                              },
                              "as": "media",
                              "in": {
                                "$ref": "media.value"
                              }
                            }
                          }
                        },
                        "else": null
                      }
                    }
                  }
                },
                {
                  "when": {
                    "$gt": [
                      {
                        "$len": {
                          "$split": [
                            {
                              "$lower": {
                                "$trim": {
                                  "$ref": "request.model"
                                }
                              }
                            },
                            "sora-v3"
                          ]
                        }
                      },
                      1
                    ]
                  },
                  "then": {
                    "model": {
                      "$ref": "request.model"
                    },
                    "prompt": {
                      "$ref": "request.prompt"
                    },
                    "seconds": {
                      "$if": {
                        "condition": {
                          "$gt": [
                            {
                              "$ref": "request.duration"
                            },
                            0
                          ]
                        },
                        "then": {
                          "$toString": {
                            "$toInt": {
                              "$ref": "request.duration"
                            }
                          }
                        },
                        "else": null
                      }
                    },
                    "aspect_ratio": {
                      "$omitEmpty": {
                        "$trim": {
                          "$ref": "request.aspectRatio"
                        }
                      }
                    },
                    "resolution": {
                      "$coalesce": [
                        {
                          "$omitEmpty": {
                            "$lower": {
                              "$trim": {
                                "$ref": "request.resolution"
                              }
                            }
                          }
                        },
                        "720p"
                      ]
                    },
                    "image_urls": {
                      "$omitEmpty": {
                        "$map": {
                          "from": {
                            "$sortByOrder": {
                              "$ref": "request.images"
                            }
                          },
                          "as": "media",
                          "in": {
                            "$ref": "media.value"
                          }
                        }
                      }
                    },
                    "video_urls": {
                      "$omitEmpty": {
                        "$map": {
                          "from": {
                            "$sortByOrder": {
                              "$ref": "request.videos"
                            }
                          },
                          "as": "media",
                          "in": {
                            "$ref": "media.value"
                          }
                        }
                      }
                    },
                    "audio_urls": {
                      "$omitEmpty": {
                        "$map": {
                          "from": {
                            "$sortByOrder": {
                              "$ref": "request.audios"
                            }
                          },
                          "as": "media",
                          "in": {
                            "$ref": "media.value"
                          }
                        }
                      }
                    }
                  }
                },
                {
                  "when": {
                    "$gt": [
                      {
                        "$len": {
                          "$split": [
                            {
                              "$lower": {
                                "$trim": {
                                  "$ref": "request.model"
                                }
                              }
                            },
                            "gemini-omni"
                          ]
                        }
                      },
                      1
                    ]
                  },
                  "then": {
                    "model": {
                      "$ref": "request.model"
                    },
                    "prompt": {
                      "$ref": "request.prompt"
                    },
                    "seconds": {
                      "$if": {
                        "condition": {
                          "$gt": [
                            {
                              "$ref": "request.duration"
                            },
                            0
                          ]
                        },
                        "then": {
                          "$toInt": {
                            "$ref": "request.duration"
                          }
                        },
                        "else": null
                      }
                    },
                    "aspect_ratio": {
                      "$omitEmpty": {
                        "$trim": {
                          "$ref": "request.aspectRatio"
                        }
                      }
                    },
                    "images": {
                      "$omitEmpty": {
                        "$map": {
                          "from": {
                            "$filter": {
                              "from": {
                                "$sortByOrder": {
                                  "$ref": "request.images"
                                }
                              },
                              "as": "media",
                              "where": {
                                "$eq": [
                                  {
                                    "$ref": "media.role"
                                  },
                                  "first_frame"
                                ]
                              }
                            }
                          },
                          "as": "media",
                          "in": {
                            "$ref": "media.value"
                          }
                        }
                      }
                    },
                    "reference_images": {
                      "$omitEmpty": {
                        "$map": {
                          "from": {
                            "$filter": {
                              "from": {
                                "$sortByOrder": {
                                  "$ref": "request.images"
                                }
                              },
                              "as": "media",
                              "where": {
                                "$not": {
                                  "$in": [
                                    {
                                      "$ref": "media.role"
                                    },
                                    [
                                      "first_frame",
                                      "last_frame"
                                    ]
                                  ]
                                }
                              }
                            }
                          },
                          "as": "media",
                          "in": {
                            "$ref": "media.value"
                          }
                        }
                      }
                    },
                    "input_video": {
                      "$first": {
                        "$map": {
                          "from": {
                            "$sortByOrder": {
                              "$ref": "request.videos"
                            }
                          },
                          "as": "media",
                          "in": {
                            "$ref": "media.value"
                          }
                        }
                      }
                    }
                  }
                },
                {
                  "when": {
                    "$gt": [
                      {
                        "$len": {
                          "$split": [
                            {
                              "$lower": {
                                "$trim": {
                                  "$ref": "request.model"
                                }
                              }
                            },
                            "seedance-2.0-full"
                          ]
                        }
                      },
                      1
                    ]
                  },
                  "then": {
                    "model": {
                      "$ref": "request.model"
                    },
                    "prompt": {
                      "$ref": "request.prompt"
                    },
                    "seconds": {
                      "$if": {
                        "condition": {
                          "$gt": [
                            {
                              "$ref": "request.duration"
                            },
                            0
                          ]
                        },
                        "then": {
                          "$toInt": {
                            "$ref": "request.duration"
                          }
                        },
                        "else": null
                      }
                    },
                    "ratio": {
                      "$omitEmpty": {
                        "$trim": {
                          "$ref": "request.aspectRatio"
                        }
                      }
                    },
                    "resolution": {
                      "$omitEmpty": {
                        "$lower": {
                          "$trim": {
                            "$ref": "request.resolution"
                          }
                        }
                      }
                    },
                    "first_frame": {
                      "$if": {
                        "condition": {
                          "$gt": [
                            {
                              "$add": [
                                {
                                  "$len": {
                                    "$filter": {
                                      "from": {
                                        "$sortByOrder": {
                                          "$ref": "request.images"
                                        }
                                      },
                                      "as": "media",
                                      "where": {
                                        "$eq": [
                                          {
                                            "$ref": "media.role"
                                          },
                                          "first_frame"
                                        ]
                                      }
                                    }
                                  }
                                },
                                {
                                  "$len": {
                                    "$filter": {
                                      "from": {
                                        "$sortByOrder": {
                                          "$ref": "request.images"
                                        }
                                      },
                                      "as": "media",
                                      "where": {
                                        "$eq": [
                                          {
                                            "$ref": "media.role"
                                          },
                                          "last_frame"
                                        ]
                                      }
                                    }
                                  }
                                }
                              ]
                            },
                            0
                          ]
                        },
                        "then": {
                          "$first": {
                            "$map": {
                              "from": {
                                "$filter": {
                                  "from": {
                                    "$sortByOrder": {
                                      "$ref": "request.images"
                                    }
                                  },
                                  "as": "media",
                                  "where": {
                                    "$eq": [
                                      {
                                        "$ref": "media.role"
                                      },
                                      "first_frame"
                                    ]
                                  }
                                }
                              },
                              "as": "media",
                              "in": {
                                "$ref": "media.value"
                              }
                            }
                          }
                        },
                        "else": null
                      }
                    },
                    "last_frame": {
                      "$if": {
                        "condition": {
                          "$gt": [
                            {
                              "$add": [
                                {
                                  "$len": {
                                    "$filter": {
                                      "from": {
                                        "$sortByOrder": {
                                          "$ref": "request.images"
                                        }
                                      },
                                      "as": "media",
                                      "where": {
                                        "$eq": [
                                          {
                                            "$ref": "media.role"
                                          },
                                          "first_frame"
                                        ]
                                      }
                                    }
                                  }
                                },
                                {
                                  "$len": {
                                    "$filter": {
                                      "from": {
                                        "$sortByOrder": {
                                          "$ref": "request.images"
                                        }
                                      },
                                      "as": "media",
                                      "where": {
                                        "$eq": [
                                          {
                                            "$ref": "media.role"
                                          },
                                          "last_frame"
                                        ]
                                      }
                                    }
                                  }
                                }
                              ]
                            },
                            0
                          ]
                        },
                        "then": {
                          "$first": {
                            "$map": {
                              "from": {
                                "$filter": {
                                  "from": {
                                    "$sortByOrder": {
                                      "$ref": "request.images"
                                    }
                                  },
                                  "as": "media",
                                  "where": {
                                    "$eq": [
                                      {
                                        "$ref": "media.role"
                                      },
                                      "last_frame"
                                    ]
                                  }
                                }
                              },
                              "as": "media",
                              "in": {
                                "$ref": "media.value"
                              }
                            }
                          }
                        },
                        "else": null
                      }
                    },
                    "images": {
                      "$if": {
                        "condition": {
                          "$gt": [
                            {
                              "$add": [
                                {
                                  "$len": {
                                    "$filter": {
                                      "from": {
                                        "$sortByOrder": {
                                          "$ref": "request.images"
                                        }
                                      },
                                      "as": "media",
                                      "where": {
                                        "$eq": [
                                          {
                                            "$ref": "media.role"
                                          },
                                          "first_frame"
                                        ]
                                      }
                                    }
                                  }
                                },
                                {
                                  "$len": {
                                    "$filter": {
                                      "from": {
                                        "$sortByOrder": {
                                          "$ref": "request.images"
                                        }
                                      },
                                      "as": "media",
                                      "where": {
                                        "$eq": [
                                          {
                                            "$ref": "media.role"
                                          },
                                          "last_frame"
                                        ]
                                      }
                                    }
                                  }
                                }
                              ]
                            },
                            0
                          ]
                        },
                        "then": null,
                        "else": {
                          "$omitEmpty": {
                            "$map": {
                              "from": {
                                "$sortByOrder": {
                                  "$ref": "request.images"
                                }
                              },
                              "as": "media",
                              "in": {
                                "$ref": "media.value"
                              }
                            }
                          }
                        }
                      }
                    },
                    "videos": {
                      "$omitEmpty": {
                        "$map": {
                          "from": {
                            "$sortByOrder": {
                              "$ref": "request.videos"
                            }
                          },
                          "as": "media",
                          "in": {
                            "$ref": "media.value"
                          }
                        }
                      }
                    },
                    "audios": {
                      "$omitEmpty": {
                        "$map": {
                          "from": {
                            "$sortByOrder": {
                              "$ref": "request.audios"
                            }
                          },
                          "as": "media",
                          "in": {
                            "$ref": "media.value"
                          }
                        }
                      }
                    }
                  }
                }
              ],
              "default": {
                "model": {
                  "$ref": "request.model"
                },
                "prompt": {
                  "$ref": "request.prompt"
                },
                "seconds": {
                  "$if": {
                    "condition": {
                      "$gt": [
                        {
                          "$ref": "request.duration"
                        },
                        0
                      ]
                    },
                    "then": {
                      "$toInt": {
                        "$ref": "request.duration"
                      }
                    },
                    "else": null
                  }
                },
                "aspect_ratio": {
                  "$if": {
                    "condition": {
                      "$gt": [
                        {
                          "$len": {
                            "$split": [
                              {
                                "$lower": {
                                  "$trim": {
                                    "$ref": "request.model"
                                  }
                                }
                              },
                              "hss-"
                            ]
                          }
                        },
                        1
                      ]
                    },
                    "then": null,
                    "else": {
                      "$omitEmpty": {
                        "$trim": {
                          "$ref": "request.aspectRatio"
                        }
                      }
                    }
                  }
                },
                "ratio": {
                  "$if": {
                    "condition": {
                      "$gt": [
                        {
                          "$len": {
                            "$split": [
                              {
                                "$lower": {
                                  "$trim": {
                                    "$ref": "request.model"
                                  }
                                }
                              },
                              "hss-"
                            ]
                          }
                        },
                        1
                      ]
                    },
                    "then": {
                      "$omitEmpty": {
                        "$trim": {
                          "$ref": "request.aspectRatio"
                        }
                      }
                    },
                    "else": null
                  }
                },
                "resolution": {
                  "$if": {
                    "condition": {
                      "$or": [
                        {
                          "$gt": [
                            {
                              "$len": {
                                "$split": [
                                  {
                                    "$lower": {
                                      "$trim": {
                                        "$ref": "request.model"
                                      }
                                    }
                                  },
                                  "seadanco"
                                ]
                              }
                            },
                            1
                          ]
                        },
                        {
                          "$gt": [
                            {
                              "$len": {
                                "$split": [
                                  {
                                    "$lower": {
                                      "$trim": {
                                        "$ref": "request.model"
                                      }
                                    }
                                  },
                                  "dreamina-seedance"
                                ]
                              }
                            },
                            1
                          ]
                        }
                      ]
                    },
                    "then": null,
                    "else": {
                      "$omitEmpty": {
                        "$lower": {
                          "$trim": {
                            "$ref": "request.resolution"
                          }
                        }
                      }
                    }
                  }
                },
                "images": {
                  "$omitEmpty": {
                    "$map": {
                      "from": {
                        "$sortByOrder": {
                          "$ref": "request.images"
                        }
                      },
                      "as": "media",
                      "in": {
                        "$ref": "media.value"
                      }
                    }
                  }
                },
                "videos": {
                  "$omitEmpty": {
                    "$map": {
                      "from": {
                        "$sortByOrder": {
                          "$ref": "request.videos"
                        }
                      },
                      "as": "media",
                      "in": {
                        "$ref": "media.value"
                      }
                    }
                  }
                },
                "audios": {
                  "$omitEmpty": {
                    "$map": {
                      "from": {
                        "$sortByOrder": {
                          "$ref": "request.audios"
                        }
                      },
                      "as": "media",
                      "in": {
                        "$ref": "media.value"
                      }
                    }
                  }
                }
              }
            }
          }
        },
        "poll": {
          "method": "GET",
          "path": "/v1/videos/{{taskId}}"
        },
        "result": {
          "method": "GET",
          "path": "/v1/videos/{{taskId}}/content",
          "headers": {
            "Accept": "video/mp4"
          }
        },
        "response": {
          "taskId": {
            "$coalesce": [
              {
                "$ref": "response.id"
              },
              {
                "$ref": "response.task_id"
              },
              {
                "$ref": "response.data.id"
              },
              {
                "$ref": "response.data.task_id"
              },
              {
                "$ref": "taskId"
              }
            ]
          },
          "status": {
            "$coalesce": [
              {
                "$ref": "response.status"
              },
              {
                "$ref": "response.data.status"
              },
              "pending"
            ]
          },
          "message": {
            "$coalesce": [
              {
                "$ref": "response.error.message"
              },
              {
                "$ref": "response.error"
              },
              {
                "$ref": "response.fail_reason"
              },
              {
                "$ref": "response.message"
              }
            ]
          },
          "stage": {
            "$switch": {
              "cases": [
                {
                  "when": {
                    "$in": [
                      {
                        "$lower": {
                          "$coalesce": [
                            {
                              "$ref": "response.status"
                            },
                            {
                              "$ref": "response.data.status"
                            }
                          ]
                        }
                      },
                      [
                        "in_progress",
                        "running",
                        "processing"
                      ]
                    ]
                  },
                  "then": "上游生成中"
                },
                {
                  "when": {
                    "$in": [
                      {
                        "$lower": {
                          "$coalesce": [
                            {
                              "$ref": "response.status"
                            },
                            {
                              "$ref": "response.data.status"
                            }
                          ]
                        }
                      },
                      [
                        "not_start",
                        "queued",
                        "pending",
                        "submitted"
                      ]
                    ]
                  },
                  "then": "上游排队中"
                }
              ],
              "default": null
            }
          },
          "videos": {
            "$if": {
              "condition": {
                "$in": [
                  {
                    "$lower": {
                      "$coalesce": [
                        {
                          "$ref": "response.status"
                        },
                        {
                          "$ref": "response.data.status"
                        }
                      ]
                    }
                  },
                  [
                    "completed",
                    "succeeded",
                    "success"
                  ]
                ]
              },
              "then": {
                "$coalesce": [
                  {
                    "$ref": "response.video_url"
                  },
                  {
                    "$ref": "response.url"
                  },
                  {
                    "$ref": "response.result_url"
                  },
                  {
                    "$ref": "response.data.video_url"
                  },
                  {
                    "$ref": "response.data.url"
                  }
                ]
              },
              "else": null
            }
          },
          "errorPaths": [
            "error.code"
          ],
          "resultEphemeral": true
        }
      }
    ]
  },
  "documentation": "<当前插件的完整 documentation，由 README.md 与 docs/interface.md 拼接而成；为避免 JSON 递归，此处不重复展开正文。>"
}
```
<!-- YINGCE_MANIFEST_CONTRACT_END -->
