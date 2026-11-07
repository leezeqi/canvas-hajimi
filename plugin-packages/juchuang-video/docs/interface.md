# 聚创引擎视频接口

## 身份与配置

- 插件和 Provider ID：`juchuang-video`。
- 可见性：仅管理员可见。普通用户的插件中心、插件状态和协议目录不返回该插件，自定义渠道也不能使用；管理员把它配置到系统渠道模型后，普通用户可以通过该系统渠道正常生成，插件被平台停用时一并失败。
- 默认 Base URL：`https://nexhub.token6688.com`。
- `apiKey`：Bearer 密钥；带参考素材时同时需要文件上传权限。
- 不修改其他视频协议，不支持上游未公开的取消接口。

## 上传、提交与查询

1. 图片 `images`、视频 `videos`、音频 `audios` 均先 `POST /v1/files`，multipart 字段 `file`，单文件不超过 50MB，同时遵守模型卡更严格的限制。
2. 必须取得有效 HTTPS `url`，没有 URL 时停止提交。协议通过 `media.uploadId` 保存 URL，不使用影策临时签名链接作为聚创的生成素材。
3. `POST /v1/media/generate`，JSON 顶层只有 `model`、`prompt`、`client_request_id` 和 `params`。`model` 必须是聚创模型卡 ID。`prompt` 必填。
4. `duration`、`aspectRatio`、`resolution` 映射到 `params.duration`、`params.aspect_ratio`、`params.resolution`。`operation` 和素材角色映射 `params.mode`：text-to-video、first-frame、first-last、reference。首帧先于尾帧，其他素材按 order 排序。
5. `providerOptions.juchuang-video` 只允许显式 `quality`、`lane_id`，不透传任意 body 覆盖受校验参数。模型未公开音频生成或水印开关时不发送这些字段。
6. `client_request_id` 使用同一任务的宿主幂等键；提交重试仍使用原键。已有 task_id 的任务只查询，不重新上传或提交。
7. `GET /v1/tasks/{task_id}`，分离式轮询。503 等暂时性查询故障不重新创建任务。

## 响应与错误

提交兼容 `data.task_id`、`task_id` 和 `id`。查询以 `is_final` 为终态依据：false（包括 needs_review）继续等待；true 且 status=completed 才成功，其余终态失败。
成功优先读取 `output_url`，再读取 result.videos、data、result_url。失败从 error.message、error、msg、message 提取，最终由后端过滤成可公开原因。HTTP 失败保留真实失败语义。

## 模型边界

例如 `seedance-2-0-special` 按文档固定 15 秒；不能复用其他上游的“特价seedance-2.0-720p”别名或 12 秒配置。
价格、模型 ID、素材限制和支持的模式均单独配置，不根据名称猜测，也不自动切换现有渠道。

<!-- YINGCE_MANIFEST_CONTRACT_START -->
## Manifest 完整接口定义

以下 JSON 与插件包内实际 `manifest.json` 逐字段一致，覆盖插件身份、权限、配置、鉴权、参数、校验、创建、Agent、查询、取消、结果下载、响应和 Agent 响应映射。`documentation` 字段的值就是当前完整文档；为避免文档在自身内部无限递归，JSON 中仅用等义占位文本表示正文。

```json
{
  "apiVersion": "yingce.plugin/v2",
  "id": "juchuang-video",
  "name": "聚创引擎视频",
  "version": "1.0.0",
  "author": "影策",
  "description": "聚创引擎视频异步协议，先上传参考素材，再提交生成并查询原任务。",
  "permissions": [
    "generation.run",
    "media.read"
  ],
  "configuration": {
    "fields": [
      {
        "name": "apiKey",
        "type": "secret",
        "label": "API Key（需视频和文件权限）",
        "required": true
      }
    ]
  },
  "contributes": {
    "providers": [
      {
        "id": "juchuang-video",
        "label": "聚创引擎视频",
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
        "baseUrl": "https://nexhub.token6688.com",
        "requiresPublicMediaUrls": false,
        "idempotentCreate": true,
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
            "description": "聚创模型卡 ID，不能使用其他中转站的模型别名。"
          },
          {
            "name": "prompt",
            "type": "string",
            "required": true,
            "mapping": "prompt",
            "description": "视频描述。"
          },
          {
            "name": "images",
            "type": "media[]",
            "required": false,
            "mapping": "params.images",
            "description": "先上传再按顺序传 URL；首帧在尾帧之前。"
          },
          {
            "name": "videos",
            "type": "media[]",
            "required": false,
            "mapping": "params.videos",
            "description": "参考视频，需满足模型卡限制及上传的 50MB 上限。"
          },
          {
            "name": "audios",
            "type": "media[]",
            "required": false,
            "mapping": "params.audios",
            "description": "参考音频，先上传到聚创文件服务。"
          },
          {
            "name": "duration",
            "type": "integer",
            "required": false,
            "mapping": "params.duration",
            "description": "时长按模型卡配置，不根据模型名猜测。"
          },
          {
            "name": "aspectRatio",
            "type": "string",
            "required": false,
            "mapping": "params.aspect_ratio",
            "description": "画幅比例。"
          },
          {
            "name": "resolution",
            "type": "string",
            "required": false,
            "mapping": "params.resolution",
            "description": "输出清晰度。"
          },
          {
            "name": "operation",
            "type": "string",
            "required": false,
            "mapping": "params.mode",
            "description": "按输入角色映射文生、首帧、首尾帧或参考生。"
          },
          {
            "name": "providerOptions",
            "type": "object",
            "required": false,
            "mapping": "params.quality/params.lane_id",
            "description": "juchuang-video 命名空间可选 quality、lane_id。"
          }
        ],
        "upload": {
          "kinds": [
            "image",
            "video",
            "audio"
          ],
          "method": "POST",
          "path": "/v1/files",
          "originPath": true,
          "contentType": "multipart/form-data",
          "files": [
            {
              "name": "file",
              "source": {
                "$ref": "media"
              }
            }
          ],
          "id": {
            "$if": {
              "condition": {
                "$eq": [
                  {
                    "$at": [
                      {
                        "$split": [
                          {
                            "$ref": "response.url"
                          },
                          "://"
                        ]
                      },
                      0
                    ]
                  },
                  "https"
                ]
              },
              "then": {
                "$ref": "response.url"
              },
              "else": null
            }
          }
        },
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
              "$gt": [
                {
                  "$len": {
                    "$ref": "request.extra.idempotencyKey"
                  }
                },
                0
              ]
            },
            "message": "缺少视频提交幂等标识，请重新提交"
          },
          {
            "assert": {
              "$eq": [
                {
                  "$len": {
                    "$filter": {
                      "from": {
                        "$ref": "request.inputs"
                      },
                      "as": "media",
                      "where": {
                        "$not": {
                          "$eq": [
                            {
                              "$at": [
                                {
                                  "$split": [
                                    {
                                      "$ref": "media.uploadId"
                                    },
                                    "://"
                                  ]
                                },
                                0
                              ]
                            },
                            "https"
                          ]
                        }
                      }
                    }
                  }
                },
                0
              ]
            },
            "message": "参考素材上传未返回有效 HTTPS 链接，请检查文件权限后重新上传"
          }
        ],
        "create": {
          "method": "POST",
          "path": "/v1/media/generate",
          "originPath": true,
          "contentType": "application/json",
          "body": {
            "model": {
              "$ref": "request.model"
            },
            "prompt": {
              "$ref": "request.prompt"
            },
            "client_request_id": {
              "$ref": "request.extra.idempotencyKey"
            },
            "params": {
              "mode": {
                "$switch": {
                  "cases": [
                    {
                      "when": {
                        "$eq": [
                          {
                            "$ref": "request.operation"
                          },
                          "reference_to_video"
                        ]
                      },
                      "then": "reference"
                    },
                    {
                      "when": {
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
                                    "last_frame"
                                  ]
                                }
                              }
                            }
                          },
                          0
                        ]
                      },
                      "then": "first-last"
                    },
                    {
                      "when": {
                        "$eq": [
                          {
                            "$ref": "request.operation"
                          },
                          "image_to_video"
                        ]
                      },
                      "then": "first-frame"
                    },
                    {
                      "when": {
                        "$gt": [
                          {
                            "$len": {
                              "$ref": "request.inputs"
                            }
                          },
                          0
                        ]
                      },
                      "then": "reference"
                    }
                  ],
                  "default": "text-to-video"
                }
              },
              "duration": {
                "$omitEmpty": {
                  "$ref": "request.duration"
                }
              },
              "resolution": {
                "$omitEmpty": {
                  "$ref": "request.resolution"
                }
              },
              "aspect_ratio": {
                "$omitEmpty": {
                  "$ref": "request.aspectRatio"
                }
              },
              "images": {
                "$omitEmpty": {
                  "$map": {
                    "from": {
                      "$concatArrays": [
                        {
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
                        {
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
                        {
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
                      ]
                    },
                    "as": "media",
                    "in": {
                      "$ref": "media.uploadId"
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
                      "$ref": "media.uploadId"
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
                      "$ref": "media.uploadId"
                    }
                  }
                }
              },
              "quality": {
                "$omitEmpty": {
                  "$ref": "request.providerOptions.juchuang-video.quality"
                }
              },
              "lane_id": {
                "$omitEmpty": {
                  "$ref": "request.providerOptions.juchuang-video.lane_id"
                }
              }
            }
          }
        },
        "poll": {
          "method": "GET",
          "path": "/v1/tasks/{{taskId}}",
          "originPath": true
        },
        "response": {
          "taskId": {
            "$coalesce": [
              {
                "$ref": "response.data.task_id"
              },
              {
                "$ref": "response.task_id"
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
            "$if": {
              "condition": {
                "$eq": [
                  {
                    "$ref": "response.is_final"
                  },
                  true
                ]
              },
              "then": {
                "$if": {
                  "condition": {
                    "$eq": [
                      {
                        "$ref": "response.status"
                      },
                      "completed"
                    ]
                  },
                  "then": "succeeded",
                  "else": "failed"
                }
              },
              "else": "pending"
            }
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
                      "needs_review"
                    ]
                  },
                  "then": "等待上游审核"
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
                  "then": "上游生成中"
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
                "$ref": "response.error"
              },
              {
                "$ref": "response.msg"
              },
              {
                "$ref": "response.message"
              }
            ]
          },
          "videos": {
            "$if": {
              "condition": {
                "$and": [
                  {
                    "$eq": [
                      {
                        "$ref": "response.is_final"
                      },
                      true
                    ]
                  },
                  {
                    "$eq": [
                      {
                        "$ref": "response.status"
                      },
                      "completed"
                    ]
                  }
                ]
              },
              "then": {
                "$coalesce": [
                  {
                    "$ref": "response.output_url"
                  },
                  {
                    "$ref": "response.result.videos"
                  },
                  {
                    "$ref": "response.data"
                  },
                  {
                    "$ref": "response.result_url"
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
