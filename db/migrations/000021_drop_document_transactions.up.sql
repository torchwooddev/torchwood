-- 子表有 FK，先 ops 后 transactions。
DROP TABLE IF EXISTS public.document_transaction_ops;
DROP TABLE IF EXISTS public.document_transactions;
