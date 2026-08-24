-- 反向恢复分两阶段：
-- Phase 1 先子后父拆除复合约束（否则父表复合主键因被子表复合外键引用无法 DROP，SQLSTATE 2BP01）；
-- Phase 2 先父后子恢复单列形态（子表外键引用父表单列 id，须待父表主键就位，SQLSTATE 42830）。

-- ===== Phase 1: 拆除复合键（deepest child → parent）=====
ALTER TABLE document_indexes DROP CONSTRAINT IF EXISTS document_indexes_collection_fkey;
ALTER TABLE document_indexes DROP CONSTRAINT document_indexes_pkey;

ALTER TABLE document_attributes DROP CONSTRAINT IF EXISTS document_attributes_collection_fkey;
ALTER TABLE document_attributes DROP CONSTRAINT IF EXISTS document_attributes_collection_key_key;
ALTER TABLE document_attributes DROP CONSTRAINT document_attributes_pkey;

ALTER TABLE document_collections DROP CONSTRAINT IF EXISTS document_collections_database_fkey;
ALTER TABLE document_collections DROP CONSTRAINT IF EXISTS document_collections_project_database_name_key;
ALTER TABLE document_collections DROP CONSTRAINT document_collections_pkey;

ALTER TABLE document_databases DROP CONSTRAINT IF EXISTS document_databases_project_id_fkey;
ALTER TABLE document_databases DROP CONSTRAINT document_databases_pkey;

-- ===== Phase 2: 恢复单列主键/唯一键/外键（parent → child）=====
ALTER TABLE document_databases ADD PRIMARY KEY (id);

ALTER TABLE document_collections ADD PRIMARY KEY (id);
ALTER TABLE document_collections ADD CONSTRAINT document_collections_database_id_name_key UNIQUE (database_id, name);
ALTER TABLE document_collections ADD CONSTRAINT document_collections_database_id_fkey
    FOREIGN KEY (database_id) REFERENCES document_databases (id) ON DELETE CASCADE;

ALTER TABLE document_attributes DROP COLUMN IF EXISTS project_id;
ALTER TABLE document_attributes DROP COLUMN IF EXISTS database_id;
ALTER TABLE document_attributes ADD PRIMARY KEY (id);
ALTER TABLE document_attributes ADD CONSTRAINT document_attributes_collection_id_key_key UNIQUE (collection_id, key);
ALTER TABLE document_attributes ADD CONSTRAINT document_attributes_collection_id_fkey
    FOREIGN KEY (collection_id) REFERENCES document_collections (id) ON DELETE CASCADE;

ALTER TABLE document_indexes DROP COLUMN IF EXISTS project_id;
ALTER TABLE document_indexes DROP COLUMN IF EXISTS database_id;
ALTER TABLE document_indexes ADD PRIMARY KEY (id);
ALTER TABLE document_indexes ADD CONSTRAINT document_indexes_collection_id_fkey
    FOREIGN KEY (collection_id) REFERENCES document_collections (id) ON DELETE CASCADE;
