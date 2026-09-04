DROP INDEX IF EXISTS document_events_outbox_seq_key;

ALTER TABLE document_events_outbox
    DROP COLUMN IF EXISTS seq;
