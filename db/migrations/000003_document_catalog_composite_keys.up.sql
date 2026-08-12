-- Catalog metadata IDs are scoped per project; switch to composite primary keys.
-- Drop dependent FKs first (deepest child → parent).

-- document_indexes: add scope columns before collection PK changes
ALTER TABLE document_indexes ADD COLUMN IF NOT EXISTS project_id TEXT;
ALTER TABLE document_indexes ADD COLUMN IF NOT EXISTS database_id TEXT;

UPDATE document_indexes di
SET project_id = dc.project_id,
    database_id = dc.database_id
FROM document_collections dc
WHERE di.collection_id = dc.id
  AND di.project_id IS NULL;

DELETE FROM document_indexes WHERE project_id IS NULL OR database_id IS NULL;

ALTER TABLE document_indexes DROP CONSTRAINT IF EXISTS document_indexes_collection_id_fkey;
ALTER TABLE document_indexes DROP CONSTRAINT IF EXISTS document_indexes_pkey;

-- document_attributes: add scope columns
ALTER TABLE document_attributes ADD COLUMN IF NOT EXISTS project_id TEXT;
ALTER TABLE document_attributes ADD COLUMN IF NOT EXISTS database_id TEXT;

UPDATE document_attributes da
SET project_id = dc.project_id,
    database_id = dc.database_id
FROM document_collections dc
WHERE da.collection_id = dc.id
  AND da.project_id IS NULL;

DELETE FROM document_attributes WHERE project_id IS NULL OR database_id IS NULL;

ALTER TABLE document_attributes DROP CONSTRAINT IF EXISTS document_attributes_collection_id_fkey;
ALTER TABLE document_attributes DROP CONSTRAINT IF EXISTS document_attributes_collection_id_key_key;
ALTER TABLE document_attributes DROP CONSTRAINT IF EXISTS document_attributes_pkey;

-- document_collections: drop FK to document_databases before changing parent PK
ALTER TABLE document_collections DROP CONSTRAINT IF EXISTS document_collections_database_id_fkey;
ALTER TABLE document_collections DROP CONSTRAINT IF EXISTS document_collections_pkey;
ALTER TABLE document_collections DROP CONSTRAINT IF EXISTS document_collections_database_id_name_key;

-- document_databases: composite PK (project_id, id)
ALTER TABLE document_databases DROP CONSTRAINT IF EXISTS document_databases_pkey;
ALTER TABLE document_databases ADD PRIMARY KEY (project_id, id);

-- document_collections: composite PK + composite FK
ALTER TABLE document_collections ADD PRIMARY KEY (project_id, database_id, id);
ALTER TABLE document_collections ADD CONSTRAINT document_collections_project_database_name_key UNIQUE (project_id, database_id, name);
ALTER TABLE document_collections ADD CONSTRAINT document_collections_database_fkey
    FOREIGN KEY (project_id, database_id) REFERENCES document_databases (project_id, id) ON DELETE CASCADE;

-- document_attributes: composite PK + composite FK
ALTER TABLE document_attributes ALTER COLUMN project_id SET NOT NULL;
ALTER TABLE document_attributes ALTER COLUMN database_id SET NOT NULL;
ALTER TABLE document_attributes ADD PRIMARY KEY (project_id, database_id, collection_id, id);
ALTER TABLE document_attributes ADD CONSTRAINT document_attributes_collection_key_key UNIQUE (project_id, database_id, collection_id, key);
ALTER TABLE document_attributes ADD CONSTRAINT document_attributes_collection_fkey
    FOREIGN KEY (project_id, database_id, collection_id)
    REFERENCES document_collections (project_id, database_id, id) ON DELETE CASCADE;

-- document_indexes: composite PK + composite FK
ALTER TABLE document_indexes ALTER COLUMN project_id SET NOT NULL;
ALTER TABLE document_indexes ALTER COLUMN database_id SET NOT NULL;
ALTER TABLE document_indexes ADD PRIMARY KEY (project_id, database_id, collection_id, id);
ALTER TABLE document_indexes ADD CONSTRAINT document_indexes_collection_fkey
    FOREIGN KEY (project_id, database_id, collection_id)
    REFERENCES document_collections (project_id, database_id, id) ON DELETE CASCADE;
