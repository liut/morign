-- 幂等键唯一索引：同键并发插入只有一个成功，其余转为「已有轮次」。
CREATE UNIQUE INDEX IF NOT EXISTS uk_turn_idem_key ON convo_chat_turn (idempotency_key);
