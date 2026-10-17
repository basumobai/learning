ALTER TABLE learning_documents ADD COLUMN duplicate_of text REFERENCES learning_documents(id) CHECK(duplicate_of<>id);
CREATE INDEX learning_document_duplicate ON learning_documents(duplicate_of) WHERE duplicate_of IS NOT NULL;
UPDATE learning_processing_config SET config=config||'{"share_tokens":200000,"share_seconds":300}';
