# 会话训练数据归档

## 目标与边界

启用文件归档后，主数据库的 `conversation_archives` 只保存检索索引，不再保存请求或响应正文。正文以不可变、带校验值的版本化记录写入独立数据盘，避免 PostgreSQL 的 TOAST 数据持续挤满系统盘。

会被归档的会话接口：

- OpenAI Chat Completions、legacy Completions、Responses、Responses Compact；
- Anthropic Messages；
- Gemini `generateContent` / `streamGenerateContent` 的文本会话；
- OpenAI Realtime 的文本、工具调用、控制事件和音频转写；
- `/pg/chat/completions` 等最终进入上述 relay 的兼容入口。

不会归档：图片生成、图片编辑、视频任务、TTS/转录等独立音频接口、Embedding、Rerank、Moderation、异步媒体任务。Realtime 内的音频二进制也不保存，只记录剔除字段的位置、SHA-256 和大小；文本、工具调用、转写及事件顺序仍完整保留。会话输入中已经出现在客户端或实际上游请求里的图片、文件和 Data URI 会随原始字节保存，大字段进入内容寻址存储并去重。

## 存储格式

目录布局：

```text
conversation-archive/
  .new-api-conversation-archive-volume
  records/YYYY/MM/DD/<archive-id>-<random>.json.gz
  blobs/sha256/<prefix>/<sha256>.gz
  migration/legacy-conversation-logs.checkpoint.json
```

每条 record 是版本化 envelope，包含客户端原始请求、客户端最终响应、每次上游重试的实际请求与响应、协议转换链、状态、模型和完整性声明。大 JSON 字符串及大块二进制按 SHA-256 放入 `blobs`，恢复时会验证 record、每个 payload 和 blob 的大小及哈希，并逐字节重建原数据。

`complete=false` 和 `missing` 是训练筛选条件，不得在导出时忽略。旧 `conversation_logs` 本身没有保存原始客户端请求、上游重试和失败响应，因此迁移后会明确标记为历史不完整，而不会伪装成新格式的完整记录。

## 配置与挂载保护

```env
CONVERSATION_LOG_ENABLED=true
CONVERSATION_LOG_STORAGE_PATH=/data/conversation-archive
CONVERSATION_LOG_STORAGE_SENTINEL=.new-api-conversation-archive-volume
CONVERSATION_LOG_BLOB_THRESHOLD_BYTES=65536
CONVERSATION_LOG_MIN_FREE_BYTES=10737418240
```

生产环境必须把 `CONVERSATION_LOG_STORAGE_PATH` 映射到独立数据盘，并预先在宿主机目录创建 sentinel 普通文件。sentinel 不存在时服务拒绝启动，防止挂载丢失后悄悄写回系统盘。可用空间低于安全预留值时，新会话会在访问上游前返回 503；这样会牺牲新请求可用性，但不会让数据库和系统盘随归档盘一起崩溃。

## 历史迁移、校验与训练导出

容器镜像内包含 `/conversation-archive` 工具。工具从不执行 `TRUNCATE` 或删除源数据。

```bash
# 以启动时 MAX(id) 为固定高水位，断点续传迁移旧表
/conversation-archive -mode migrate -root /data/conversation-archive -batch-size 5

# 逐条恢复并核对数据库 manifest 中的 SHA-256
/conversation-archive -mode verify -root /data/conversation-archive -batch-size 20

# 导出跨协议、逐条可恢复的 gzip JSONL；payloads_base64 保存原始字节
/conversation-archive -mode export -root /data/conversation-archive \
  -output /data/conversation-archive/exports/training-raw.jsonl.gz -batch-size 20
```

迁移 checkpoint 每完成一条才原子推进；重新运行会先恢复并校验已经建立索引的记录。训练 JSONL 是协议中立的原始层，后续训练流水线可按 `protocol` 选择适配器，并按 `complete`、用户授权状态、时间、模型等字段筛选，而不必再次访问线上数据库。

## 生产释放空间门槛

只有同时满足以下条件，才允许在单独获批后清理 `conversation_logs`：

1. 固定高水位范围全部迁移，checkpoint 到达 high-water；
2. `verify` 全量通过，记录数与源范围核对一致；
3. 归档目录已经复制到第二个独立故障域并完成抽样恢复；
4. 新归档链路持续写入、查询索引、恢复记录和 `/api/status` 均通过；
5. 数据盘容量与增长告警已配置，不能把 100G 单盘视作长期唯一副本；
6. 当前操作回合再次取得明确的数据库清理授权。

历史 `.dump`、`.tar.gz` 等归档不属于自动清理范围，任何删除都必须单独批准。
