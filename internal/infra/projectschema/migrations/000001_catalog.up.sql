-- 占位符 {{schema}} 由 Apply 替换为 quoteIdent(ProjectSchemaName(id))。
-- 禁止未限定表名。结构对齐 public catalog（000001+000003+000005+000009）。

CREATE TABLE IF NOT EXISTS {{schema}}.document_databases (
    id          TEXT NOT NULL,
    project_id  TEXT NOT NULL REFERENCES public.projects(id),
    name        TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (project_id, id),
    UNIQUE (project_id, name)
);

CREATE TABLE IF NOT EXISTS {{schema}}.document_collections (
    id                 TEXT NOT NULL,
    database_id        TEXT NOT NULL,
    project_id         TEXT NOT NULL,
    name               TEXT NOT NULL,
    document_security  BOOLEAN NOT NULL DEFAULT TRUE,
    permissions        TEXT[] DEFAULT '{}',
    disabled           BOOLEAN NOT NULL DEFAULT FALSE,
    is_system          BOOLEAN NOT NULL DEFAULT FALSE,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (project_id, database_id, id),
    UNIQUE (project_id, database_id, name),
    FOREIGN KEY (project_id, database_id)
        REFERENCES {{schema}}.document_databases (project_id, id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS {{schema}}.document_attributes (
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
    UNIQUE (project_id, database_id, collection_id, key),
    FOREIGN KEY (project_id, database_id, collection_id)
        REFERENCES {{schema}}.document_collections (project_id, database_id, id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS {{schema}}.document_indexes (
    id            TEXT NOT NULL,
    collection_id TEXT NOT NULL,
    database_id   TEXT NOT NULL,
    project_id    TEXT NOT NULL,
    type          TEXT NOT NULL,
    attributes    TEXT[] NOT NULL,
    orders        TEXT[] DEFAULT '{}',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (project_id, database_id, collection_id, id),
    FOREIGN KEY (project_id, database_id, collection_id)
        REFERENCES {{schema}}.document_collections (project_id, database_id, id) ON DELETE CASCADE
);
