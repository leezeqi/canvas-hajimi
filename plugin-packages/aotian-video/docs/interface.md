# 傲天 Seedance 视频接口字段

## 协议身份

- 插件 ID：`aotian-video`。
- Provider ID：`aotian-video`。
- 能力：`video`。
- 默认 Base URL：`https://aotianzz.xyz`；经 NewAPI 中转时把渠道地址改为中转站根地址。
- 鉴权驱动：`bearer`。
- 创建：`POST /v1/videos`，`application/json`。
- 查询：`GET /v1/videos/{{taskId}}`。
- 不调用 `/v1/videos/{id}/content`；完成响应里的 `url` 可匿名下载。

## 配置字段

| 字段 | 类型 | 必填 | 含义 |
| --- | --- | --- | --- |
| `apiKey` | secret | 是 | API Key，以 `Authorization: Bearer` 发送 |

## 统一字段映射

| 统一字段 | 类型 | 必填 | 上游映射 | 说明 |
| --- | --- | --- | --- | --- |
| `model` | string | 是 | `model` | 上游 `/v1/models` 返回的视频模型 ID。 |
| `prompt` | string | 是 | `prompt` | 视频描述，不能为空。 |
| `images` | media[] | 否 | `images` | 参考图按顺序传公网 HTTPS URL 数组，首帧在尾帧之前。 |
| `videos` | media[] | 否 | `reference_videos` | 参考视频公网 HTTPS URL 数组，仅在上游模型支持时生效。 |
| `audios` | media[] | 否 | `reference_audios` | 参考音频公网 HTTPS URL 数组，仅在上游模型支持时生效。 |
| `duration` | integer | 否 | `seconds` | 大于 0 时以字符串发送；按秒计费模型必须提供。 |
| `aspectRatio` | string | 否 | `size` | 画幅比例原样发送，例如 `16:9`；不换算像素。 |
| `resolution` | string | 否 | `resolution` | 清晰度档位，例如 `720p`。 |

上游文档未声明生成音频、水印和首尾帧专用字段，本插件不发送这些字段。模型卡上的“生成音频”开关不会改变上游请求，应按实际上游能力关闭。

## 参考素材

- 插件声明 `requiresPublicMediaUrls: true`：宿主把画布资源换成带签名和有效期的公网 HTTPS 下载地址，内嵌 `data:` 素材直接拒绝，不会退回 base64 或 multipart。
- 校验步骤检查 `images`、`videos`、`audios` 中每个地址的协议，非 HTTPS 地址在提交前失败。
- 上游在排队和生成期间回源下载素材，签名地址有效期由后台“资源与请求策略”的模型输入链接有效期决定，应覆盖上游的排队与生成时长。
- 上游文档未给出参考素材数量上限，按账号权限和模型能力决定；模型卡上的数量限制只在影策侧生效。

## 上游请求模板逐字段清单

| 上游位置 | 值或转换表达式 |
| --- | --- |
| `create.method` | `"POST"` |
| `create.path` | `"/v1/videos"` |
| `create.contentType` | `"application/json"` |
| `create.body.model` | `request.model` |
| `create.body.prompt` | `request.prompt` |
| `create.body.seconds` | `request.duration > 0` 时为字符串化的 `request.duration`，否则省略 |
| `create.body.size` | `request.aspectRatio`，为空时省略 |
| `create.body.resolution` | `request.resolution`，为空时省略 |
| `create.body.images` | 按 `order` 排序后的 `request.images[].value`，为空时省略 |
| `create.body.reference_videos` | 按 `order` 排序后的 `request.videos[].value`，为空时省略 |
| `create.body.reference_audios` | 按 `order` 排序后的 `request.audios[].value`，为空时省略 |
| `poll.method` | `"GET"` |
| `poll.path` | `"/v1/videos/{{taskId}}"` |

## 轮询

插件声明 `detachedPolling: true`：提交后释放 worker，由任务调度按间隔回查同一个上游任务号，不因等待超时重新提交。上游典型生成时间为 10–25 分钟，`processing` 表示归档中，需要继续查询。

## 响应映射逐字段清单

| 映射位置 | 上游路径或转换表达式 |
| --- | --- |
| `response.taskId` | `response.id`，依次回退 `response.task_id`、`response.data.id`、已知任务号 |
| `response.status` | `response.status`，缺失时为 `pending`；`queued` 视为排队，`in_progress` / `processing` 视为处理中，`completed` 视为成功，`failed` 视为失败 |
| `response.stage` | `in_progress` 为“上游生成中”，`processing` 为“上游归档中”，其余为“上游排队中” |
| `response.message` | `response.error.message`，依次回退 `response.fail_reason`、`response.message` |
| `response.videos` | 仅在 `status = completed` 时取 `response.url`，依次回退 `response.video_url`、`response.result_url`、`response.metadata.url` |
| `response.errorPaths[0]` | `"error.code"` |
| `response.resultEphemeral` | `true` |

## 响应与错误

插件把上游任务号、状态、阶段、失败原因和视频地址映射为统一结果。结果地址标记为临时地址，由宿主立即下载持久化。HTTP 400 表示参数或任务不可用，不能重复提交；失败任务的 `error.message` 作为失败原因返回，不包装成成功。

## 兼容边界

- 只覆盖傲天文档声明的 JSON 字段；上游对未知字段静默忽略，因此不额外透传扩展对象。
- 画幅只发 `size`，不发 `aspect_ratio` 或 `ratio`。
- 经 NewAPI 中转时，中转插件必须原样转发 JSON 请求体，并在查询响应中保留上游的 `status`、`url` 和 `error`。

<!-- YINGCE_MANIFEST_CONTRACT_START -->
## Manifest 完整接口定义

以下 JSON 与插件包内实际 `manifest.json` 逐字段一致，覆盖插件身份、权限、配置、鉴权、参数、校验、创建、Agent、查询、取消、结果下载、响应和 Agent 响应映射。`documentation` 字段的值就是当前完整文档；为避免文档在自身内部无限递归，JSON 中仅用等义占位文本表示正文。

```json
{
  "apiVersion": "yingce.plugin/v2",
  "id": "aotian-video",
  "name": "傲天 Seedance 视频",
  "version": "1.0.0",
  "author": "影策",
  "description": "傲天 AoTian 视频协议：JSON 提交公网 HTTPS 参考素材链接，查询原任务并直接下载结果链接。",
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
        "id": "aotian-video",
        "label": "傲天 Seedance 视频",
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
        "baseUrl": "https://aotianzz.xyz",
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
            "description": "上游 /v1/models 返回的视频模型 ID。"
          },
          {
            "name": "prompt",
            "type": "string",
            "required": true,
            "mapping": "prompt",
            "description": "视频描述，不能为空。"
          },
          {
            "name": "images",
            "type": "media[]",
            "required": false,
            "mapping": "images",
            "description": "参考图按顺序传公网 HTTPS URL 数组，首帧在尾帧之前；不发送 data URL 或 multipart 文件。"
          },
          {
            "name": "videos",
            "type": "media[]",
            "required": false,
            "mapping": "reference_videos",
            "description": "参考视频公网 HTTPS URL 数组，仅在上游模型支持时生效。"
          },
          {
            "name": "audios",
            "type": "media[]",
            "required": false,
            "mapping": "reference_audios",
            "description": "参考音频公网 HTTPS URL 数组，仅在上游模型支持时生效。"
          },
          {
            "name": "duration",
            "type": "integer",
            "required": false,
            "mapping": "seconds",
            "description": "时长秒数，以字符串发送；按秒计费模型必须提供。"
          },
          {
            "name": "aspectRatio",
            "type": "string",
            "required": false,
            "mapping": "size",
            "description": "画幅比例原样发送，例如 16:9；不换算像素，也不使用 aspect_ratio 或 ratio。"
          },
          {
            "name": "resolution",
            "type": "string",
            "required": false,
            "mapping": "resolution",
            "description": "清晰度档位，例如 720p。"
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
              "$eq": [
                {
                  "$len": {
                    "$filter": {
                      "from": {
                        "$concatArrays": [
                          {
                            "$ref": "request.images"
                          },
                          {
                            "$ref": "request.videos"
                          },
                          {
                            "$ref": "request.audios"
                          }
                        ]
                      },
                      "as": "media",
                      "where": {
                        "$ne": [
                          {
                            "$lower": {
                              "$at": [
                                {
                                  "$split": [
                                    {
                                      "$ref": "media.value"
                                    },
                                    "://"
                                  ]
                                },
                                0
                              ]
                            }
                          },
                          "https"
                        ]
                      }
                    }
                  }
                },
                0
              ]
            },
            "message": "傲天视频要求参考素材使用公网 HTTPS 链接，请检查素材公网访问地址后重试"
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
                  "$toString": {
                    "$ref": "request.duration"
                  }
                },
                "else": null
              }
            },
            "size": {
              "$omitEmpty": {
                "$ref": "request.aspectRatio"
              }
            },
            "resolution": {
              "$omitEmpty": {
                "$ref": "request.resolution"
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
                    "$ref": "media.value"
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
                    "$ref": "media.value"
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
              "pending"
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
                      "in_progress"
                    ]
                  },
                  "then": "上游生成中"
                },
                {
                  "when": {
                    "$eq": [
                      {
                        "$ref": "response.status"
                      },
                      "processing"
                    ]
                  },
                  "then": "上游归档中"
                }
              ],
              "default": "上游排队中"
            }
          },
          "message": {
            "$coalesce": [
              {
                "$ref": "response.error.message"
              },
              {
                "$ref": "response.fail_reason"
              },
              {
                "$ref": "response.message"
              }
            ]
          },
          "videos": {
            "$if": {
              "condition": {
                "$eq": [
                  {
                    "$ref": "response.status"
                  },
                  "completed"
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
