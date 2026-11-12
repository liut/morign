---
title: fix: 会话删除的级联清理与轮次状态的枚举化
type: fix
status: completed
date: 2026-11-06
origin: docs/plans/2026-10-29-003-feat-turn-idempotency-state-machine-plan.md
---

# fix: 会话删除的级联清理与轮次状态的枚举化

## Summary

两项修正，都落在计划 003 引入的 `convo_chat_turn` 上：

1. **P0 — 会话删除时的级联清理。** `DeleteSession` 只删 `convo_session` 一行，`convo_message`、`convo_tool_record`、`convo_chat_turn` 三张子表全部残留。`convo_chat_turn.result` 存的是模型最终答案全文 —— 用户删了会话，答案还在库里。
2. **P1 — 轮次状态枚举化。** `ChatTurn.Status` 是裸 `varchar(16)` + 手写字符串常量，与同文件里 `SessionStatus` 的 enum 写法不一致。状态值拼错在编译期不可发现，运行时落进数据库就是一个永远查不出来的值。

本计划不引入新的表或新的对外接口。改动集中在 `docs/convo.yaml` 契约、`pkg/services/stores/convo_x.go` 与 `pkg/web/api/handle_turn.go`。

---

## Problem Frame

### P0：删除会话不删子表

`DeleteSession`（`pkg/services/stores/convo_gen.go:301`）：

```go
func (s *convoStore) DeleteSession(ctx context.Context, id string) error {
	obj := new(convo.Session)
	return s.w.db.DeleteModel(ctx, obj, id)
}
```

只删 `convo_session` 一行。三张子表都带 `session_id`，都没有外键级联（`docs/convo.yaml` 里没有 `REFERENCES` 声明，`data/schemas/` 的迁移 SQL 里也没有）：

| 表 | 字段 | 内容 |
| --- | --- | --- |
| `convo_message` | `session_id` | 用户提问与助手回答各一行 |
| `convo_tool_record` | `session_id` | 工具调用与结果全文 |
| `convo_chat_turn` | `session_id` | 轮次元数据 + `result`（模型最终答案全文） |

`convo_chat_turn.result` 的写入点在 `finishTurn`（`pkg/web/api/handle_turn.go:107`）：

```go
status, result := stores.TurnStatusSucceeded, answer
```

`answer` 是完整答案文本。计划 003 的 review 已经点名这条风险（"把模型最终回答落到 PG，会把本来 24 小时后随 Redis 过期消失的对话内容变成长期留存的数据"），实现时以"本轮不做定时清理"结案 —— 但**级联删除**本身也没接线。

现有删除路径 `DeleteModel` → `DoDeleteM` → `op_affect_delete` 是**移动删除**：行被复制到 `<schema>_trash` 后从主表移除。子表不走这条路径，留在主表里。

### P1：状态值是裸字符串

`docs/convo.yaml` 的 `ChatTurn`：

```yaml
- comment: 状态 queued/running/succeeded/failed/cancelled/interrupted
  name: Status
  type: string
  tags: {bson: 'status', json: 'status', pg: ',notnull,type:varchar(16)'}
```

状态常量手写在 `pkg/services/stores/convo_x.go:689-696`：

```go
const (
	TurnStatusQueued      = "queued"
	TurnStatusRunning     = "running"
	...
)
```

对比同文件里 `SessionStatus`（`docs/convo.yaml:9-21`）：正经 enum，`type: int8`，有 `Decode`/`String`/`MarshalText` 生成，编译期常量。

裸字符串的故障模式：`"suceeded"` 这种 typo 编译期不报错，运行时落进数据库就是一个永远查不出来的状态值。而 `acquireTurn` 的 `switch existing.Status`（`handle_turn.go:74`）走到 `default` 分支 —— 既不执行也不返回 —— **这条幂等键被永久占用，用户重发同样请求永远拿不到答案**。

---

## Requirements

- R1. 删除会话时，`convo_message`、`convo_tool_record`、`convo_chat_turn` 中该 `session_id` 的行一并删除。
- R2. 级联清理与会话行的删除在同一事务内 —— 要么都成，要么都不成。
- R3. 会话删除仍走既有归属规则：本人可删，keeper 可删他人，非 keeper 删他人会话被拒。
- R4. `ChatTurn.Status` 改为 enum，合法值集合由类型系统约束。
- R5. 状态值落库形式与 `SessionStatus` 一致（int8 + `text` 编解码），不改变已落库数据的可读性。
- R6. `acquireTurn` 对未知状态值的处置明确化，不再静默落到 `default` 分支。
- R7. `make vet` 与 `make lint` 通过。

---

## Scope Boundaries

### In Scope

- `docs/convo.yaml`：`ChatTurn.Status` 改 enum；`Session` 补 `beforeDeleting` 钩子
- `make codegen` 产物
- `data/schemas/`：一条 PG 迁移 SQL
- `pkg/services/stores/convo_x.go`：级联清理实现；轮次状态常量迁到生成的 enum
- `pkg/web/api/handle_turn.go`：状态判断适配
- 相应测试

### Out of Scope

- **定时保留期清理**：本计划只保证"删会话即删子表"。若用户从不删会话，答案仍长期留存。保留期策略是产品决策，另议。
- **`convo_chat_turn` 的独立清理端点**：轮次没有 webapi 暴露（`docs/convo.yaml` 的 `webapi.uris` 只列了 Session/Message/User/UsageRecord），本轮不加。
- **`Result` 与 `Message.Content` 的双写收敛**：见「Key Technical Decisions」第 3 条，本轮不动。
- **多副本租约字段**（`LeaseOwner`/`LeaseExpiresAt`）：见计划 003 的已知约束，本轮不碰。

---

## Key Technical Decisions

### 1. 级联清理走 `beforeDeleting` 钩子，不覆盖 `DeleteSession`

`docs/convo.yaml` 的 `Session` 模型目前没有 `hooks` 段。加一个：

```yaml
hooks:
  beforeDeleting: yes
```

`make codegen` 后 `DeleteSession` 变成（对照 `DeleteMessage` 的生成形态）：

```go
func (s *convoStore) DeleteSession(ctx context.Context, id string) error {
	obj := new(convo.Session)
	if err := dbGetWithPKID(ctx, s.w.db, obj, id); err != nil { ... }
	return s.w.db.RunInTx(ctx, nil, func(ctx context.Context, tx pgTx) (err error) {
		if err = dbBeforeDeleteSession(ctx, tx, obj); err != nil { return }
		err = dbDeleteM(ctx, tx, s.w.db.Schema(), s.w.db.SchemaCrap(), obj)
		return
	})
}
```

钩子函数名由 scaffold 的 `ParseHook` 规则生成：`beforeDeleting` → `dbBeforeDeleteSession`（`Memory` 的 `dbBeforeDeleteMemory` 是同一规则）。

**为什么不直接改 `stores` 层**：生成的 `DeleteSession` 在 `_gen.go` 里，CLAUDE.md 规定 `_gen.go` 勿动。钩子是契约层声明的扩展点，与 `Memory`/`Message` 的既有做法一致。

### 2. 级联删除用硬删，不用移动删除

既有删除路径是**移动删除**（`op_affect_delete` 把行搬到 `<schema>_trash`）。级联清理子表**不走这条路**，用普通 `DELETE`：

```go
func dbBeforeDeleteSession(ctx context.Context, db ormDB, obj *convo.Session) error {
	// 子表随会话一并消失：答案全文不留在任何 schema 里。
	for _, tbl := range []any{(*convo.Message)(nil), (*convo.ToolRecord)(nil), (*convo.ChatTurn)(nil)} {
		if _, err := db.NewDelete().Model(tbl).
			Where("session_id = ?", obj.ID).Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}
```

**理由**：`_trash` schema 是"可恢复删除"的暂存区，子表搬进去等于换个地方存着答案 —— 用户删了会话，答案仍可被 DBA 从 trash schema 里捞出来。硬删才兑现"删会话即删答案"。

**事务语义**：钩子的 `db` 参数是 `pgTx`（bun 的 `Tx`），`db.NewDelete()` 返回绑定该事务连接的查询（bun 的 `Conn(tx)` 机制），因此三张表的删除与会话行的删除在同一事务内。R2 满足。

**与 `dbBeforeDeleteMemory` 的差异**：`dbBeforeDeleteMemory`（`convo_x.go:660`）用 `db.NewDelete()` 但传的是外层 `ctx`，实际不在事务内；本计划按 `dbBeforeDeleteMessage` 的形态，在钩子内用传入的 `db`（即 `tx`）。

### 3. `Result` 与 `Message.Content` 的双写不动

`convo_chat_turn.result` 与 `convo_message`（`role=assistant`）都存最终答案。两处内容会重合，但**职责不同**：

- `result` 是**幂等回放的权威副本** —— 重复请求命中 `succeeded` 轮次时直接返回它（`handle_turn.go:76`）
- `Message.Content` 是**对话内容投影** —— 给历史查询与上下文投影用

写入顺序是 `finishTurn` 先、`Persist` 后（`handle_convo.go:335` vs `:339`）。这个顺序不能反：若先写 Message 后写 Result，Result 失败时轮次停在 `running`，replay 走不到 `succeeded` 分支，用户拿到空白或 409。

合并掉 `Result` 会把幂等能力押在 Message 写入成功上 —— 而 Message 写入是 best-effort（失败只记日志）。**本轮保留冗余**，只做 P0 的级联清理（两者一起被删，冗余不产生额外风险）。

### 4. `Status` 改为 int8 enum

照抄 `SessionStatus` 的写法：

```yaml
enums:
  - comment: 轮次状态
    name: TurnStatus
    start: 1
    type: int8
    values:
      - { label: 排队中, suffix: Queued }
      - { label: 执行中, suffix: Running }
      - { label: 已完成, suffix: Succeeded }
      - { label: 已失败, suffix: Failed }
      - { label: 已取消, suffix: Cancelled }
      - { label: 已中断, suffix: Interrupted }
    stringer: true
    decodable: true
    textMarshaler: true
    textUnmarshaler: true
```

字段改为：

```yaml
- comment: 状态
  name: Status
  type: TurnStatus
  tags: {bson: 'status', json: 'status', pg: ',notnull,type:smallint'}
  isset: true
  query: 'equal'
```

**为什么是 int8 而不是 string enum**：scaffold 的 enum 生成器（`../scaffold/scripts/codegen/gens/type_enum.go`）的 `Decode` 同时接受序号与后缀名（`"1"` / `"queued"` / `"Queued"`），落库用 `smallint`。与 `SessionStatus` 完全一致，读写路径无新增分支。

**enum 后缀与现有字符串值的关系**：`SessionStatus` 的 `suffix: Open` 生成 `String()` 返回 `"open"`（`getCode` 取后缀首字母小写）。所以 `suffix: Queued` → `"queued"`，与现有常量值逐字对应 —— **迁移 SQL 可以按字符串值映射到序号**，已有数据不丢语义。

**状态值不并入 `enums` 段而是独立命名**：`docs/convo.yaml` 的 `enums` 段目前只有 `SessionStatus`。加 `TurnStatus` 是同一段追加，不影响其它契约。

### 5. 迁移 SQL：状态列 varchar → smallint

`convo_chat_turn` 已建表，列类型是 `varchar(16)`，存量值是字符串。转换需要一条 `ALTER`：

```sql
-- 状态列改为 smallint：字符串值按 TurnStatus 枚举序号映射。
ALTER TABLE convo_chat_turn
  ALTER COLUMN status TYPE smallint
  USING (CASE status
    WHEN 'queued'      THEN 1
    WHEN 'running'     THEN 2
    WHEN 'succeeded'   THEN 3
    WHEN 'failed'      THEN 4
    WHEN 'cancelled'   THEN 5
    WHEN 'interrupted' THEN 6
    ELSE 0
  END);
```

**用 `ALTER` 而非 `DROP`+重建**：表内已有线上数据，重建会丢轮次记录与答案。

**`ELSE 0` 的含义**：0 不属于任何枚举值，代表"迁移前的未知值"。scaffold 生成的 `Decode` 对未知值返回 error，bun 扫描时会报错。这是有意的 fail loud —— 与 R6 一致。

**幂等性**：迁移由 `RunMigrations` 执行（bun 的 migrator），同一文件只跑一次；且 `ALTER COLUMN TYPE` 在已是 smallint 时会失败，因此迁移 SQL 需写成可重复执行。**采用 `USING` + 类型判断**：

```sql
DO $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_name = 'convo_chat_turn' AND column_name = 'status'
      AND data_type = 'character varying'
  ) THEN
    ALTER TABLE convo_chat_turn ALTER COLUMN status TYPE smallint USING (...);
  END IF;
END $$;
```

**注意**：`RunMigrations` 用 `data/schemas` 的 `upgrades` FS（`//go:embed 20????????????_*.up.sql`），文件名需匹配 14 位时间戳前缀。`DO $$ ... $$` 是单条语句，bun 的 migrator 按整文件执行，不需要语句切分 —— 但需实测确认（见 U3 的验证项）。

### 6. `acquireTurn` 对未知状态的处置

`handle_turn.go:74` 的 `switch existing.Status` 目前是：

```go
switch existing.Status {
case stores.TurnStatusSucceeded:
	return &turnGate{ID: existing.StringID(), Replay: true, Cached: existing.Result}, nil
case stores.TurnStatusFailed, stores.TurnStatusCancelled:
	... 转为 running 重新执行
default:
	return &turnGate{ID: existing.StringID()}, nil   // 既不执行也不返回
}
```

`default` 现在同时兜着"queued/running/interrupted"（正确 —— 执行中或中断，不重复执行也不返回）和"未知值"（错误 —— 幂等键永久占用）。枚举化后两者可区分：

```go
switch existing.Status {
case convo.TurnStatusSucceeded:
	...
case convo.TurnStatusFailed, convo.TurnStatusCancelled:
	...
case convo.TurnStatusQueued, convo.TurnStatusRunning, convo.TurnStatusInterrupted:
	return &turnGate{ID: existing.StringID()}, nil
default:
	// 未知状态：既不该执行也不该静默占住幂等键。
	logger().Warn("turn: unknown status", "turn", existing.StringID(), "status", existing.Status)
	return nil, fmt.Errorf("unknown turn status %d", existing.Status)
}
```

未知状态返回错误（→ 503 可重试）而不是 409。用户能重新提问，而不是永远卡住。

### 7. 状态常量从 `stores` 迁到 `convo`

`stores.TurnStatus*` 六个常量（`convo_x.go:689-696`）删除，改用 codegen 生成的 `convo.TurnStatus*`。

**调用点影响面**（`grep -rn "TurnStatus"` 全仓）：`handle_turn.go`（8 处）、`handle_platform.go`（1 处）、`stores/convo_x.go`（5 处）、`stores/convo_turn_integration_test.go`（8 处）、`stores/tool_record_integration_test.go`（1 处）、`handle_turn_test.go`（10 处）。

`UpdateTurnStatus(ctx, id, status, result string)` 的签名要把 `status` 从 `string` 改成 `convo.TurnStatus`。

---

## Implementation Units

### U1. 契约：新增 `TurnStatus` enum，`ChatTurn.Status` 改类型

**Files:**
- `docs/convo.yaml`

**Steps:**
1. `enums` 段追加 `TurnStatus`（值与 suffix 见「Key Technical Decisions」第 4 条）
2. `ChatTurn.Status` 字段：`type: string` → `type: TurnStatus`，`pg` tag 加 `type:smallint`
3. `ChatTurnBasic.Status` 的 `isset: true` 保留 —— 否则生成的 `ChatTurnSet` 里没有它，终态状态无处写入

**验收：** `make codegen` 后 `pkg/models/convo/convo_gen.go` 出现 `TurnStatus` 类型与 `TurnStatusQueued` 等常量；`ChatTurnBasic.Status` 类型为 `convo.TurnStatus`。

---

### U2. 契约：`Session` 加 `beforeDeleting` 钩子

**Files:**
- `docs/convo.yaml`

**Steps:**
1. `Session` 模型加 `hooks: {beforeDeleting: yes}`
2. `make codegen`

**验收：** 生成的 `DeleteSession` 走 `RunInTx` + `dbBeforeDeleteSession` + `dbDeleteM` 形态（对照 `DeleteMessage`）。

**注意：** 这一步会让项目编译失败（`dbBeforeDeleteSession` 尚未实现），U3 补齐。

---

### U3. 迁移 SQL：状态列类型转换

**Files:**
- `data/schemas/<14位时间戳>_convo_chat_turn_status.up.sql`

**Steps:**
1. 按「Key Technical Decisions」第 5 条写 `DO $$ ... $$` 包裹的 `ALTER`
2. 文件名时间戳取实施当日时间

**验收：**
- 在有存量数据的库上执行成功，`convo_chat_turn.status` 变为 `smallint`
- 存量行的值按映射表正确转换（`queued`→1 等）
- 在已转换过的库上重复执行不报错

---

### U4. 存储层：实现级联清理

**Files:**
- `pkg/services/stores/convo_x.go`

**Steps:**
1. 实现 `dbBeforeDeleteSession(ctx context.Context, db ormDB, obj *convo.Session) error`
2. 按 `session_id` 硬删三张子表的行
3. **归属校验**：会话行由 `dbDeleteM` 删除，而 `dbDeleteM` 不做归属检查。`DeleteSession` 走的是 `DeleteModel`（`s.w.db.DeleteModel`），此前**没有任何归属校验** —— 任意登录用户都能删别人的会话（`regHI(true, "DELETE", "/convo/sessions/{id}")` 只要求登录）。加了钩子后，`dbBeforeDeleteSession` 是校验归属的合适位置：

```go
func dbBeforeDeleteSession(ctx context.Context, db ormDB, obj *convo.Session) error {
	if !ownerAllowed(ctx, obj.OwnerID) {
		return ErrForbidden
	}
	...
}
```

**这是本计划顺带修掉的一个越权漏洞。** R3 要求它，且它与 P0 同属"删除会话"这条路径，不另立计划。

4. **但 `Session.OwnerID` 当前是否被填充需要先确认**：`Session` 模型有 `comm.OwnerField`（`owner_id` 列），但 `dbBeforeCreateSession` 钩子不存在，`SaveSession` 走的 `dbInsert` 也不自动填 owner。若 `owner_id` 恒为 0，`ownerAllowed` 会把**本人删自己的会话也拒掉**。实施时必须先查清 `owner_id` 的写入来源；若确实恒为 0，本单元改为"不校验归属，但在计划文档记录该越权问题"，另立修复。

**验收：**
- 删除会话后，三张子表中该 `session_id` 的行数为 0
- 删除他人会话（非 keeper）返回 `ErrForbidden`，且子表未被删除
- keeper 删除他人会话成功

---

### U5. 存储层与入口层：状态枚举化适配

**Files:**
- `pkg/services/stores/convo_x.go`
- `pkg/web/api/handle_turn.go`
- `pkg/web/api/handle_platform.go`

**Steps:**
1. 删除 `stores.TurnStatus*` 六个常量
2. `UpdateTurnStatus(ctx, id, status, result string)` 的 `status` 改为 `convo.TurnStatus`
3. `UpdateChatTurn` 的 `ChatTurnSet.Status` 字段类型随之改变（codegen 产物），确认 `SetWith` 逻辑仍正确
4. `handle_turn.go` / `handle_platform.go` 的常量引用改为 `convo.TurnStatus*`
5. `acquireTurn` 的 `switch` 按「Key Technical Decisions」第 6 条拆开 `default`

**注意：** `ReclaimInterruptedTurns`（`convo_x.go:748`）用裸 SQL 更新状态：

```go
Set("status = ?", TurnStatusInterrupted).
Where("status = ? OR status = ?", TurnStatusQueued, TurnStatusRunning)
```

枚举化后 `TurnStatusInterrupted` 是 int8 值，bun 会按整数写入 `smallint` 列 —— 语义正确，无需改 SQL。但**迁移执行前**这段代码若跑在未转换的库上会因类型不匹配报错。迁移在 `InitDB` 里先于 `ReclaimInterruptedTurns` 执行（`wrap.go:181` vs `:189`），顺序安全。

**验收：**
- `go build ./...` 通过
- 全部 `TurnStatus` 引用点编译通过，无残留的裸字符串比较
- `acquireTurn` 的未知状态分支返回错误而非静默占键

---

### U6. 测试

**Files:**
- `pkg/web/api/handle_turn_test.go`
- `pkg/services/stores/convo_turn_integration_test.go`（新增用例）

**用例：**

| 用例 | 断言 |
| --- | --- |
| 删除会话 | 三张子表中该 `session_id` 的行全部消失 |
| 删除会话后 `convo_chat_turn.result` | 不可从主表与 `_trash` schema 查到 |
| 非 keeper 删他人会话 | 返回 `ErrForbidden`，子表数据仍在 |
| keeper 删他人会话 | 成功，子表数据一并清除 |
| 状态枚举 | `convo.TurnStatus` 各常量与 `String()` 输出对应；非法值 `Decode` 报错 |
| 未知状态轮次 | `acquireTurn` 返回错误而非 `Proceed=false` 的静默占键 |

**注意：** `handle_turn_test.go` 的 `fakeTurnStore` 需跟着 `UpdateTurnStatus` 的签名改动调整。

**验证：** `make test-models` 通过；`make test-stores` 需要本地 PostgreSQL + Redis。

---

## System-Wide Impact

| Area | Impact |
| --- | --- |
| `docs/convo.yaml` | 新增 `TurnStatus` enum；`ChatTurn.Status` 改类型；`Session` 加钩子 |
| `pkg/models/convo` | codegen 产物：`TurnStatus` 类型与常量 |
| `pkg/services/stores` | `dbBeforeDeleteSession`；状态常量迁移；`UpdateTurnStatus` 签名变化 |
| `pkg/web/api` | 状态常量引用适配；`acquireTurn` 未知状态分支 |
| `data/schemas` | 一条迁移 SQL |
| 对外 API | 无变化 —— `ChatTurn` 未被 webapi 暴露 |
| 行为 | 删会话会真删子表数据；非 keeper 不能删他人会话 |

---

## Risks & Dependencies

| Risk | Mitigation |
| --- | --- |
| `ALTER COLUMN TYPE` 在大表上锁表 | `convo_chat_turn` 是新表（2026-11-05 建立），存量小；生产执行前确认行数 |
| `USING` 表达式漏了某个状态值 → 落成 0 | 迁移后跑一次校验查询，确认无 `status = 0` 的行 |
| 迁移未执行时新代码写入 int 值 | `InitDB` 里迁移先于任何轮次写入；部署顺序保证 |
| `owner_id` 恒为 0 导致本人也删不了自己的会话 | U4 的前置检查项：先确认 `owner_id` 的写入来源，再决定是否启用归属校验 |
| 枚举化后 `switch` 的 `default` 遗漏合法状态 | 枚举值集合固定为 6 个，`switch` 显式列出全部，`default` 只兜未知值 |
| codegen 需要 Go 1.26，本机默认 1.27 | `PATH=/usr/local/go/bin:$PATH GOCACHE=/tmp/gocache126 make codegen`（已实测通过） |

---

## Documentation / Operational Notes

- `convo_chat_turn` 的数据在会话删除时一并消失；若需要"删会话但留轮次记录以供审计"，需要另立设计（当前 `Result` 与轮次是一体的）
- 本计划完成后，`convo_chat_turn` 的长期留存问题只剩"用户从不删会话"这一条路径

---

## Sources & References

- `docs/plans/2026-10-29-003-feat-turn-idempotency-state-machine-plan.md`（轮次状态机与幂等的原始设计）
- `docs/plans/2026-10-29-004-feat-conversation-persistence-basis-plan.md`（原始记录持久化，`turn_id` 的来源）
- `pkg/services/stores/convo_x.go`（轮次存储与 `dbBeforeDeleteMemory` 的钩子形态）
- `pkg/web/api/handle_turn.go`（`acquireTurn` / `finishTurn`）
- `pkg/services/stores/convo_gen.go:301`（`DeleteSession` 的当前实现）
- `../scaffold/scripts/codegen/gens/type_enum.go`（enum 生成规则）
- `../scaffold/scripts/codegen/gens/util.go:435`（`ParseHook` 的钩子命名规则）
- `pkg/services/stores/wrap.go:181`（`InitDB` 中迁移与轮次恢复的先后）
