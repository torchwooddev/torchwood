-- v3 经济事件复用 transactional outbox 同表同管道（设计 §5.1）：
-- 经济事件频道不挂在 database/collection 上，channel 直接落列（显式频道，
-- D17 单一 accounts.{userId}）；v2 文档事件 channel 为 NULL，行为不变。
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name = 'document_events_outbox'
          AND column_name = 'channel'
    ) THEN
        ALTER TABLE document_events_outbox ADD COLUMN channel TEXT;
        ALTER TABLE document_events_outbox_dead ADD COLUMN channel TEXT;
    END IF;
END
$$;
