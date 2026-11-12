-- 轮次状态列改型：字符串枚举换成 smallint 序号。
-- 已是 smallint 时跳过，重复执行不报错。
DO $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_schema = current_schema()
      AND table_name = 'convo_chat_turn' AND column_name = 'status'
      AND data_type = 'character varying'
  ) THEN
    ALTER TABLE convo_chat_turn ALTER COLUMN status TYPE smallint USING (CASE status
      WHEN 'queued'      THEN 1
      WHEN 'running'     THEN 2
      WHEN 'succeeded'   THEN 3
      WHEN 'failed'      THEN 4
      WHEN 'cancelled'   THEN 5
      WHEN 'interrupted' THEN 6
      ELSE 0
    END)::smallint;
  END IF;
END $$;

-- 会话归属回填：历史会话的 owner_id 恒为 0，用创建者补上，归属校验才有依据。
UPDATE convo_session SET owner_id = creator_id WHERE owner_id = 0 AND creator_id <> 0;
