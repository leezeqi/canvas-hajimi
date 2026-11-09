# 仙凤API 视频接口字段

## 协议身份

- 插件 ID：`xianfeng-video`。
- Provider ID：`xianfeng-video`（与插件 ID 相同）。
- 能力：`video`。
- 默认 Base URL：`https://xianfengapi.xyz`，末尾不要带 `/v1`（接口路径已包含 `/v1`）。
- 鉴权：`Authorization: Bearer <apiKey>`，使用仙凤API 控制台创建的令牌（`sk-` 开头）。
- 创建：`POST /v1/videos`（`application/json`，上游不接受 multipart 建单）。
- 查询：`GET /v1/videos/{task_id}`，任务 ID 取创建响应的 `id`。
- 下载：查询响应带绝对地址 `url` 时直接使用（上游说明该直链无需 Key）；否则 `GET /v1/videos/{task_id}/content` 带同一个 Bearer Key 下载（上游返回 `video/mp4` 或 302 跳转）。相对路径的 `content_url` 不当作直链，统一走 `/content`。
- 取消：上游未提供，插件不声明取消操作。
- 轮询：声明 `detachedPolling`，提交后释放 worker，由任务调度按间隔回查原任务。
- 重试：未声明 `idempotentCreate`。上游要求建单超时不要重复提交（成功建单即预扣费），宿主不会自动重发创建请求。

## 配置字段

| 字段 | 类型 | 必填 | 含义 |
| --- | --- | --- | --- |
| `apiKey` | secret | 是 | 仙凤API 令牌，以 `Authorization: Bearer` 发送 |

## 适用模型

| 模型 | 分辨率 | 画幅 | 输出时长 | 参考素材 |
| --- | --- | --- | --- | --- |
| `seedance-2.5` | 固定 `720p` | `auto`、`16:9`、`9:16`、`1:1`、`4:3`、`3:4`、`21:9` | 整数秒，见下方规则 | 图片 ≤30、视频 ≤10、音频 ≤10（MP3/WAV），合计 ≤30 |
| `JD2.5满参` | 固定 `720p` | 同上，默认 `auto` | 整数秒，见下方规则；不传时上游默认 30 秒，按条计费 | 同上 |

两个模型都只有参考图语义，没有首帧、尾帧字段。画布传来的首帧、尾帧图片按参考图发送；需要指定开场画面时，在提示词里写明“图片1作为开场画面”。

`wan3（班车）` 不在本插件范围：它的参考音视频必须先走 `/v1/media/presign` → `PUT` → `/v1/media/commit` 三步上传后用 `media[{id}]` 引用，图片 URL 限 180 字符，首尾帧用 `role=first/last`，请求体形状完全不同。模型名包含 `wan3` 时提交前拒绝。

## 统一字段映射

| 统一字段 | 类型 | 必填 | 上游映射 | 说明 |
| --- | --- | --- | --- | --- |
| `model` | string | 是 | `model` | 仙凤API 模型名，原样发送。 |
| `prompt` | string | 是 | `prompt` | 不能为空，最多 45000 字。素材按“图片1”“视频1”“音频1”指代。 |
| `images` | media[] | 否 | `images[{url, name}]` | 按 `order` 排序，发送公网 URL 对象，不带 `role`。 |
| `videos` | media[] | 否 | `videos[{url, name, seconds}]` | 按 `order` 排序；`seconds` 为素材自身时长：素材库 `durationMs` 向上取整到整秒。 |
| `audios` | media[] | 否 | `audios[{url, name, seconds}]` | 规则同参考视频。 |

### 素材 `name`

宿主交给上游的签名地址形如 `https://<站点>/api/public/resources/<id>/file?expires=…&signature=…`，路径没有文件后缀。仙凤API 靠后缀识别素材格式，缺后缀时要求填写与真实格式一致的 `name`，并且不能改写素材链接（改写会破坏签名）。插件按素材的 MIME 类型生成 `name`，编号与排序后的位置一致，对应提示词里的“图片1”“视频1”“音频1”：

| 类型 | MIME | `name` |
| --- | --- | --- |
| 图片 | `image/png` / `image/jpeg`（含 `image/jpg`、`image/pjpeg`）/ `image/webp` | `image1.png` / `image1.jpg` / `image1.webp` |
| 视频 | `video/mp4` / `video/quicktime` / `video/webm` | `video1.mp4` / `video1.mov` / `video1.webm` |
| 音频 | `audio/mpeg`（含 `audio/mp3`）/ `audio/wav`（含 `audio/x-wav`、`audio/wave`、`audio/vnd.wave`）/ `audio/aac` / `audio/flac` / `audio/ogg` / `audio/mp4`（含 `audio/x-m4a`、`audio/m4a`） | `audio1.mp3` / `audio1.wav` / `audio1.aac` / `audio1.flac` / `audio1.ogg` / `audio1.m4a` |

MIME 参数（如 `; codecs=…`）会先去掉再匹配。没有完整 MIME 类型的素材（例如直接填写的外部链接，或只标了 `image` 这类泛称）不发送 `name`，也不做格式校验，由上游按链接后缀识别。
| `duration` | integer | 否 | `seconds` | 大于 0 时以整数发送；为 0 时省略，由上游取默认值。 |
| `aspectRatio` | string | 否 | `aspect_ratio` | 去空白转小写后发送；`adaptive` 转为 `auto`；为空时省略。 |
| `resolution` | string | 否 | `resolution` | 总是发送 `720p`；用户选了其他分辨率时提交前拒绝。 |

不发送 `n`、`files`、`generate_audio`、`watermark`、`role`、`token` 和 Data URL 形式的 `data`。

## 请求示例

```json
{
  "model": "JD2.5满参",
  "prompt": "以图片1为场景，参考视频1的运镜，配合音频1的节奏",
  "seconds": 10,
  "aspect_ratio": "16:9",
  "resolution": "720p",
  "images": [{ "url": "https://canvas.example.com/api/public/resources/r1/file?expires=…&signature=…", "name": "image1.png" }],
  "videos": [{ "url": "https://canvas.example.com/api/public/resources/r2/file?expires=…&signature=…", "name": "video1.mp4", "seconds": 5 }],
  "audios": [{ "url": "https://canvas.example.com/api/public/resources/r3/file?expires=…&signature=…", "name": "audio1.wav", "seconds": 3 }]
}
```

空的素材数组、未设置的时长和画幅不会发送。

## 提交前校验

- 提示词去掉首尾空白后不能为空，总长度不超过 45000 字。
- 模型名包含 `wan3` 时拒绝。
- `aspectRatio` 非空时必须是 `auto`、`adaptive`、`16:9`、`9:16`、`1:1`、`4:3`、`3:4`、`21:9` 之一。
- `resolution` 非空时必须是 `720p`（不区分大小写）。
- 参考图 ≤30、参考视频 ≤10、参考音频 ≤10，三者合计 ≤30。
- MIME 类型完整（含 `/`）的素材必须是上表列出的格式（图片 PNG/JPG/WEBP，视频 MP4/MOV/WEBM，音频 MP3/WAV/AAC/FLAC/OGG/M4A），否则无法生成可识别的 `name`；`seedance-2.5` 的参考音频只接受 MP3 和 WAV。
- 输出时长为 0（不发送）或：无参考音视频且参考图 ≤9 张时 4–30 秒，否则 5–30 秒。
- 每个参考视频、参考音频的素材时长必须在 2–30 秒之间；没有时长信息（例如未入素材库的外部链接）时拒绝，避免上游因缺少 `seconds` 返回 400。

上游还要求参考视频与参考音频的时长合计 ≤30 秒；声明式表达式无法对数组求和，这一条由上游校验（参数越界返回 400，不扣费）。

## 响应映射

| 统一字段 | 来源 | 说明 |
| --- | --- | --- |
| 任务 ID | `id` → `task_id` → `data.id` | 形如 `task_xxxxxxxx`。 |
| 状态 | `status` → `data.status` | `queued` 为排队，`in_progress` 为生成中，`completed` 为成功，`failed` 为失败。 |
| 阶段 | `status` | 排队显示“上游排队中”，生成中显示“上游生成中”。 |
| 错误信息 | `error.message` → `message` → `fail_reason` | 失败时展示。 |
| 视频 | 成功状态下的 `url`，且必须是带 `://` 的绝对地址 | 临时链接（`resultEphemeral`），由宿主下载并持久化；缺失或为相对路径时走 `/content`。 |
| 错误码 | `error.code` | 非空即判失败。 |

上游提示：轮询间隔 5–30 秒即可；下载 `/content` 遇到 502/503/504 时按 `Retry-After` 重试下载，不重新生成；410 `artifact_gone` 表示成片已过期。查询 HTTP 429 或网络错误由宿主按轮询策略重试，不重复提交创建请求。

<!-- YINGCE_MANIFEST_CONTRACT_START -->
## Manifest 完整接口定义

以下 JSON 与插件包内实际 `manifest.json` 逐字段一致，覆盖插件身份、权限、配置、鉴权、参数、校验、创建、Agent、查询、取消、结果下载、响应和 Agent 响应映射。`documentation` 字段的值就是当前完整文档；为避免文档在自身内部无限递归，JSON 中仅用等义占位文本表示正文。

```json
{
  "apiVersion": "yingce.plugin/v2",
  "id": "xianfeng-video",
  "name": "仙凤API 视频",
  "version": "1.0.1",
  "author": "影策",
  "description": "仙凤API（xianfengapi.xyz）视频协议：JSON 提交 /v1/videos，适配 seedance-2.5 与 JD2.5满参，参考图/视频/音频均为对象数组并按 MIME 类型附带 name，音视频按素材真实时长填写 seconds；查询原任务，成片优先取直链，缺失时走 /content 鉴权下载。",
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
        "id": "xianfeng-video",
        "label": "仙凤API 视频",
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
        "baseUrl": "https://xianfengapi.xyz",
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
            "description": "仙凤API /v1/models 返回的模型名，原样发送：seedance-2.5、JD2.5满参。wan3（班车）需要先上传素材，本插件不支持。"
          },
          {
            "name": "prompt",
            "type": "string",
            "required": true,
            "mapping": "prompt",
            "description": "视频提示词，不能为空，最多 45000 字。素材按“图片1”“视频1”“音频1”指代。"
          },
          {
            "name": "images",
            "type": "media[]",
            "required": false,
            "mapping": "images[{url,name}]",
            "description": "参考图按 order 排序，发送公网 URL 对象数组，最多 30 张；name 按 MIME 类型生成（如 image1.png），上游据此识别没有后缀的签名地址。上游没有首尾帧语义，首帧、尾帧图片同样按参考图发送。"
          },
          {
            "name": "videos",
            "type": "media[]",
            "required": false,
            "mapping": "videos[{url,name,seconds}]",
            "description": "参考视频按 order 排序，最多 10 个；name 按 MIME 类型生成（如 video1.mp4）；seconds 为素材自身时长，按素材库记录的毫秒时长向上取整，单条 2–30 秒。"
          },
          {
            "name": "audios",
            "type": "media[]",
            "required": false,
            "mapping": "audios[{url,name,seconds}]",
            "description": "参考音频按 order 排序，最多 10 个；name 按 MIME 类型生成（如 audio1.mp3）；seedance-2.5 只接受 MP3/WAV；seconds 规则同参考视频。"
          },
          {
            "name": "duration",
            "type": "integer",
            "required": false,
            "mapping": "seconds",
            "description": "输出视频时长，整数秒：无参考音视频且图片不超过 9 张时 4–30，否则 5–30；为 0 时不发送，由上游取默认值（JD2.5满参默认 30）。"
          },
          {
            "name": "aspectRatio",
            "type": "string",
            "required": false,
            "values": [
              "auto",
              "16:9",
              "9:16",
              "1:1",
              "4:3",
              "3:4",
              "21:9"
            ],
            "mapping": "aspect_ratio",
            "description": "画幅比例；adaptive 转换为 auto，为空时省略。"
          },
          {
            "name": "resolution",
            "type": "string",
            "required": false,
            "values": [
              "720p"
            ],
            "mapping": "resolution",
            "description": "两个模型都固定 720p，插件总是发送 720p；选择其他分辨率时提交前拒绝。"
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
              "$lte": [
                {
                  "$len": {
                    "$ref": "request.prompt"
                  }
                },
                45000
              ]
            },
            "message": "仙凤API 视频提示词最多 45000 字"
          },
          {
            "assert": {
              "$not": {
                "$gt": [
                  {
                    "$len": {
                      "$split": [
                        {
                          "$lower": {
                            "$ref": "request.model"
                          }
                        },
                        "wan3"
                      ]
                    }
                  },
                  1
                ]
              }
            },
            "message": "wan3（班车）需要先上传素材再引用，仙凤API 视频插件只支持 seedance-2.5 和 JD2.5满参"
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
                      "$lower": {
                        "$trim": {
                          "$ref": "request.aspectRatio"
                        }
                      }
                    },
                    [
                      "auto",
                      "adaptive",
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
            "message": "仙凤API 视频画幅只支持 auto、16:9、9:16、1:1、4:3、3:4、21:9"
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
                  "$eq": [
                    {
                      "$lower": {
                        "$trim": {
                          "$ref": "request.resolution"
                        }
                      }
                    },
                    "720p"
                  ]
                }
              ]
            },
            "message": "仙凤API seedance-2.5 与 JD2.5满参只支持 720p"
          },
          {
            "assert": {
              "$and": [
                {
                  "$lte": [
                    {
                      "$len": {
                        "$ref": "request.images"
                      }
                    },
                    30
                  ]
                },
                {
                  "$lte": [
                    {
                      "$len": {
                        "$ref": "request.videos"
                      }
                    },
                    10
                  ]
                },
                {
                  "$lte": [
                    {
                      "$len": {
                        "$ref": "request.audios"
                      }
                    },
                    10
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
                        },
                        {
                          "$len": {
                            "$ref": "request.audios"
                          }
                        }
                      ]
                    },
                    30
                  ]
                }
              ]
            },
            "message": "仙凤API 视频最多 30 张参考图、10 个参考视频、10 个参考音频，合计不超过 30 个"
          },
          {
            "assert": {
              "$or": [
                {
                  "$lte": [
                    {
                      "$ref": "request.duration"
                    },
                    0
                  ]
                },
                {
                  "$and": [
                    {
                      "$lte": [
                        {
                          "$ref": "request.duration"
                        },
                        30
                      ]
                    },
                    {
                      "$gte": [
                        {
                          "$ref": "request.duration"
                        },
                        {
                          "$if": {
                            "condition": {
                              "$or": [
                                {
                                  "$gt": [
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
                                },
                                {
                                  "$gt": [
                                    {
                                      "$len": {
                                        "$ref": "request.images"
                                      }
                                    },
                                    9
                                  ]
                                }
                              ]
                            },
                            "then": 5,
                            "else": 4
                          }
                        }
                      ]
                    }
                  ]
                }
              ]
            },
            "message": "仙凤API 视频时长需为 4–30 秒；带参考视频、参考音频或超过 9 张参考图时为 5–30 秒"
          },
          {
            "assert": {
              "$and": [
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
                            "$and": [
                              {
                                "$gt": [
                                  {
                                    "$len": {
                                      "$split": [
                                        {
                                          "$lower": {
                                            "$trim": {
                                              "$at": [
                                                {
                                                  "$split": [
                                                    {
                                                      "$ref": "media.mimeType"
                                                    },
                                                    ";"
                                                  ]
                                                },
                                                0
                                              ]
                                            }
                                          }
                                        },
                                        "/"
                                      ]
                                    }
                                  },
                                  1
                                ]
                              },
                              {
                                "$not": {
                                  "$in": [
                                    {
                                      "$lower": {
                                        "$trim": {
                                          "$at": [
                                            {
                                              "$split": [
                                                {
                                                  "$ref": "media.mimeType"
                                                },
                                                ";"
                                              ]
                                            },
                                            0
                                          ]
                                        }
                                      }
                                    },
                                    [
                                      "image/png",
                                      "image/jpeg",
                                      "image/jpg",
                                      "image/pjpeg",
                                      "image/webp"
                                    ]
                                  ]
                                }
                              }
                            ]
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
                        "$filter": {
                          "from": {
                            "$ref": "request.videos"
                          },
                          "as": "media",
                          "where": {
                            "$and": [
                              {
                                "$gt": [
                                  {
                                    "$len": {
                                      "$split": [
                                        {
                                          "$lower": {
                                            "$trim": {
                                              "$at": [
                                                {
                                                  "$split": [
                                                    {
                                                      "$ref": "media.mimeType"
                                                    },
                                                    ";"
                                                  ]
                                                },
                                                0
                                              ]
                                            }
                                          }
                                        },
                                        "/"
                                      ]
                                    }
                                  },
                                  1
                                ]
                              },
                              {
                                "$not": {
                                  "$in": [
                                    {
                                      "$lower": {
                                        "$trim": {
                                          "$at": [
                                            {
                                              "$split": [
                                                {
                                                  "$ref": "media.mimeType"
                                                },
                                                ";"
                                              ]
                                            },
                                            0
                                          ]
                                        }
                                      }
                                    },
                                    [
                                      "video/mp4",
                                      "video/quicktime",
                                      "video/webm"
                                    ]
                                  ]
                                }
                              }
                            ]
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
                        "$filter": {
                          "from": {
                            "$ref": "request.audios"
                          },
                          "as": "media",
                          "where": {
                            "$and": [
                              {
                                "$gt": [
                                  {
                                    "$len": {
                                      "$split": [
                                        {
                                          "$lower": {
                                            "$trim": {
                                              "$at": [
                                                {
                                                  "$split": [
                                                    {
                                                      "$ref": "media.mimeType"
                                                    },
                                                    ";"
                                                  ]
                                                },
                                                0
                                              ]
                                            }
                                          }
                                        },
                                        "/"
                                      ]
                                    }
                                  },
                                  1
                                ]
                              },
                              {
                                "$not": {
                                  "$in": [
                                    {
                                      "$lower": {
                                        "$trim": {
                                          "$at": [
                                            {
                                              "$split": [
                                                {
                                                  "$ref": "media.mimeType"
                                                },
                                                ";"
                                              ]
                                            },
                                            0
                                          ]
                                        }
                                      }
                                    },
                                    [
                                      "audio/mpeg",
                                      "audio/mp3",
                                      "audio/wav",
                                      "audio/x-wav",
                                      "audio/wave",
                                      "audio/vnd.wave",
                                      "audio/aac",
                                      "audio/x-aac",
                                      "audio/flac",
                                      "audio/x-flac",
                                      "audio/ogg",
                                      "audio/mp4",
                                      "audio/x-m4a",
                                      "audio/m4a"
                                    ]
                                  ]
                                }
                              }
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
            "message": "仙凤API 参考素材格式只支持：图片 PNG/JPG/WEBP，视频 MP4/MOV/WEBM，音频 MP3/WAV/AAC/FLAC/OGG/M4A"
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
                                "$ref": "request.model"
                              }
                            },
                            "seedance"
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
                        "$filter": {
                          "from": {
                            "$ref": "request.audios"
                          },
                          "as": "media",
                          "where": {
                            "$and": [
                              {
                                "$gt": [
                                  {
                                    "$len": {
                                      "$split": [
                                        {
                                          "$lower": {
                                            "$trim": {
                                              "$at": [
                                                {
                                                  "$split": [
                                                    {
                                                      "$ref": "media.mimeType"
                                                    },
                                                    ";"
                                                  ]
                                                },
                                                0
                                              ]
                                            }
                                          }
                                        },
                                        "/"
                                      ]
                                    }
                                  },
                                  1
                                ]
                              },
                              {
                                "$not": {
                                  "$in": [
                                    {
                                      "$lower": {
                                        "$trim": {
                                          "$at": [
                                            {
                                              "$split": [
                                                {
                                                  "$ref": "media.mimeType"
                                                },
                                                ";"
                                              ]
                                            },
                                            0
                                          ]
                                        }
                                      }
                                    },
                                    [
                                      "audio/mpeg",
                                      "audio/mp3",
                                      "audio/wav",
                                      "audio/x-wav",
                                      "audio/wave",
                                      "audio/vnd.wave"
                                    ]
                                  ]
                                }
                              }
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
            "message": "仙凤API seedance-2.5 的参考音频只支持 MP3 和 WAV"
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
                            "$ref": "request.videos"
                          },
                          {
                            "$ref": "request.audios"
                          }
                        ]
                      },
                      "as": "media",
                      "where": {
                        "$or": [
                          {
                            "$lt": [
                              {
                                "$ref": "media.metadata.durationMs"
                              },
                              2000
                            ]
                          },
                          {
                            "$gt": [
                              {
                                "$ref": "media.metadata.durationMs"
                              },
                              30000
                            ]
                          }
                        ]
                      }
                    }
                  }
                },
                0
              ]
            },
            "message": "仙凤API 参考视频和参考音频每条需为 2–30 秒；缺少时长信息的素材（例如外部链接）无法提交，请先上传到素材库"
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
                  "$toInt": {
                    "$ref": "request.duration"
                  }
                },
                "else": null
              }
            },
            "aspect_ratio": {
              "$omitEmpty": {
                "$if": {
                  "condition": {
                    "$eq": [
                      {
                        "$lower": {
                          "$trim": {
                            "$ref": "request.aspectRatio"
                          }
                        }
                      },
                      "adaptive"
                    ]
                  },
                  "then": "auto",
                  "else": {
                    "$lower": {
                      "$trim": {
                        "$ref": "request.aspectRatio"
                      }
                    }
                  }
                }
              }
            },
            "resolution": "720p",
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
                    "url": {
                      "$ref": "media.value"
                    },
                    "name": {
                      "$switch": {
                        "cases": [
                          {
                            "when": {
                              "$in": [
                                {
                                  "$lower": {
                                    "$trim": {
                                      "$at": [
                                        {
                                          "$split": [
                                            {
                                              "$ref": "media.mimeType"
                                            },
                                            ";"
                                          ]
                                        },
                                        0
                                      ]
                                    }
                                  }
                                },
                                [
                                  "image/png"
                                ]
                              ]
                            },
                            "then": {
                              "$concat": [
                                "image",
                                {
                                  "$toString": {
                                    "$add": [
                                      {
                                        "$ref": "mediaIndex"
                                      },
                                      1
                                    ]
                                  }
                                },
                                ".png"
                              ]
                            }
                          },
                          {
                            "when": {
                              "$in": [
                                {
                                  "$lower": {
                                    "$trim": {
                                      "$at": [
                                        {
                                          "$split": [
                                            {
                                              "$ref": "media.mimeType"
                                            },
                                            ";"
                                          ]
                                        },
                                        0
                                      ]
                                    }
                                  }
                                },
                                [
                                  "image/jpeg",
                                  "image/jpg",
                                  "image/pjpeg"
                                ]
                              ]
                            },
                            "then": {
                              "$concat": [
                                "image",
                                {
                                  "$toString": {
                                    "$add": [
                                      {
                                        "$ref": "mediaIndex"
                                      },
                                      1
                                    ]
                                  }
                                },
                                ".jpg"
                              ]
                            }
                          },
                          {
                            "when": {
                              "$in": [
                                {
                                  "$lower": {
                                    "$trim": {
                                      "$at": [
                                        {
                                          "$split": [
                                            {
                                              "$ref": "media.mimeType"
                                            },
                                            ";"
                                          ]
                                        },
                                        0
                                      ]
                                    }
                                  }
                                },
                                [
                                  "image/webp"
                                ]
                              ]
                            },
                            "then": {
                              "$concat": [
                                "image",
                                {
                                  "$toString": {
                                    "$add": [
                                      {
                                        "$ref": "mediaIndex"
                                      },
                                      1
                                    ]
                                  }
                                },
                                ".webp"
                              ]
                            }
                          }
                        ],
                        "default": null
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
                    },
                    "name": {
                      "$switch": {
                        "cases": [
                          {
                            "when": {
                              "$in": [
                                {
                                  "$lower": {
                                    "$trim": {
                                      "$at": [
                                        {
                                          "$split": [
                                            {
                                              "$ref": "media.mimeType"
                                            },
                                            ";"
                                          ]
                                        },
                                        0
                                      ]
                                    }
                                  }
                                },
                                [
                                  "video/mp4"
                                ]
                              ]
                            },
                            "then": {
                              "$concat": [
                                "video",
                                {
                                  "$toString": {
                                    "$add": [
                                      {
                                        "$ref": "mediaIndex"
                                      },
                                      1
                                    ]
                                  }
                                },
                                ".mp4"
                              ]
                            }
                          },
                          {
                            "when": {
                              "$in": [
                                {
                                  "$lower": {
                                    "$trim": {
                                      "$at": [
                                        {
                                          "$split": [
                                            {
                                              "$ref": "media.mimeType"
                                            },
                                            ";"
                                          ]
                                        },
                                        0
                                      ]
                                    }
                                  }
                                },
                                [
                                  "video/quicktime"
                                ]
                              ]
                            },
                            "then": {
                              "$concat": [
                                "video",
                                {
                                  "$toString": {
                                    "$add": [
                                      {
                                        "$ref": "mediaIndex"
                                      },
                                      1
                                    ]
                                  }
                                },
                                ".mov"
                              ]
                            }
                          },
                          {
                            "when": {
                              "$in": [
                                {
                                  "$lower": {
                                    "$trim": {
                                      "$at": [
                                        {
                                          "$split": [
                                            {
                                              "$ref": "media.mimeType"
                                            },
                                            ";"
                                          ]
                                        },
                                        0
                                      ]
                                    }
                                  }
                                },
                                [
                                  "video/webm"
                                ]
                              ]
                            },
                            "then": {
                              "$concat": [
                                "video",
                                {
                                  "$toString": {
                                    "$add": [
                                      {
                                        "$ref": "mediaIndex"
                                      },
                                      1
                                    ]
                                  }
                                },
                                ".webm"
                              ]
                            }
                          }
                        ],
                        "default": null
                      }
                    },
                    "seconds": {
                      "$divide": [
                        {
                          "$ceilStep": [
                            {
                              "$ref": "media.metadata.durationMs"
                            },
                            1000
                          ]
                        },
                        1000
                      ]
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
                    },
                    "name": {
                      "$switch": {
                        "cases": [
                          {
                            "when": {
                              "$in": [
                                {
                                  "$lower": {
                                    "$trim": {
                                      "$at": [
                                        {
                                          "$split": [
                                            {
                                              "$ref": "media.mimeType"
                                            },
                                            ";"
                                          ]
                                        },
                                        0
                                      ]
                                    }
                                  }
                                },
                                [
                                  "audio/mpeg",
                                  "audio/mp3"
                                ]
                              ]
                            },
                            "then": {
                              "$concat": [
                                "audio",
                                {
                                  "$toString": {
                                    "$add": [
                                      {
                                        "$ref": "mediaIndex"
                                      },
                                      1
                                    ]
                                  }
                                },
                                ".mp3"
                              ]
                            }
                          },
                          {
                            "when": {
                              "$in": [
                                {
                                  "$lower": {
                                    "$trim": {
                                      "$at": [
                                        {
                                          "$split": [
                                            {
                                              "$ref": "media.mimeType"
                                            },
                                            ";"
                                          ]
                                        },
                                        0
                                      ]
                                    }
                                  }
                                },
                                [
                                  "audio/wav",
                                  "audio/x-wav",
                                  "audio/wave",
                                  "audio/vnd.wave"
                                ]
                              ]
                            },
                            "then": {
                              "$concat": [
                                "audio",
                                {
                                  "$toString": {
                                    "$add": [
                                      {
                                        "$ref": "mediaIndex"
                                      },
                                      1
                                    ]
                                  }
                                },
                                ".wav"
                              ]
                            }
                          },
                          {
                            "when": {
                              "$in": [
                                {
                                  "$lower": {
                                    "$trim": {
                                      "$at": [
                                        {
                                          "$split": [
                                            {
                                              "$ref": "media.mimeType"
                                            },
                                            ";"
                                          ]
                                        },
                                        0
                                      ]
                                    }
                                  }
                                },
                                [
                                  "audio/aac",
                                  "audio/x-aac"
                                ]
                              ]
                            },
                            "then": {
                              "$concat": [
                                "audio",
                                {
                                  "$toString": {
                                    "$add": [
                                      {
                                        "$ref": "mediaIndex"
                                      },
                                      1
                                    ]
                                  }
                                },
                                ".aac"
                              ]
                            }
                          },
                          {
                            "when": {
                              "$in": [
                                {
                                  "$lower": {
                                    "$trim": {
                                      "$at": [
                                        {
                                          "$split": [
                                            {
                                              "$ref": "media.mimeType"
                                            },
                                            ";"
                                          ]
                                        },
                                        0
                                      ]
                                    }
                                  }
                                },
                                [
                                  "audio/flac",
                                  "audio/x-flac"
                                ]
                              ]
                            },
                            "then": {
                              "$concat": [
                                "audio",
                                {
                                  "$toString": {
                                    "$add": [
                                      {
                                        "$ref": "mediaIndex"
                                      },
                                      1
                                    ]
                                  }
                                },
                                ".flac"
                              ]
                            }
                          },
                          {
                            "when": {
                              "$in": [
                                {
                                  "$lower": {
                                    "$trim": {
                                      "$at": [
                                        {
                                          "$split": [
                                            {
                                              "$ref": "media.mimeType"
                                            },
                                            ";"
                                          ]
                                        },
                                        0
                                      ]
                                    }
                                  }
                                },
                                [
                                  "audio/ogg"
                                ]
                              ]
                            },
                            "then": {
                              "$concat": [
                                "audio",
                                {
                                  "$toString": {
                                    "$add": [
                                      {
                                        "$ref": "mediaIndex"
                                      },
                                      1
                                    ]
                                  }
                                },
                                ".ogg"
                              ]
                            }
                          },
                          {
                            "when": {
                              "$in": [
                                {
                                  "$lower": {
                                    "$trim": {
                                      "$at": [
                                        {
                                          "$split": [
                                            {
                                              "$ref": "media.mimeType"
                                            },
                                            ";"
                                          ]
                                        },
                                        0
                                      ]
                                    }
                                  }
                                },
                                [
                                  "audio/mp4",
                                  "audio/x-m4a",
                                  "audio/m4a"
                                ]
                              ]
                            },
                            "then": {
                              "$concat": [
                                "audio",
                                {
                                  "$toString": {
                                    "$add": [
                                      {
                                        "$ref": "mediaIndex"
                                      },
                                      1
                                    ]
                                  }
                                },
                                ".m4a"
                              ]
                            }
                          }
                        ],
                        "default": null
                      }
                    },
                    "seconds": {
                      "$divide": [
                        {
                          "$ceilStep": [
                            {
                              "$ref": "media.metadata.durationMs"
                            },
                            1000
                          ]
                        },
                        1000
                      ]
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
                "$and": [
                  {
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
                  {
                    "$gt": [
                      {
                        "$len": {
                          "$split": [
                            {
                              "$ref": "response.url"
                            },
                            "://"
                          ]
                        }
                      },
                      1
                    ]
                  }
                ]
              },
              "then": {
                "$ref": "response.url"
              },
              "else": null
            }
          },
          "errorPaths": [
            "error.code"
          ],
          "messagePaths": [
            "error.message",
            "message",
            "fail_reason"
          ],
          "resultEphemeral": true
        }
      }
    ]
  }
}
```
<!-- YINGCE_MANIFEST_CONTRACT_END -->
