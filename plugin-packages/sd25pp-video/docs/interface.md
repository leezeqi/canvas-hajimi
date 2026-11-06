# SD2.5PP 视频接口字段

## 协议身份

- 插件 ID：`sd25pp-video`。
- Provider ID：`sd25pp-videos`。
- 能力：`video`。
- 默认 Base URL：无，渠道里填写站点域名，末尾不要带 `/v1`（接口路径已包含 `/v1`）。
- 鉴权：`Authorization: Bearer <apiKey>`，只使用站点令牌（`sk-` 开头）。
- 创建：`POST /v1/videos`（`application/json`）。
- 查询：`GET /v1/videos/{task_id}`。
- 下载：`GET /v1/videos/{task_id}/content`，带同一个 Bearer 令牌。
- 取消：上游未提供，插件不声明取消操作。

## 请求字段

| 字段 | 类型 | 必填 | 来源 | 说明 |
| --- | --- | --- | --- | --- |
| `model` | string | 是 | `request.model` | 本通道只有 `sd2.5pp`。 |
| `prompt` | string | 是 | `request.prompt` | 最长 20000 字；`@图片1` 只是普通文字，上游不会替换。 |
| `seconds` | integer | 是 | `request.duration` | 5–30 的整数；宿主未传时长时发送 `5`。 |
| `resolution` | string | 否 | `request.resolution` | `720p` 或 `1080p`，统一转小写；为空时省略，上游默认 `720p`。 |
| `aspect_ratio` | string | 否 | `request.aspectRatio` | `16:9`、`9:16`、`1:1`、`4:3`、`3:4`、`21:9`；为空时省略，上游默认 `16:9`。 |
| `mode` | string | 否 | 固定值 | 始终发送 `omni_reference`。 |
| `references` | object[] | 是 | 图片、视频、音频 | 依次拼接图片、视频、音频，每类内部按 `order` 排序。 |

`references` 每一项只发送两个键：

| 键 | 取值 |
| --- | --- |
| `kind` | `image`、`video` 或 `audio`，由素材所在数组决定。 |
| `data` | 素材的公网 HTTPS URL。插件声明 `requiresPublicMediaUrls`，宿主把站内资源换成签名 URL，内嵌 data URL 会在提交前被拒绝。 |

上游拒收多余的键，所以插件不发送 `role`、`name` 等宿主字段，也不发送 `duration`、`ratio`、`images`、`size`、`generate_audio` 等其他协议字段。

创建请求带 `Idempotency-Key`，值取宿主为本次提交生成的幂等键。插件声明 `idempotentCreate`：遇到 429、5xx 或网络中断时，宿主用同一个键和同一份请求体重试，上游按幂等键返回同一条任务，不会重复建单和扣费。400 / 422（含当日额度售罄）不重试。

## 提交前校验

| 规则 | 提示 |
| --- | --- |
| 提示词不超过 20000 字 | SD2.5PP 提示词最长 20000 字 |
| 时长为 0（使用默认 5 秒）或 5–30 | SD2.5PP 视频时长必须是 5–30 秒的整数 |
| 图片数 + 视频数 ≥ 1 | SD2.5PP 至少需要 1 张参考图片或 1 个参考视频，只有音频不能生成 |
| 图片 ≤ 30、视频 ≤ 10、音频 ≤ 10 | 对应数量上限提示 |
| 画幅为空或在支持列表内 | SD2.5PP 画幅只支持 16:9、9:16、1:1、4:3、3:4、21:9 |
| 分辨率为空或为 720p / 1080p | SD2.5PP 分辨率只支持 720p 和 1080p |

素材 URL 必须能被上游从公网访问：本机、内网或需要登录的地址会被上游以 400 拒收，所以部署需要对外可访问的 HTTPS 资源地址。

单个素材的大小、格式和时长（参考视频 / 音频 2–15 秒、音频合计不超过 15 秒）宿主拿不到可靠信息，由上游校验；超限时上游返回 400 并指出是第几个参考素材。

## 查询与结果

| 上游状态 | 宿主状态 | 进度文案 |
| --- | --- | --- |
| `queued` | 等待 | 排队中 |
| `in_progress` | 处理中 | 生成中 `progress`% |
| `completed` | 成功 | — |
| `failed` | 失败 | — |

失败原因依次取 `error.message`、`error`（字符串）、`message`、`fail_reason`。上游只返回中文说明，不返回内部错误代码。

查询响应不映射视频地址。任务成功后，宿主调用 `GET /v1/videos/{task_id}/content` 带令牌下载成片并持久化。参考耗时：5 秒约 1–2 分钟，15 秒约 3–5 分钟，30 秒约 5–8 分钟；进度超过 90% 后变慢属于正常合成阶段。

## 计费参考

按条计费，与分辨率无关：5–15 秒每条 4 元，16–30 秒每条 6 元。提交时预扣，失败自动全额退回。售罄时上游在建单前直接返回 400，不扣费。

## 兼容边界

该插件只实现 `sd2.5pp` 通道的 `/v1/videos` 合同。OpenAI Videos、MaxForAI 等同路径但字段不同的协议保持各自插件，不根据模型名猜测。

<!-- YINGCE_MANIFEST_CONTRACT_START -->
## Manifest 完整接口定义

以下 JSON 与插件包内实际 `manifest.json` 逐字段一致，覆盖插件身份、权限、配置、鉴权、参数、校验、创建、Agent、查询、取消、结果下载、响应和 Agent 响应映射。`documentation` 字段的值就是当前完整文档；为避免文档在自身内部无限递归，JSON 中仅用等义占位文本表示正文。

```json
{
  "apiVersion": "yingce.plugin/v2",
  "id": "sd25pp-video",
  "name": "SD2.5PP 全能参考视频",
  "version": "1.0.0",
  "author": "影策",
  "description": "sd2.5pp 视频通道：JSON 创建 /v1/videos，references 全能参考素材，成片走 /content 鉴权下载。",
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
        "id": "sd25pp-videos",
        "label": "SD2.5PP Videos",
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
        "baseUrl": "",
        "requiresPublicMediaUrls": true,
        "idempotentCreate": true,
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
            "description": "上游模型名，本通道固定为 sd2.5pp。"
          },
          {
            "name": "prompt",
            "type": "string",
            "required": true,
            "mapping": "prompt",
            "description": "视频提示词，最长 20000 字；@图片1 之类的写法只作为普通文字发送。"
          },
          {
            "name": "images",
            "type": "media[]",
            "mapping": "references[kind=image]",
            "description": "参考图片，最多 30 张，公网 HTTPS URL；PNG / JPEG / WebP，单张不超过 10 MiB。"
          },
          {
            "name": "videos",
            "type": "media[]",
            "mapping": "references[kind=video]",
            "description": "参考视频，最多 10 个，公网 HTTPS URL；MP4 / WebM，单个 2–15 秒、不超过 100 MiB。"
          },
          {
            "name": "audios",
            "type": "media[]",
            "mapping": "references[kind=audio]",
            "description": "参考音频，最多 10 个，公网 HTTPS URL；MP3 / WAV / OGG / M4A，单个 2–15 秒，合计不超过 15 秒。"
          },
          {
            "name": "duration",
            "type": "integer",
            "mapping": "seconds",
            "description": "生成时长 5–30 秒整数；未指定时发送 5。5–15 秒与 16–30 秒分档计费。"
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
              "21:9"
            ],
            "mapping": "aspect_ratio",
            "description": "画幅比例，未指定时由上游默认 16:9；不支持自适应。"
          },
          {
            "name": "resolution",
            "type": "string",
            "values": [
              "720p",
              "1080p"
            ],
            "mapping": "resolution",
            "description": "分辨率，未指定时由上游默认 720p；不支持 4K 与自适应。"
          }
        ],
        "validations": [
          {
            "assert": {
              "$lte": [
                {
                  "$len": {
                    "$ref": "request.prompt"
                  }
                },
                20000
              ]
            },
            "message": "SD2.5PP 提示词最长 20000 字"
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
                        5
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
            "message": "SD2.5PP 视频时长必须是 5–30 秒的整数"
          },
          {
            "assert": {
              "$gt": [
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
                0
              ]
            },
            "message": "SD2.5PP 至少需要 1 张参考图片或 1 个参考视频，只有音频不能生成"
          },
          {
            "assert": {
              "$lte": [
                {
                  "$len": {
                    "$ref": "request.images"
                  }
                },
                30
              ]
            },
            "message": "SD2.5PP 最多支持 30 张参考图片"
          },
          {
            "assert": {
              "$lte": [
                {
                  "$len": {
                    "$ref": "request.videos"
                  }
                },
                10
              ]
            },
            "message": "SD2.5PP 最多支持 10 个参考视频"
          },
          {
            "assert": {
              "$lte": [
                {
                  "$len": {
                    "$ref": "request.audios"
                  }
                },
                10
              ]
            },
            "message": "SD2.5PP 最多支持 10 个参考音频"
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
            "message": "SD2.5PP 画幅只支持 16:9、9:16、1:1、4:3、3:4、21:9"
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
                      "720p",
                      "1080p"
                    ]
                  ]
                }
              ]
            },
            "message": "SD2.5PP 分辨率只支持 720p 和 1080p"
          }
        ],
        "create": {
          "method": "POST",
          "path": "/v1/videos",
          "contentType": "application/json",
          "headers": {
            "Idempotency-Key": {
              "$omitEmpty": {
                "$ref": "request.extra.idempotencyKey"
              }
            }
          },
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
                "else": 5
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
            "mode": "omni_reference",
            "references": {
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
                      "kind": "image",
                      "data": {
                        "$ref": "media.value"
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
                      "kind": "video",
                      "data": {
                        "$ref": "media.value"
                      }
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
                      "kind": "audio",
                      "data": {
                        "$ref": "media.value"
                      }
                    }
                  }
                }
              ]
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
          }
        }
      }
    ]
  }
}
```
<!-- YINGCE_MANIFEST_CONTRACT_END -->
