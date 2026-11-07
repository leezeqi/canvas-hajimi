# nskeven 视频接口字段

## 协议身份

- 插件 ID：`nskeven-video`。
- Provider ID：`nskeven-video`（与插件 ID 相同）。
- 可见性：仅管理员可见。普通用户的插件中心、插件状态和协议目录不返回该插件，自定义渠道也不能使用；管理员把它配置到系统渠道模型后，普通用户可以通过该系统渠道正常生成，插件被平台停用时一并失败。
- 能力：`video`。
- 默认 Base URL：`https://newapi.nskeven.uk`，末尾不要带 `/v1`（接口路径已包含 `/v1`）。
- 鉴权：`Authorization: Bearer <apiKey>`。使用平台分配的令牌，不要自行在控制台新建；令牌带分组属性，视频模型和图片模型通常在不同分组，用错分组会返回 503 `model_not_found`。
- 创建：`POST /v1/video/generations`（`application/json`）。
- 查询：`GET /v1/video/generations/{task_id}`，任务 ID 取创建响应的 `task_id`（与 `id` 相同）。
- 下载：任务 `SUCCESS` 后固定走 `GET /v1/videos/{task_id}/content`，带同一个 Bearer Key（上游推荐做法；`data.result_url` 恒为同一地址，插件不单独解析）。成片约保留 10 小时，宿主完成后立即下载持久化。
- 取消：上游未提供，插件不声明取消操作。
- 轮询：声明 `detachedPolling`，提交后释放 worker，由任务调度约 15 秒回查一次原任务（上游建议 10～20 秒），不重复提交。
- 不使用 `/v1/videos` OpenAI 兼容路径：上游说明该路径的查询结构随模型变化，不稳定。

## 配置字段

| 字段 | 类型 | 必填 | 含义 |
| --- | --- | --- | --- |
| `apiKey` | secret | 是 | nskeven 平台令牌，以 `Authorization: Bearer` 发送 |

## 模型族与请求体形状

模型名去首尾空格并转小写后判定，模型名本身原样发送：

| 模型族 | 判定规则 | 代表模型 | 请求体形状 |
| --- | --- | --- | --- |
| 豆包 Seedance | 包含 `doubao-seedance` | `doubao-seedance-2-0-260128`、`doubao-seedance-2-0-fast-260128`、`doubao-seedance-2-0-mini-260615`、`doubao-seedance-2-5-260628` | 分辨率、画幅、素材、声音、水印全部写在 `metadata`，顶层不发这些字段 |
| 快线 / 长时长 | 包含 `sd-2-`、`sd-2.5-` 或 `wan-3.0-` | `sd-2-E4`、`sd-2-E5`、`sd-2-E6`、`sd-2-E8`、`sd-2.5-E1`、`wan-3.0-1080`、`even-sd-2.5-30s` | 参考图在顶层 `images`；参考视频、音频写在 `metadata.videos` / `metadata.audios` |
| 扁平字段（其余） | 以上都不匹配 | `even-sd2.0-A`、`even-sd2.5-A`、`even-sd2.5-B`、`video-ultra-720p`、`video-ultra-720p-fast`、`video-ultra-720p-mini`、`video-ultra2.5-720p`、`even--seedance-2.0-480p/720p/1080p/4k` | 全部顶层扁平字段 |

`even-sd-2.5-30s` 归入快线族依据上游模型清单 10.5 节“快线 / 长时长系列（使用 4.3 格式）”。

## 统一字段映射

| 统一字段 | 类型 | 必填 | 扁平 / 快线 | 豆包 | 说明 |
| --- | --- | --- | --- | --- | --- |
| `model` | string | 是 | `model` | `model` | 原样发送。 |
| `prompt` | string | 是 | `prompt` | `prompt` | 不能为空。`even-sd2.0-A` / `even-sd2.5-A` 会把画布的 `@图片N` 改写为上游要求的 `@图N`。 |
| `duration` | integer | 否 | `seconds` | `seconds` | 以字符串发送（如 `"8"`），上游拒绝裸数字；为 0 时省略。 |
| `resolution` | string | 否 / 豆包必填 | `resolution` | `metadata.resolution` | 统一转小写，空值省略。 |
| `aspectRatio` | string | 否 | `aspect_ratio` | `metadata.ratio` | 空值省略，上游默认 16:9。 |
| `images`（role 非首尾帧） | media[] | 否 | `images`（URL 字符串数组） | `metadata.content[]`，`role: reference_image` | 按 `order` 排序。 |
| `images`（role=`first_frame`） | media | 否 | `first_frame_url` | `metadata.content[]`，`role: first_frame` | 最多 1 张。 |
| `images`（role=`last_frame`） | media | 否 | `last_frame_url` | `metadata.content[]`，`role: last_frame` | 最多 1 张。 |
| `videos` | media[] | 否 | 扁平：`videos`；快线：`metadata.videos` | `metadata.content[]`，`role: reference_video` | URL 字符串数组，按 `order` 排序。 |
| `audios` | media[] | 否 | 扁平：`audios`；快线：`metadata.audios` | `metadata.content[]`，`role: reference_audio` | URL 字符串数组，按 `order` 排序。 |
| `generateAudio` | boolean | 否 | 仅为 true 时发送 `generate_audio: true` | `metadata.generate_audio` 原样发送 | 上游说明“部分模型支持”，非豆包模型不发送 false。 |
| `watermark` | boolean | 否 | 不发送 | 为 true 时发送 `metadata.watermark: true` | 仅豆包有该字段。 |

豆包 `metadata.content` 素材项写法：`{"type":"image_url","image_url":{"url":"..."},"role":"reference_image"}`，视频 / 音频分别为 `video_url` / `audio_url`。不发送 `metadata.seed`。

## 请求示例

扁平字段（A 系列带参考图和参考视频）：

```json
{
  "model": "even-sd2.5-A",
  "prompt": "按 @图1 的构图与 @视频1 的运镜，生成雨夜霓虹街道镜头",
  "seconds": "8",
  "resolution": "720p",
  "aspect_ratio": "9:16",
  "images": ["https://cdn.example.com/ref-1.jpg"],
  "videos": ["https://cdn.example.com/ref-cam.mp4"]
}
```

首尾帧（扁平字段）：

```json
{
  "model": "video-ultra-720p",
  "prompt": "从清晨过渡到黄昏",
  "seconds": "5",
  "resolution": "720p",
  "first_frame_url": "https://cdn.example.com/first.png",
  "last_frame_url": "https://cdn.example.com/last.png"
}
```

豆包 Seedance：

```json
{
  "model": "doubao-seedance-2-0-fast-260128",
  "prompt": "参考图片中的人物，按参考视频的运镜生成街头镜头",
  "seconds": "8",
  "metadata": {
    "resolution": "720p",
    "ratio": "9:16",
    "content": [
      { "type": "image_url", "image_url": { "url": "https://cdn.example.com/ref-1.jpg" }, "role": "reference_image" },
      { "type": "video_url", "video_url": { "url": "https://cdn.example.com/ref.mp4" }, "role": "reference_video" }
    ],
    "generate_audio": true
  }
}
```

快线系列：

```json
{
  "model": "sd-2-E6",
  "prompt": "根据参考图与参考视频节奏生成同款镜头",
  "seconds": "8",
  "resolution": "720p",
  "aspect_ratio": "16:9",
  "images": ["https://cdn.example.com/ref-1.jpg"],
  "metadata": {
    "videos": ["https://cdn.example.com/ref.mp4"],
    "audios": ["https://cdn.example.com/ref.mp3"]
  }
}
```

## 提交前校验

- 提示词去空格后不能为空。
- 时长为 0（省略）或 4–30 秒；各模型的精确上限（如豆包 2.0 系列 15 秒）由上游校验。
- 画幅只允许 16:9、9:16、1:1、4:3、3:4、21:9；21:9 只有豆包支持；快线系列只支持 1:1、16:9、9:16。
- 分辨率只允许 480p、720p、1080p、4k（不区分大小写）；豆包模型必须选择分辨率。
- 首帧、尾帧各最多 1 张。
- 豆包使用首尾帧时图片总数不超过 2 张，且不能同时带参考视频。
- 快线系列参考图最多 9 张、参考视频最多 3 个、参考音频最多 3 个。
- `even-sd2.0-A` / `even-sd2.5-A` 带参考图、参考视频、参考音频时，提示词必须分别包含 `@图`、`@视频`、`@音频` 引用。

## 响应映射

创建响应：

```json
{ "id": "task_385412", "task_id": "task_385412", "object": "video", "status": "queued", "created_at": 1790224802 }
```

查询响应（统一结构，外层 `code` 固定为 `success`）：

```json
{
  "code": "success",
  "message": "",
  "data": {
    "id": 883,
    "task_id": "task_385412",
    "status": "FAILURE",
    "progress": "100%",
    "fail_reason": "生成被拒绝，内容安全审核未通过，修改提示词或素材后重试",
    "result_url": "",
    "created_at": 1790224802,
    "finish_time": 1790224804
  }
}
```

| 映射位置 | 来源 | 说明 |
| --- | --- | --- |
| 任务 ID | `task_id` → `data.task_id` → `id` | 不使用 `data.id`（平台自增数字 ID）。 |
| 状态 | `data.status` → `status` | 只认外层 `data.status`，不读 `data.data.status` 或 `progress`。`QUEUED`、`SUBMITTED` 为排队；`IN_PROGRESS` 为生成中；`SUCCESS` 成功；`FAILURE` 失败；`NOT_START`、`UNKNOWN` 按排队继续回查。 |
| 阶段文案 | 同上 | `NOT_START` / `SUBMITTED` / `QUEUED` 显示“排队中”，`IN_PROGRESS` 显示“生成中”，`UNKNOWN` 显示“上游状态未知，继续查询”。不展示 `progress`（上游说明排队中也可能是 100%）。 |
| 失败原因 | 仅 `FAILURE` 时取 `data.fail_reason` → `data.data.fail_reason` → `data.data.error.message` → `data.data.message` → `error.message` | 原样展示，不做关键词匹配；成功时即使 `fail_reason` 非空也不读取。 |
| 接口错误 | `error.code`、`error.message` | 上游错误结构 `{"error":{"message","type":"new_api_error","code"}}`，`code` 可能为空，`message` 非空同样视为失败。 |
| 成片 | `GET /v1/videos/{task_id}/content` | 查询响应不解析直链，成功后由宿主带 Key 下载并持久化。 |

<!-- YINGCE_MANIFEST_CONTRACT_START -->
## Manifest 完整接口定义

以下 JSON 与插件包内实际 `manifest.json` 逐字段一致，覆盖插件身份、权限、配置、鉴权、参数、校验、创建、Agent、查询、取消、结果下载、响应和 Agent 响应映射。`documentation` 字段的值就是当前完整文档；为避免文档在自身内部无限递归，JSON 中仅用等义占位文本表示正文。

```json
{
  "apiVersion": "yingce.plugin/v2",
  "id": "nskeven-video",
  "name": "nskeven 视频",
  "version": "1.0.0",
  "author": "影策",
  "description": "nskeven NewAPI 视频通道：JSON 创建 /v1/video/generations，按模型族切换扁平字段、豆包 metadata.content 与快线 metadata.videos/audios 三种格式，seconds 以字符串发送；查询统一结构 data.status，成片走 /v1/videos/{id}/content 带 Key 下载。",
  "documentation": "<当前插件的完整 documentation，由 README.md 与 docs/interface.md 拼接而成；为避免 JSON 递归，此处不重复展开正文。>",
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
        "id": "nskeven-video",
        "label": "nskeven 视频",
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
        "baseUrl": "https://newapi.nskeven.uk",
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
            "description": "nskeven 模型名，原样发送，例如 even-sd2.5-A、video-ultra-720p、doubao-seedance-2-0-fast-260128、sd-2-E6；以控制台模型广场为准。"
          },
          {
            "name": "prompt",
            "type": "string",
            "required": true,
            "mapping": "prompt",
            "description": "视频提示词，不能为空。even-sd2.x-A 带参考素材时须用 @图1、@视频1、@音频1 引用，画布的 @图片N 会改写为 @图N。"
          },
          {
            "name": "images",
            "type": "media[]",
            "required": false,
            "mapping": "images / first_frame_url / last_frame_url / metadata.content",
            "description": "按 order 排序。role 为 first_frame / last_frame 的图片发送到 first_frame_url / last_frame_url，其余作为 images 参考图；豆包写入 metadata.content。须为公网 URL。"
          },
          {
            "name": "videos",
            "type": "media[]",
            "required": false,
            "mapping": "videos / metadata.videos / metadata.content",
            "description": "参考视频，按 order 排序；快线系列写入 metadata.videos，豆包写入 metadata.content。须为公网 URL。"
          },
          {
            "name": "audios",
            "type": "media[]",
            "required": false,
            "mapping": "audios / metadata.audios / metadata.content",
            "description": "参考音频，按 order 排序；快线系列写入 metadata.audios，豆包写入 metadata.content。须为公网 URL。"
          },
          {
            "name": "duration",
            "type": "integer",
            "required": false,
            "mapping": "seconds",
            "description": "时长 4–30 秒，以字符串 seconds 发送（上游拒绝裸数字）；未指定时省略，由上游按模型默认值生成。"
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
              "21:9"
            ],
            "mapping": "aspect_ratio / metadata.ratio",
            "description": "画幅；21:9 仅豆包支持，快线系列只支持 1:1、16:9、9:16。未指定时省略，上游默认 16:9。"
          },
          {
            "name": "resolution",
            "type": "string",
            "required": false,
            "values": [
              "480p",
              "720p",
              "1080p",
              "4k"
            ],
            "mapping": "resolution / metadata.resolution",
            "description": "分辨率，统一转小写。豆包必填；A 系列省略时上游默认 480p，建议显式选择。"
          },
          {
            "name": "generateAudio",
            "type": "boolean",
            "required": false,
            "mapping": "generate_audio / metadata.generate_audio",
            "description": "豆包按开关原样写入 metadata.generate_audio；其他模型仅为 true 时发送 generate_audio。"
          },
          {
            "name": "watermark",
            "type": "boolean",
            "required": false,
            "mapping": "metadata.watermark",
            "description": "仅豆包 Seedance 生效，为 true 时发送 metadata.watermark。"
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
            "message": "nskeven 视频提示词不能为空"
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
            "message": "nskeven 视频时长必须是 4–30 秒的整数"
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
                      "21:9"
                    ]
                  ]
                }
              ]
            },
            "message": "nskeven 视频画幅只支持 16:9、9:16、1:1、4:3、3:4、21:9"
          },
          {
            "assert": {
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
                          "doubao-seedance"
                        ]
                      }
                    },
                    1
                  ]
                },
                {
                  "$ne": [
                    {
                      "$trim": {
                        "$ref": "request.aspectRatio"
                      }
                    },
                    "21:9"
                  ]
                }
              ]
            },
            "message": "nskeven 视频只有豆包 Seedance（doubao-seedance-*）支持 21:9 画幅"
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
                                "sd-2-"
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
                                "sd-2.5-"
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
                                "wan-3.0-"
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
                      "1:1",
                      "16:9",
                      "9:16"
                    ]
                  ]
                }
              ]
            },
            "message": "nskeven 快线系列（sd-2-*、sd-2.5-*、wan-3.0-*、even-sd-2.5-30s）画幅只支持 1:1、16:9、9:16"
          },
          {
            "assert": {
              "$or": [
                {
                  "$eq": [
                    {
                      "$lower": {
                        "$trim": {
                          "$ref": "request.resolution"
                        }
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
                      "1080p",
                      "4k"
                    ]
                  ]
                }
              ]
            },
            "message": "nskeven 视频分辨率只支持 480p、720p、1080p、4k"
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
                            "doubao-seedance"
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
                        "$lower": {
                          "$trim": {
                            "$ref": "request.resolution"
                          }
                        }
                      }
                    },
                    0
                  ]
                }
              ]
            },
            "message": "nskeven 豆包 Seedance 模型必须选择分辨率（metadata.resolution 为必填）"
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
                                "$lower": {
                                  "$trim": {
                                    "$ref": "media.role"
                                  }
                                }
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
                                "$lower": {
                                  "$trim": {
                                    "$ref": "media.role"
                                  }
                                }
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
            "message": "nskeven 视频首帧、尾帧各最多 1 张"
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
                            "doubao-seedance"
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
                                    "$lower": {
                                      "$trim": {
                                        "$ref": "media.role"
                                      }
                                    }
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
                                    "$lower": {
                                      "$trim": {
                                        "$ref": "media.role"
                                      }
                                    }
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
                {
                  "$and": [
                    {
                      "$lte": [
                        {
                          "$len": {
                            "$ref": "request.images"
                          }
                        },
                        2
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
            },
            "message": "nskeven 豆包 Seedance 使用首尾帧时图片总数不超过 2 张，且不能同时带参考视频"
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
                                "sd-2-"
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
                                "sd-2.5-"
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
                                "wan-3.0-"
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
                    }
                  ]
                }
              ]
            },
            "message": "nskeven 快线系列参考图最多 9 张、参考视频最多 3 个、参考音频最多 3 个"
          },
          {
            "assert": {
              "$or": [
                {
                  "$not": {
                    "$in": [
                      {
                        "$lower": {
                          "$trim": {
                            "$ref": "request.model"
                          }
                        }
                      },
                      [
                        "even-sd2.0-a",
                        "even-sd2.5-a"
                      ]
                    ]
                  }
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
                            "$not": {
                              "$in": [
                                {
                                  "$lower": {
                                    "$trim": {
                                      "$ref": "media.role"
                                    }
                                  }
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
                  "$gt": [
                    {
                      "$len": {
                        "$split": [
                          {
                            "$ref": "request.prompt"
                          },
                          "@图"
                        ]
                      }
                    },
                    1
                  ]
                }
              ]
            },
            "message": "nskeven even-sd2.x-A 带参考图时，提示词须用 @图1、@图2 引用参考图（按顺序从 1 编号）"
          },
          {
            "assert": {
              "$or": [
                {
                  "$not": {
                    "$in": [
                      {
                        "$lower": {
                          "$trim": {
                            "$ref": "request.model"
                          }
                        }
                      },
                      [
                        "even-sd2.0-a",
                        "even-sd2.5-a"
                      ]
                    ]
                  }
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
                  "$gt": [
                    {
                      "$len": {
                        "$split": [
                          {
                            "$ref": "request.prompt"
                          },
                          "@视频"
                        ]
                      }
                    },
                    1
                  ]
                }
              ]
            },
            "message": "nskeven even-sd2.x-A 带参考视频时，提示词须用 @视频1 等引用参考视频"
          },
          {
            "assert": {
              "$or": [
                {
                  "$not": {
                    "$in": [
                      {
                        "$lower": {
                          "$trim": {
                            "$ref": "request.model"
                          }
                        }
                      },
                      [
                        "even-sd2.0-a",
                        "even-sd2.5-a"
                      ]
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
                },
                {
                  "$gt": [
                    {
                      "$len": {
                        "$split": [
                          {
                            "$ref": "request.prompt"
                          },
                          "@音频"
                        ]
                      }
                    },
                    1
                  ]
                }
              ]
            },
            "message": "nskeven even-sd2.x-A 带参考音频时，提示词须用 @音频1 等引用参考音频"
          }
        ],
        "create": {
          "method": "POST",
          "path": "/v1/video/generations",
          "contentType": "application/json",
          "body": {
            "model": {
              "$ref": "request.model"
            },
            "prompt": {
              "$if": {
                "condition": {
                  "$in": [
                    {
                      "$lower": {
                        "$trim": {
                          "$ref": "request.model"
                        }
                      }
                    },
                    [
                      "even-sd2.0-a",
                      "even-sd2.5-a"
                    ]
                  ]
                },
                "then": {
                  "$join": [
                    {
                      "$split": [
                        {
                          "$ref": "request.prompt"
                        },
                        "@图片"
                      ]
                    },
                    "@图"
                  ]
                },
                "else": {
                  "$ref": "request.prompt"
                }
              }
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
                    "$ref": "request.duration"
                  }
                },
                "else": null
              }
            },
            "resolution": {
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
                          "doubao-seedance"
                        ]
                      }
                    },
                    1
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
                          "doubao-seedance"
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
            "generate_audio": {
              "$if": {
                "condition": {
                  "$and": [
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
                                "doubao-seedance"
                              ]
                            }
                          },
                          1
                        ]
                      }
                    },
                    {
                      "$ref": "request.generateAudio"
                    }
                  ]
                },
                "then": true,
                "else": null
              }
            },
            "images": {
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
                          "doubao-seedance"
                        ]
                      }
                    },
                    1
                  ]
                },
                "then": null,
                "else": {
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
                                  "$lower": {
                                    "$trim": {
                                      "$ref": "media.role"
                                    }
                                  }
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
              }
            },
            "first_frame_url": {
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
                          "doubao-seedance"
                        ]
                      }
                    },
                    1
                  ]
                },
                "then": null,
                "else": {
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
                                "$lower": {
                                  "$trim": {
                                    "$ref": "media.role"
                                  }
                                }
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
                }
              }
            },
            "last_frame_url": {
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
                          "doubao-seedance"
                        ]
                      }
                    },
                    1
                  ]
                },
                "then": null,
                "else": {
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
                                "$lower": {
                                  "$trim": {
                                    "$ref": "media.role"
                                  }
                                }
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
                }
              }
            },
            "videos": {
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
                              "doubao-seedance"
                            ]
                          }
                        },
                        1
                      ]
                    },
                    {
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
                                  "sd-2-"
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
                                  "sd-2.5-"
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
                                  "wan-3.0-"
                                ]
                              }
                            },
                            1
                          ]
                        }
                      ]
                    }
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
            "audios": {
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
                              "doubao-seedance"
                            ]
                          }
                        },
                        1
                      ]
                    },
                    {
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
                                  "sd-2-"
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
                                  "sd-2.5-"
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
                                  "wan-3.0-"
                                ]
                              }
                            },
                            1
                          ]
                        }
                      ]
                    }
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
            "metadata": {
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
                              "doubao-seedance"
                            ]
                          }
                        },
                        1
                      ]
                    },
                    "then": {
                      "resolution": {
                        "$omitEmpty": {
                          "$lower": {
                            "$trim": {
                              "$ref": "request.resolution"
                            }
                          }
                        }
                      },
                      "ratio": {
                        "$omitEmpty": {
                          "$trim": {
                            "$ref": "request.aspectRatio"
                          }
                        }
                      },
                      "content": {
                        "$omitEmpty": {
                          "$concatArrays": [
                            {
                              "$map": {
                                "from": {
                                  "$sortByOrder": {
                                    "$ref": "request.images"
                                  }
                                },
                                "as": "media",
                                "in": {
                                  "type": "image_url",
                                  "image_url": {
                                    "url": {
                                      "$ref": "media.value"
                                    }
                                  },
                                  "role": {
                                    "$switch": {
                                      "cases": [
                                        {
                                          "when": {
                                            "$eq": [
                                              {
                                                "$lower": {
                                                  "$trim": {
                                                    "$ref": "media.role"
                                                  }
                                                }
                                              },
                                              "first_frame"
                                            ]
                                          },
                                          "then": "first_frame"
                                        },
                                        {
                                          "when": {
                                            "$eq": [
                                              {
                                                "$lower": {
                                                  "$trim": {
                                                    "$ref": "media.role"
                                                  }
                                                }
                                              },
                                              "last_frame"
                                            ]
                                          },
                                          "then": "last_frame"
                                        }
                                      ],
                                      "default": "reference_image"
                                    }
                                  }
                                }
                              }
                            },
                            {
                              "$map": {
                                "from": {
                                  "$sortByOrder": {
                                    "$ref": "request.videos"
                                  }
                                },
                                "as": "media",
                                "in": {
                                  "type": "video_url",
                                  "video_url": {
                                    "url": {
                                      "$ref": "media.value"
                                    }
                                  },
                                  "role": "reference_video"
                                }
                              }
                            },
                            {
                              "$map": {
                                "from": {
                                  "$sortByOrder": {
                                    "$ref": "request.audios"
                                  }
                                },
                                "as": "media",
                                "in": {
                                  "type": "audio_url",
                                  "audio_url": {
                                    "url": {
                                      "$ref": "media.value"
                                    }
                                  },
                                  "role": "reference_audio"
                                }
                              }
                            }
                          ]
                        }
                      },
                      "generate_audio": {
                        "$ref": "request.generateAudio"
                      },
                      "watermark": {
                        "$if": {
                          "condition": {
                            "$ref": "request.watermark"
                          },
                          "then": true,
                          "else": null
                        }
                      }
                    }
                  },
                  {
                    "when": {
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
                                  "sd-2-"
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
                                  "sd-2.5-"
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
                                  "wan-3.0-"
                                ]
                              }
                            },
                            1
                          ]
                        }
                      ]
                    },
                    "then": {
                      "$omitEmpty": {
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
                ],
                "default": null
              }
            }
          }
        },
        "poll": {
          "method": "GET",
          "path": "/v1/video/generations/{{taskId}}"
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
                "$ref": "response.task_id"
              },
              {
                "$ref": "response.data.task_id"
              },
              {
                "$ref": "response.id"
              },
              {
                "$ref": "taskId"
              }
            ]
          },
          "status": {
            "$coalesce": [
              {
                "$ref": "response.data.status"
              },
              {
                "$ref": "response.status"
              },
              "pending"
            ]
          },
          "message": {
            "$if": {
              "condition": {
                "$in": [
                  {
                    "$upper": {
                      "$trim": {
                        "$coalesce": [
                          {
                            "$ref": "response.data.status"
                          },
                          {
                            "$ref": "response.status"
                          }
                        ]
                      }
                    }
                  },
                  [
                    "FAILURE",
                    "FAILED"
                  ]
                ]
              },
              "then": {
                "$coalesce": [
                  {
                    "$ref": "response.data.fail_reason"
                  },
                  {
                    "$ref": "response.data.data.fail_reason"
                  },
                  {
                    "$ref": "response.data.data.error.message"
                  },
                  {
                    "$ref": "response.data.data.message"
                  },
                  {
                    "$ref": "response.error.message"
                  }
                ]
              },
              "else": {
                "$ref": "response.error.message"
              }
            }
          },
          "stage": {
            "$switch": {
              "cases": [
                {
                  "when": {
                    "$in": [
                      {
                        "$upper": {
                          "$trim": {
                            "$coalesce": [
                              {
                                "$ref": "response.data.status"
                              },
                              {
                                "$ref": "response.status"
                              }
                            ]
                          }
                        }
                      },
                      [
                        "NOT_START",
                        "SUBMITTED",
                        "QUEUED"
                      ]
                    ]
                  },
                  "then": "排队中"
                },
                {
                  "when": {
                    "$eq": [
                      {
                        "$upper": {
                          "$trim": {
                            "$coalesce": [
                              {
                                "$ref": "response.data.status"
                              },
                              {
                                "$ref": "response.status"
                              }
                            ]
                          }
                        }
                      },
                      "IN_PROGRESS"
                    ]
                  },
                  "then": "生成中"
                },
                {
                  "when": {
                    "$eq": [
                      {
                        "$upper": {
                          "$trim": {
                            "$coalesce": [
                              {
                                "$ref": "response.data.status"
                              },
                              {
                                "$ref": "response.status"
                              }
                            ]
                          }
                        }
                      },
                      "UNKNOWN"
                    ]
                  },
                  "then": "上游状态未知，继续查询"
                }
              ],
              "default": null
            }
          },
          "errorPaths": [
            "error.code",
            "error.message"
          ],
          "resultEphemeral": true
        }
      }
    ]
  }
}
```
<!-- YINGCE_MANIFEST_CONTRACT_END -->
