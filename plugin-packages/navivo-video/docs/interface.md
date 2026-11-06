# Navivo NewAPI 视频接口字段

## 协议身份

- 插件 ID：`navivo-video`。
- Provider ID：`navivo-video`（与插件 ID 相同）。
- 能力：`video`。
- 默认 Base URL：`https://db.navivo.cc`，末尾不要带 `/v1`（接口路径已包含 `/v1`）。
- 鉴权：`Authorization: Bearer <apiKey>`，使用自己的 NewAPI 令牌（`sk-` 开头），不使用上游渠道密钥。
- 创建：`POST /v1/videos`（`application/json`）。
- 查询：`GET /v1/videos/{task_id}`，任务 ID 取创建响应的 `id`（兼容 `task_id`）。
- 下载：优先使用查询响应中的 `url` / `video_url` / `result_url` / `metadata.url`；完成但没有直链时，`GET /v1/videos/{task_id}/content` 带同一个 Bearer Key 下载。
- 取消：上游未提供，插件不声明取消操作。
- 轮询：声明 `detachedPolling`，提交后释放 worker，由任务调度按间隔回查原任务，避免上游排队时长占用 worker。

## 配置字段

| 字段 | 类型 | 必填 | 含义 |
| --- | --- | --- | --- |
| `apiKey` | secret | 是 | NewAPI 令牌，以 `Authorization: Bearer` 发送 |

## 适用模型

| 模型 | 请求体形状 | 时长 | 参考素材 |
| --- | --- | --- | --- |
| `dola-seedance-20-fast-text-video` | 兼容格式 | 服务端固定 15 秒，不发送 | `images` 字符串数组（首帧或参考图） |
| `dola-seedance-25-text-video` | 兼容格式 | 服务端固定 30 秒，不发送 | `images` 字符串数组（首帧或参考图） |
| `sd 933`（上游兼容 `sd933`） | SD 933 格式 | 数字 `duration`，范围以上游为准 | `images`、`videos`、`audios` 均为 `[{url}]` 对象数组 |

判定规则：模型名转小写并去掉空格后包含 `sd933` 即按 SD 933 格式发送。

## 统一字段映射

| 统一字段 | 类型 | 必填 | 上游映射 | 说明 |
| --- | --- | --- | --- | --- |
| `model` | string | 是 | `model` | NewAPI 模型名，原样发送。 |
| `prompt` | string | 是 | `prompt` | 视频提示词，不能为空。 |
| `images` | media[] | 否 | `images` | 按 `order` 排序。兼容格式发送 URL 字符串数组；SD 933 发送 `[{url}]`。 |
| `videos` | media[] | 否 | `videos[{url}]` | 仅 SD 933；按 `order` 排序。 |
| `audios` | media[] | 否 | `audios[{url}]` | 仅 SD 933；按 `order` 排序。 |
| `duration` | integer | 否 | `duration` | 仅 SD 933 且大于 0 时以数字发送；其他模型不发送。 |
| `aspectRatio` | string | 否 | `aspect_ratio` | `1:1`、`3:4`、`4:3`、`9:16`、`16:9`、`21:9`；为空时省略。 |

不发送 `seconds`、`size`、`resolution`、`generate_audio`：上游文档对 `/v1/videos` JSON 只声明了上述字段，视频时长由服务端按模型约束。

## 请求示例

dola-seedance 系列：

```json
{
  "model": "dola-seedance-20-fast-text-video",
  "prompt": "镜头固定，海浪缓慢拍打礁石",
  "aspect_ratio": "16:9",
  "images": ["https://cdn.example.com/start-frame.png"]
}
```

SD 933：

```json
{
  "model": "sd 933",
  "prompt": "参考图片中的人物、视频动作和音频节奏生成视频",
  "duration": 5,
  "images": [{ "url": "https://example.com/image.jpg" }],
  "videos": [{ "url": "https://example.com/video.mp4" }],
  "audios": [{ "url": "https://example.com/audio.mp3" }]
}
```

空的素材数组和空的画幅不会发送。

## 提交前校验

- 提示词去掉首尾空白后不能为空。
- `aspectRatio` 非空时必须是 `1:1`、`3:4`、`4:3`、`9:16`、`16:9`、`21:9` 之一。
- 非 SD 933 模型带参考视频或参考音频时直接拒绝，不静默丢弃。

## 响应映射

| 统一字段 | 来源 | 说明 |
| --- | --- | --- |
| 任务 ID | `id` → `task_id` → `data.id` → `data.task_id` | 使用 NewAPI 公开任务 ID，不使用上游内部 requestId。 |
| 状态 | `status` → `data.status` | `queued`、`pending`、`submitted` 为排队；`in_progress`、`running`、`processing` 为生成中；`completed`、`succeeded`、`SUCCESS` 为成功；`failed`、`error`、`FAILURE` 为失败。 |
| 阶段 | `status` | 排队显示“上游排队中”，生成中显示“上游生成中”。 |
| 错误信息 | `error.message` → `error` → `fail_reason` → `message` | 失败时展示。 |
| 视频 | 成功状态下 `url` → `video_url` → `result_url` → `metadata.url` → `data.url` → `data.video_url` | 临时链接（`resultEphemeral`），由宿主下载并持久化；全部缺失时走 `/content`。 |
| 错误码 | `error.code` | 非空即判失败。 |

查询 HTTP 429 或网络错误由宿主按轮询策略重试，不重复提交创建请求。

<!-- YINGCE_MANIFEST_CONTRACT_START -->
## Manifest 完整接口定义

以下 JSON 与插件包内实际 `manifest.json` 逐字段一致，覆盖插件身份、权限、配置、鉴权、参数、校验、创建、Agent、查询、取消、结果下载、响应和 Agent 响应映射。`documentation` 字段的值就是当前完整文档；为避免文档在自身内部无限递归，JSON 中仅用等义占位文本表示正文。

```json
{
  "apiVersion": "yingce.plugin/v2",
  "id": "navivo-video",
  "name": "Navivo NewAPI 视频",
  "version": "1.0.0",
  "author": "影策",
  "description": "Navivo NewAPI（db.navivo.cc）视频协议：JSON 创建 /v1/videos，dola-seedance 系列发送 aspect_ratio 与图片 URL 数组，SD 933 额外发送数字 duration 与图片/视频/音频 URL 对象数组；查询原任务，成片优先取直链，缺失时走 /content 鉴权下载。",
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
        "id": "navivo-video",
        "label": "Navivo NewAPI 视频",
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
        "baseUrl": "https://db.navivo.cc",
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
            "description": "NewAPI /v1/models 返回的视频模型名，原样发送，例如 dola-seedance-20-fast-text-video、dola-seedance-25-text-video、sd 933。"
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
            "mapping": "images",
            "description": "首帧或参考图按 order 排序，公网 URL；dola-seedance 系列发送字符串数组，SD 933 发送 [{url}] 对象数组。"
          },
          {
            "name": "videos",
            "type": "media[]",
            "required": false,
            "mapping": "videos[{url}]",
            "description": "参考视频按 order 排序，公网 URL 对象数组；仅 SD 933 支持，其他模型提交前拒绝。"
          },
          {
            "name": "audios",
            "type": "media[]",
            "required": false,
            "mapping": "audios[{url}]",
            "description": "参考音频按 order 排序，公网 URL 对象数组；仅 SD 933 支持，其他模型提交前拒绝。"
          },
          {
            "name": "duration",
            "type": "integer",
            "required": false,
            "mapping": "duration",
            "description": "仅 SD 933 以数字秒发送；dola-seedance 系列时长由服务端按模型固定（15 秒 / 30 秒），不发送。"
          },
          {
            "name": "aspectRatio",
            "type": "string",
            "required": false,
            "values": [
              "1:1",
              "3:4",
              "4:3",
              "9:16",
              "16:9",
              "21:9"
            ],
            "mapping": "aspect_ratio",
            "description": "画幅比例原样发送；为空时省略。"
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
                      "3:4",
                      "4:3",
                      "9:16",
                      "16:9",
                      "21:9"
                    ]
                  ]
                }
              ]
            },
            "message": "Navivo 视频画幅只支持 1:1、3:4、4:3、9:16、16:9、21:9"
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
                            "$join": [
                              {
                                "$split": [
                                  {
                                    "$lower": {
                                      "$ref": "request.model"
                                    }
                                  },
                                  " "
                                ]
                              },
                              ""
                            ]
                          },
                          "sd933"
                        ]
                      }
                    },
                    1
                  ]
                },
                {
                  "$eq": [
                    {
                      "$add": [
                        {
                          "$len": {
                            "$ref": "request.videos"
                          }
                        },
                        {
                          "$len": {
                            "$ref": "request.audios"
                          }
                        }
                      ]
                    },
                    0
                  ]
                }
              ]
            },
            "message": "Navivo 视频只有 SD 933 支持参考视频和参考音频，dola-seedance 系列只接受参考图"
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
            "aspect_ratio": {
              "$omitEmpty": {
                "$trim": {
                  "$ref": "request.aspectRatio"
                }
              }
            },
            "duration": {
              "$if": {
                "condition": {
                  "$and": [
                    {
                      "$gt": [
                        {
                          "$len": {
                            "$split": [
                              {
                                "$join": [
                                  {
                                    "$split": [
                                      {
                                        "$lower": {
                                          "$ref": "request.model"
                                        }
                                      },
                                      " "
                                    ]
                                  },
                                  ""
                                ]
                              },
                              "sd933"
                            ]
                          }
                        },
                        1
                      ]
                    },
                    {
                      "$gt": [
                        {
                          "$ref": "request.duration"
                        },
                        0
                      ]
                    }
                  ]
                },
                "then": {
                  "$ref": "request.duration"
                },
                "else": null
              }
            },
            "images": {
              "$omitEmpty": {
                "$if": {
                  "condition": {
                    "$gt": [
                      {
                        "$len": {
                          "$split": [
                            {
                              "$join": [
                                {
                                  "$split": [
                                    {
                                      "$lower": {
                                        "$ref": "request.model"
                                      }
                                    },
                                    " "
                                  ]
                                },
                                ""
                              ]
                            },
                            "sd933"
                          ]
                        }
                      },
                      1
                    ]
                  },
                  "then": {
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
                        }
                      }
                    }
                  },
                  "else": {
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
                    "url": {
                      "$ref": "media.value"
                    }
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
                          "$ref": "response.status"
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
                          "$ref": "response.status"
                        }
                      },
                      [
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
                    "$ref": "response.url"
                  },
                  {
                    "$ref": "response.video_url"
                  },
                  {
                    "$ref": "response.result_url"
                  },
                  {
                    "$ref": "response.metadata.url"
                  },
                  {
                    "$ref": "response.data.url"
                  },
                  {
                    "$ref": "response.data.video_url"
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
