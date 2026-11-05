-- 原始消息补齐轮次与归属：老消息没有这两列，按 0 兜底。
ALTER TABLE convo_message ADD COLUMN IF NOT EXISTS turn_id bigint NOT NULL DEFAULT 0;
ALTER TABLE convo_message ADD COLUMN IF NOT EXISTS owner_id bigint NOT NULL DEFAULT 0;
CREATE INDEX IF NOT EXISTS idx_message_turn ON convo_message (turn_id);
CREATE INDEX IF NOT EXISTS idx_message_owner ON convo_message (owner_id);
