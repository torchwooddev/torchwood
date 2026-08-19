DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name = 'document_events_outbox'
          AND column_name = 'channel'
    ) THEN
        ALTER TABLE document_events_outbox DROP COLUMN channel;
        ALTER TABLE document_events_outbox_dead DROP COLUMN channel;
    END IF;
END
$$;
