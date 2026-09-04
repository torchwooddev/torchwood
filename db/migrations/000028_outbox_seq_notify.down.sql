DROP TRIGGER IF EXISTS document_events_outbox_notify ON document_events_outbox;
DROP FUNCTION IF EXISTS tw_outbox_notify();

DROP INDEX IF EXISTS document_events_outbox_seq_key;

ALTER TABLE document_events_outbox
    DROP COLUMN IF EXISTS seq;
