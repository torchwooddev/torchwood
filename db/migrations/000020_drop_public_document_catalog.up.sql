-- D-7：运行时 catalog 只在 tw_<project>；public 四张幽灵表无读路径。
-- 不碰 document_events_outbox / _dead、document_transactions / ops。
-- 子表先删，四表之间无外部 FK，无需 CASCADE。
DROP TABLE IF EXISTS public.document_indexes;
DROP TABLE IF EXISTS public.document_attributes;
DROP TABLE IF EXISTS public.document_collections;
DROP TABLE IF EXISTS public.document_databases;
