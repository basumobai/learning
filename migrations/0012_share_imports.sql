-- Reuse encrypted cloud credentials. Existing WebDAV connections keep working.
ALTER TABLE learning_cloud_connections ADD COLUMN adapter text NOT NULL DEFAULT 'webdav' CHECK(adapter IN ('webdav','openlist'));
ALTER TABLE learning_cloud_connections ADD COLUMN options jsonb NOT NULL DEFAULT '{}';
CREATE TABLE learning_shares (
 id text PRIMARY KEY,provider text NOT NULL CHECK(provider IN ('quark','baidu')),
 url text NOT NULL,link_hash text NOT NULL,passcode bytea,
 connection_id text NOT NULL REFERENCES learning_cloud_connections(id),mount_path text NOT NULL,
 owner_id text NOT NULL REFERENCES users(id),revision integer NOT NULL DEFAULT 1,
 status text NOT NULL DEFAULT 'pending',error text NOT NULL DEFAULT '',created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(link_hash,connection_id,mount_path)
);
CREATE TABLE learning_share_files (
 id text PRIMARY KEY,share_id text NOT NULL REFERENCES learning_shares(id),snapshot integer NOT NULL,
 path text NOT NULL,name text NOT NULL,size bigint NOT NULL CHECK(size>=0),version text NOT NULL,
 hash text,recommendation jsonb,document_id text REFERENCES learning_documents(id),
 UNIQUE(share_id,snapshot,path)
);
CREATE TABLE learning_share_imports (
 id text PRIMARY KEY,share_id text NOT NULL REFERENCES learning_shares(id),snapshot integer NOT NULL,
 owner_id text NOT NULL REFERENCES users(id),max_bytes bigint NOT NULL CHECK(max_bytes BETWEEN 1 AND 2147483648),
 bytes_used bigint NOT NULL DEFAULT 0,bytes_per_second integer NOT NULL CHECK(bytes_per_second BETWEEN 65536 AND 20971520),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE learning_share_import_files (
 import_id text NOT NULL REFERENCES learning_share_imports(id),file_id text NOT NULL REFERENCES learning_share_files(id),
 status text NOT NULL DEFAULT 'pending',document_id text REFERENCES learning_documents(id),error text NOT NULL DEFAULT '',
 PRIMARY KEY(import_id,file_id)
);
