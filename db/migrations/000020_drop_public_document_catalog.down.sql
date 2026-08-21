-- 重建空表，形状对齐 000001+000003+000005+000009；不恢复数据。
CREATE TABLE public.document_databases (
    id          TEXT NOT NULL,
    project_id  TEXT NOT NULL REFERENCES public.projects(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (project_id, id),
    UNIQUE (project_id, name)
);

CREATE TABLE public.document_collections (
    id                 TEXT NOT NULL,
    database_id        TEXT NOT NULL,
    project_id         TEXT NOT NULL REFERENCES public.projects(id) ON DELETE CASCADE,
    name               TEXT NOT NULL,
    document_security  BOOLEAN NOT NULL DEFAULT TRUE,
    permissions        TEXT[] DEFAULT '{}',
    disabled           BOOLEAN NOT NULL DEFAULT FALSE,
    is_system          BOOLEAN NOT NULL DEFAULT FALSE,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (project_id, database_id, id),
    CONSTRAINT document_collections_project_database_name_key UNIQUE (project_id, database_id, name),
    CONSTRAINT document_collections_database_fkey
        FOREIGN KEY (project_id, database_id)
        REFERENCES public.document_databases (project_id, id) ON DELETE CASCADE
);
CREATE INDEX idx_document_collections_project ON public.document_collections(project_id);

CREATE TABLE public.document_attributes (
    id            TEXT NOT NULL,
    collection_id TEXT NOT NULL,
    database_id   TEXT NOT NULL,
    project_id    TEXT NOT NULL,
    key           TEXT NOT NULL,
    type          TEXT NOT NULL,
    size          INT,
    required      BOOLEAN NOT NULL DEFAULT FALSE,
    is_array      BOOLEAN NOT NULL DEFAULT FALSE,
    default_value TEXT,
    options       JSONB DEFAULT '{}',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (project_id, database_id, collection_id, id),
    CONSTRAINT document_attributes_collection_key_key UNIQUE (project_id, database_id, collection_id, key),
    CONSTRAINT document_attributes_collection_fkey
        FOREIGN KEY (project_id, database_id, collection_id)
        REFERENCES public.document_collections (project_id, database_id, id) ON DELETE CASCADE
);

CREATE TABLE public.document_indexes (
    id            TEXT NOT NULL,
    collection_id TEXT NOT NULL,
    database_id   TEXT NOT NULL,
    project_id    TEXT NOT NULL,
    type          TEXT NOT NULL,
    attributes    TEXT[] NOT NULL,
    orders        TEXT[] DEFAULT '{}',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (project_id, database_id, collection_id, id),
    CONSTRAINT document_indexes_collection_fkey
        FOREIGN KEY (project_id, database_id, collection_id)
        REFERENCES public.document_collections (project_id, database_id, id) ON DELETE CASCADE
);
