# 会话训练数据归档

## 目标与边界

启用文件归档后，主数据库的 `conversation_archives` 只保存检索索引，不再保存请求或响应正文。正文以不可变、带校验值的版本化记录写入独立数据盘，避免 PostgreSQL 的 TOAST 数据持续挤满系统盘。

会被归档的会话接口：

- OpenAI Chat Completions、legacy Completions、Responses、Responses Compact；
- Anthropic Messages；
- Gemini `generateContent` / `streamGenerateContent` 的文本会话；
- OpenAI Realtime 的文本、工具调用、控制事件和音频转写；
- `/pg/chat/completions` 等最终进入上述 relay 的兼容入口。

不会归档：图片生成、图片编辑、视频任务、TTS/转录等独立音频接口、Embedding、Rerank、Moderation、独立 `/v1/alpha/search` 搜索调用和异步媒体任务。Realtime 内的音频二进制，以及 Responses `image_generation_call` 产生的图片正文也不保存，只记录剔除字段或事件的 SHA-256 和大小；文本、工具调用、转写、状态及事件顺序仍保留。会话输入中已经出现在客户端或实际上游请求里的图片、文件和 Data URI 会随原始字节保存，大 payload 进入内容寻址存储并去重。

远程图片或文件 URL 可能过期，当前初步归档不会主动下载第三方地址；遇到这种输入会写入 `external_attachment_not_pinned` 并标记 `complete=false`，避免后续清洗误认为附件已经完整保存。

## 存储格式

目录布局：

```text
conversation-archive/
  .new-api-conversation-archive-volume
  pending/<archive-id>-<random>.pending/
  records/YYYY/MM/DD/<archive-id>-<random>.json.gz
  blobs/sha256/<prefix>/<sha256>.gz
  migration/legacy-conversation-logs.checkpoint.json
```

捕获期间，请求和流式响应直接增量写入同一数据盘的 `pending`，不会把整段长会话积压在进程内存。为覆盖媒体字段恰好跨网络分片时的崩溃窗口，响应还会保留一份 recovery-only 原始暂存；正常完成时它不进入正式 record，并在 record 校验成功后随 pending 一起删除。若捕获中断，恢复记录会强制 `complete=false`，可能同时带有 `recovery_raw_*` 原始 payload，后续清洗必须优先使用它补齐尾部并再次执行媒体剔除。每条 record 是版本化 envelope，包含客户端原始请求、客户端最终响应、每次上游重试及供应商轮询/上传等辅助调用的实际请求与响应、协议转换链、状态、模型和完整性声明。请求 URL 会保留影响协议语义的普通查询参数，但 API key、token、签名和 secret 等查询值会被明确掩码；`Anthropic-Version`、`Anthropic-Beta`、`OpenAI-Beta` 等非凭证协议头会单独保留，认证头不会进入归档。大 payload 按 SHA-256 放入 `blobs`，恢复时会验证 record、每个 payload 和 blob 的大小及哈希，并逐字节重建原数据。

`complete=false` 和 `missing` 是训练筛选条件，不得在导出时忽略。旧 `conversation_logs` 本身没有保存原始客户端请求、上游重试和失败响应，因此迁移后会明确标记为历史不完整，而不会伪装成新格式的完整记录。

## 配置与挂载保护

```env
CONVERSATION_LOG_ENABLED=true
# 仅供 Docker Compose bind mount 使用：宿主机上的独立数据盘目录
CONVERSATION_LOG_HOST_PATH=/root/data/disk/conversation-archive
# 应用在容器内看到的归档目录
CONVERSATION_LOG_STORAGE_PATH=/data/conversation-archive
CONVERSATION_LOG_STORAGE_SENTINEL=.new-api-conversation-archive-volume
CONVERSATION_LOG_BLOB_THRESHOLD_BYTES=65536
CONVERSATION_LOG_MIN_FREE_BYTES=10737418240
```

生产环境必须把 `CONVERSATION_LOG_HOST_PATH` 设置为宿主机独立数据盘上的真实目录；Compose 不提供系统盘 fallback，变量缺失会直接拒绝启动。`CONVERSATION_LOG_STORAGE_PATH` 同时控制 bind mount 的容器内 `target` 和应用配置，默认 `/data/conversation-archive`；sentinel 名称和空间阈值也可由同名环境变量覆盖。部署命令必须从 Compose 目录执行，或显式传入 `--env-file`，避免读取错误的环境文件。宿主机目录需要预先创建 sentinel 普通文件。服务启动和每一次写入前都会检查 sentinel；运行中挂载丢失后会立即停止归档，防止悄悄写回系统盘。归档根目录拒绝 `/` 等过宽路径和末级符号链接。Compose 使用 `create_host_path: false`，宿主机目录缺失时不会自动在系统盘创建替代目录。可用空间低于安全预留值时，新会话会在访问上游前返回 503；这样会牺牲新请求可用性，但不会让数据库和系统盘随归档盘一起崩溃。

## 历史迁移、校验与训练导出

容器镜像内包含 `/conversation-archive` 工具。工具从不执行 `TRUNCATE` 或删除源数据。

```bash
# 以启动时 MAX(id) 为固定高水位，断点续传迁移旧表
/conversation-archive -mode migrate -root /data/conversation-archive -batch-size 5

# 逐条恢复并核对数据库 manifest 中的 SHA-256
/conversation-archive -mode verify -root /data/conversation-archive -batch-size 20

# 网关持续有流量时，固定 conversation_archives 的 ID 高水位并校验该快照；
# 活跃 pending 和高水位后的新记录只计数，不会被误报为损坏
/conversation-archive -mode verify-online -root /data/conversation-archive -batch-size 100

# 停止网关写入后，恢复进程中断留下的 pending（捕获中断会标记 complete=false），并重建缺失索引
/conversation-archive -mode recover -root /data/conversation-archive

# 仅根据正式文件重建缺失的数据库小索引（不改正文）
/conversation-archive -mode reindex -root /data/conversation-archive

# 停止网关写入后，导出跨协议、逐条可恢复的 gzip JSONL；payloads_base64 保存原始字节
/conversation-archive -mode export -root /data/conversation-archive \
  -output /data/conversation-archive/exports/training-raw.jsonl.gz -batch-size 20
```

迁移记录使用稳定文件名，checkpoint 每完成一条才原子推进；重新运行会逐字节核对当前源行，既不会重复制造文件，也不会把源数据变化当作成功。所有归档工具命令由根目录维护锁串行化，每个 pending 另有跨进程排他锁；`recover` 遇到仍在写入的会话会拒绝处理，避免并发命令或活跃流互相覆盖、截断或误删。`verify-online` 适合生产持续写入期间的日常巡检：它先固定数据库 ID 高水位，再逐条恢复并核对该快照内 manifest、record、SHA-256 和所有引用 blob，同时报告开始校验时的活跃 pending 数量；它不会把高水位后的新记录或正在写入的 pending 当作损坏。严格的 `verify` 用于维护窗口，会额外要求 pending 清零，并检查没有索引的正式文件、未被任何 record 引用的 blob 和崩溃遗留临时文件；两种校验都只报告问题，不自动删除。导出会先执行同等全量严格校验，再固定数据库 ID 高水位并逐条复核，避免在线增长造成无界导出或索引漂移；输出只能写到归档根目录下的 `exports/`，通过不可覆盖的原子发布拒绝同名文件，并在每批数据前复核剩余容量。

流式客户端在上游响应结束前断开时，网关会保存已实际读取的全部字节，并把记录标记为 `complete=false`，`missing` 中写入 `*_response_not_fully_read` 和 `stream_client_gone`，元数据保留流结束原因、错误摘要和软错误数。关闭响应体会并发打断阻塞读取，不会因归档锁导致请求清理卡死。这不是静默损坏；训练清洗必须默认排除 `complete=false`，只有人工确认用途后才能纳入。

导出的 JSONL 只是协议中立、可追溯的原始语料层，不是可直接训练的数据集。后续训练流水线仍需做授权筛选、去隐私、去重、质量过滤、跨协议标准化，并按 `complete`、`missing`、用户授权状态、时间和模型等字段筛选。

## 生产释放空间门槛

只有同时满足以下条件，才允许在单独获批后清理 `conversation_logs`：

1. 固定高水位范围全部迁移，checkpoint 到达 high-water；
2. `verify` 全量通过，记录数与源范围核对一致；
3. 归档目录已经复制到第二个独立故障域并完成抽样恢复；
4. 新归档链路持续写入、查询索引、恢复记录和 `/api/status` 均通过；
5. 数据盘容量与增长告警已配置，不能把 100G 单盘视作长期唯一副本；
6. 当前操作回合再次取得明确的数据库清理授权。

历史 `.dump`、`.tar.gz` 等归档不属于自动清理范围，任何删除都必须单独批准。
