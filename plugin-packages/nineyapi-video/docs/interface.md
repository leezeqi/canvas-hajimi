# 9Y API 视频接口字段

## 协议身份

- 插件 ID：`nineyapi-video`。
- Provider ID：`nineyapi-video`（与插件 ID 相同）。
- 能力：`video`。
- 默认 Base URL：`https://9yapi.shop`，末尾不要带 `/v1`（接口路径已包含 `/v1`）。
- 鉴权：`Authorization: Bearer <apiKey>`，使用 9Y 账号 API Key。
- 创建：`POST /v1/videos`（`application/json`）。
- 查询：`GET /v1/videos/{task_id}`。
- 下载：优先使用查询响应中的 `video_url` / `result_url`（无需鉴权的临时直链）；响应没有直链时，`GET /v1/videos/{task_id}/content` 带同一个 Bearer Key 下载。
- 取消：上游未提供，插件不声明取消操作。

## 配置字段

| 字段 | 类型 | 必填 | 含义 |
| --- | --- | --- | --- |
| `apiKey` | secret | 是 | 9Y API Key |

## 适用模型

| 模型 | 时长 | 分辨率 | 画幅 | 素材与限制 |
| --- | --- | --- | --- | --- |
| `【满血】SD-2.0 720p` / `【满血】SD-2.0 480p` | 4–15 | 720p / 480p | 6 种标准画幅 | 图 ≤ 9、音频 ≤ 3；不支持参考视频和首尾帧 |
| `【官转】sd2.0-满血720` / `【官转】sd2.0-满血480` | 4–15 | 720p / 480p | 6 种标准画幅 | 至少 1 张图；图 ≤ 9、音频 ≤ 3；不支持参考视频和首尾帧 |
| `【满血】SD-2.5-720p` / `【满血】SD-2.5-480p` | 4–30，默认 5 | 720p / 480p | 6 种标准画幅；首尾帧须 `adaptive` | 图 ≤ 30、视频 ≤ 10、音频 ≤ 10；支持首尾帧但不能与普通参考混用；提示词 ≤ 5000 字 |
| `【官转】sd-2.5-720p` / `【官转】sd-2.5-480p` | 4–30 | 720p / 480p | 6 种标准画幅 | 图 ≤ 30、视频 ≤ 10、音频 ≤ 10；`generate_audio` 不支持 false |
| `【官转】sd2.0-fast` | 4–15，必填 | 480p / 720p | 6 种标准画幅 + `adaptive` | 图 ≤ 9、视频 ≤ 3、音频 ≤ 3；音频须搭配图或视频；支持首尾帧 |
| `sd2.0-fast` | 固定 15 | 720p | 6 种标准画幅 | 图 ≤ 9 |
| `【官转】sd2.0-mini`、`DB-SD-2.0-Fast` | 以 9Y 文档为准 | 以 9Y 文档为准 | 以 9Y 文档为准 | 以 9Y 文档为准 |
| `DB-SD-2.5` | 固定 30 | 固定 720p | 6 种标准画幅 | 图 ≤ 9；不支持参考视频、音频和首尾帧；提示词 ≤ 3000 字 |
| `【官转】Minimax-H3` | 4–15，默认 5 | 768p / 2k | 6 种标准画幅 | 至少 1 张图；图 ≤ 9、视频 ≤ 3、音频 ≤ 3，合计 ≤ 12；不支持首尾帧 |
| `wan3.0-特惠版480p` / `720p` / `1080p` | 5–30，默认 15 | 与模型名一致 | 16:9、9:16、1:1 | 图 ≤ 10、视频 ≤ 5、音频 ≤ 5 |

各模型的时长、分辨率、素材数量差异由管理员在渠道模型能力里配置；插件只做所有模型共有的宽校验，超出单个模型限制时由上游返回原因。

## 统一字段映射

| 统一字段 | 类型 | 必填 | 上游映射 | 说明 |
| --- | --- | --- | --- | --- |
| `model` | string | 是 | `model` | 9Y 模型名，原样发送（含中文与括号）。 |
| `prompt` | string | 是 | `prompt` | 视频提示词，不能为空。 |
| `images` | media[] | 否 | `reference_images[{url, role}]` | 按 `order` 排序；`role` 为 `first_frame`、`last_frame` 或缺省 `reference_image`。 |
| `videos` | media[] | 否 | `reference_videos[{url}]` | 按 `order` 排序。 |
| `audios` | media[] | 否 | `reference_audios[{url}]` | 按 `order` 排序。 |
| `duration` | integer | 否 | `seconds` | 整数秒；为 0 时省略，由上游按模型默认值生成。 |
| `aspectRatio` | string | 否 | `aspect_ratio` | 为空时省略，上游默认 `16:9`。 |
| `resolution` | string | 否 | `resolution` | 统一转小写（如 `768P` → `768p`、`2K` → `2k`）；为空时省略。 |
| `generateAudio` | boolean | 否 | `generate_audio` | 为 true 时发送 `true`；为 false 时不发送，上游按模型默认（有声）生成。 |

`generateAudio=false` 不下发，是因为 `【官转】sd-2.5` 系列会拒收 `generate_audio:false`。需要静音成片的模型请在 9Y 侧确认后再扩展。

## 请求示例

```json
{
  "model": "【满血】SD-2.5-720p",
  "prompt": "参考@图1中的主体，保持外观一致，主体沿湖边缓慢行走。",
  "seconds": 5,
  "resolution": "720p",
  "aspect_ratio": "16:9",
  "generate_audio": true,
  "reference_images": [{ "url": "https://example.com/reference-1.png", "role": "reference_image" }],
  "reference_videos": [{ "url": "https://example.com/motion.mp4" }],
  "reference_audios": [{ "url": "https://example.com/voice.mp3" }]
}
```

空的素材数组不会发送。

## 提交前校验

| 规则 | 提示 |
| --- | --- |
| 时长为 0（使用上游默认）或 4–30 | 9Y 视频时长必须是 4–30 秒的整数 |
| 画幅为空或在 16:9、9:16、1:1、4:3、3:4、21:9、adaptive 内 | 9Y 视频画幅只支持 … |
| 分辨率为空或为 480p / 720p / 768p / 1080p / 2k（大小写不敏感） | 9Y 视频分辨率只支持 … |
| 有尾帧时必须同时有首帧 | 9Y 视频尾帧必须搭配首帧 |

素材 URL 必须能被上游从公网访问：本机、内网或需要登录的地址会被上游拒收，部署需要对外可访问的 HTTPS 资源地址。

## 查询与响应

| 上游状态 | 宿主状态 | 进度文案 |
| --- | --- | --- |
| `queued` | 等待 | 排队中 |
| `in_progress` | 处理中 | 生成中 `progress`% |
| `completed` | 成功 | — |
| `failed` | 失败 | — |

响应映射：

- 任务 ID：依次取 `id`、`task_id`、`data.id`，都缺失时沿用已知任务 ID。
- 失败原因：依次取 `error.message`、`error`（字符串）、`message`、`fail_reason`。
- 视频地址：依次取 `video_url`、`result_url`、`data.video_url`、`data.result_url`。该直链有 `expires_at` 失效时间，插件声明 `resultEphemeral`，宿主拿到后立即下载并持久化。
- 没有直链时，宿主调用 `GET /v1/videos/{task_id}/content` 带 Key 下载；返回 `video_archive_pending` 时按 `Retry-After` 重试。

## 已知边界

- 带参考素材的 SD 满血线路，9Y 建议创建请求至少等待 12 分钟；宿主单次上游请求超时为 5 分钟，超时会让本次生成失败；上游可能已经建单，重试前请先在 9Y 后台核对，避免重复扣费。
- 插件不调用 9Y 图床 `/v1/reference-assets`；素材以宿主签名 URL 提交。若 9Y 只接受自家图床地址，需要另行扩展。

<!-- YINGCE_MANIFEST_CONTRACT_START -->
## Manifest 完整接口定义

以下 JSON 与插件包内实际 `manifest.json` 逐字段一致，覆盖插件身份、权限、配置、鉴权、参数、校验、创建、Agent、查询、取消、结果下载、响应和 Agent 响应映射。`documentation` 字段的值就是当前完整文档；为避免文档在自身内部无限递归，JSON 中仅用等义占位文本表示正文。

```json
{
  "apiVersion": "yingce.plugin/v2",
  "id": "nineyapi-video",
  "name": "9Y API 视频",
  "version": "1.0.0",
  "author": "影策",
  "description": "9Y API 视频通道（SD-2.0/2.5、Minimax-H3、wan3.0 特惠版、sd2.0-fast）：JSON 创建 /v1/videos，参考素材为带 role 的对象数组，成片优先取查询返回的直链，缺失时走 /content 鉴权下载。",
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
        "id": "nineyapi-video",
        "label": "9Y API Videos",
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
        "baseUrl": "https://9yapi.shop",
        "requiresPublicMediaUrls": true,
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
            "description": "9Y 模型名，原样发送，例如 【满血】SD-2.0 720p、【官转】Minimax-H3、wan3.0-特惠版720p、【官转】sd2.0-fast。"
          },
          {
            "name": "prompt",
            "type": "string",
            "required": true,
            "mapping": "prompt",
            "description": "视频提示词，不能为空；可用 @图1 等写法引用参考素材。"
          },
          {
            "name": "images",
            "type": "media[]",
            "mapping": "reference_images[{url, role}]",
            "description": "参考图按 order 排序，role 取 first_frame、last_frame 或 reference_image（缺省）；公网 HTTPS URL。"
          },
          {
            "name": "videos",
            "type": "media[]",
            "mapping": "reference_videos[{url}]",
            "description": "参考视频按 order 排序，公网 HTTPS URL；是否支持及数量上限由所选模型决定。"
          },
          {
            "name": "audios",
            "type": "media[]",
            "mapping": "reference_audios[{url}]",
            "description": "参考音频按 order 排序，公网 HTTPS URL；是否支持及数量上限由所选模型决定。"
          },
          {
            "name": "duration",
            "type": "integer",
            "mapping": "seconds",
            "description": "生成时长，整数秒，4–30；未指定时省略，由上游按模型默认值生成。"
          },
          {
            "name": "aspectRatio",
            "type": "string",
            "values": [
              "16:9",
              "9:16",
              "1:1",
              "4:3",
              "3:4",
              "21:9",
              "adaptive"
            ],
            "mapping": "aspect_ratio",
            "description": "画幅比例；未指定时省略，上游默认 16:9。adaptive 仅部分模型支持。"
          },
          {
            "name": "resolution",
            "type": "string",
            "values": [
              "480p",
              "720p",
              "768p",
              "1080p",
              "2k"
            ],
            "mapping": "resolution",
            "description": "分辨率，统一转小写；未指定时省略，由上游按模型默认值生成。"
          },
          {
            "name": "generateAudio",
            "type": "boolean",
            "mapping": "generate_audio",
            "description": "为 true 时发送 generate_audio=true；为 false 时省略，由上游按模型默认（通常有声）生成。"
          }
        ],
        "validations": [
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
                        4
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
            "message": "9Y 视频时长必须是 4–30 秒的整数"
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
                      "adaptive"
                    ]
                  ]
                }
              ]
            },
            "message": "9Y 视频画幅只支持 16:9、9:16、1:1、4:3、3:4、21:9、adaptive"
          },
          {
            "assert": {
              "$or": [
                {
                  "$eq": [
                    {
                      "$trim": {
                        "$ref": "request.resolution"
                      }
                    },
                    ""
                  ]
                },
                {
                  "$in": [
                    {
                      "$lower": {
                        "$trim": {
                          "$ref": "request.resolution"
                        }
                      }
                    },
                    [
                      "480p",
                      "720p",
                      "768p",
                      "1080p",
                      "2k"
                    ]
                  ]
                }
              ]
            },
            "message": "9Y 视频分辨率只支持 480p、720p、768p、1080p、2k"
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
                            "$ref": "request.images"
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
                            "$ref": "request.images"
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
            "message": "9Y 视频尾帧必须搭配首帧"
          }
        ],
        "create": {
          "method": "POST",
          "path": "/v1/videos",
          "contentType": "application/json",
          "body": {
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
                  "$ref": "request.duration"
                },
                "else": null
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
            "aspect_ratio": {
              "$omitEmpty": {
                "$trim": {
                  "$ref": "request.aspectRatio"
                }
              }
            },
            "generate_audio": {
              "$if": {
                "condition": {
                  "$ref": "request.generateAudio"
                },
                "then": true,
                "else": null
              }
            },
            "reference_images": {
              "$omitEmpty": {
                "$map": {
                  "from": {
                    "$sortByOrder": {
                      "$ref": "request.images"
                    }
                  },
                  "as": "media",
                  "in": {
                    "url": {
                      "$ref": "media.value"
                    },
                    "role": {
                      "$coalesce": [
                        {
                          "$ref": "media.role"
                        },
                        "reference_image"
                      ]
                    }
                  }
                }
              }
            },
            "reference_videos": {
              "$omitEmpty": {
                "$map": {
                  "from": {
                    "$sortByOrder": {
                      "$ref": "request.videos"
                    }
                  },
                  "as": "media",
                  "in": {
                    "url": {
                      "$ref": "media.value"
                    }
                  }
                }
              }
            },
            "reference_audios": {
              "$omitEmpty": {
                "$map": {
                  "from": {
                    "$sortByOrder": {
                      "$ref": "request.audios"
                    }
                  },
                  "as": "media",
                  "in": {
                    "url": {
                      "$ref": "media.value"
                    }
                  }
                }
              }
            }
          }
        },
        "poll": {
          "method": "GET",
          "path": "/v1/videos/{{taskId}}",
          "contentType": "application/json"
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
                "$ref": "response.message"
              },
              {
                "$ref": "response.fail_reason"
              }
            ]
          },
          "stage": {
            "$switch": {
              "cases": [
                {
                  "when": {
                    "$eq": [
                      {
                        "$ref": "response.status"
                      },
                      "queued"
                    ]
                  },
                  "then": "排队中"
                },
                {
                  "when": {
                    "$eq": [
                      {
                        "$ref": "response.status"
                      },
                      "in_progress"
                    ]
                  },
                  "then": {
                    "$concat": [
                      "生成中 ",
                      {
                        "$coalesce": [
                          {
                            "$ref": "response.progress"
                          },
                          0
                        ]
                      },
                      "%"
                    ]
                  }
                }
              ],
              "default": null
            }
          },
          "videos": {
            "$coalesce": [
              {
                "$ref": "response.video_url"
              },
              {
                "$ref": "response.result_url"
              },
              {
                "$ref": "response.data.video_url"
              },
              {
                "$ref": "response.data.result_url"
              }
            ]
          },
          "resultEphemeral": true
        }
      }
    ]
  },
  "documentation": "<当前插件的完整 documentation，由 README.md 与 docs/interface.md 拼接而成；为避免 JSON 递归，此处不重复展开正文。>"
}
```
<!-- YINGCE_MANIFEST_CONTRACT_END -->
