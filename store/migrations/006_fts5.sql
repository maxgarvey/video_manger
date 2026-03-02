-- Full-text search index using the trigram tokenizer so MATCH behaves like
-- LIKE '%query%' but with O(1) lookup instead of a full-table scan.
-- Requires SQLite 3.34+ (bundled by modernc.org/sqlite).
--
-- content=videos: FTS5 stores only the index; content is read from the videos
-- table on query. This also enables the 'delete' auxiliary command that is
-- required by the update/delete triggers.
CREATE VIRTUAL TABLE IF NOT EXISTS videos_fts USING fts5(
    display_name,
    filename,
    content=videos,
    content_rowid=id,
    tokenize = 'trigram'
);

-- Populate the index from existing rows.
INSERT INTO videos_fts(rowid, display_name, filename)
    SELECT id, COALESCE(display_name, ''), filename FROM videos;

-- Keep the index in sync with the videos table.
CREATE TRIGGER IF NOT EXISTS videos_fts_ai AFTER INSERT ON videos BEGIN
    INSERT INTO videos_fts(rowid, display_name, filename)
        VALUES (new.id, COALESCE(new.display_name, ''), new.filename);
END;

CREATE TRIGGER IF NOT EXISTS videos_fts_au AFTER UPDATE ON videos BEGIN
    INSERT INTO videos_fts(videos_fts, rowid, display_name, filename)
        VALUES ('delete', old.id, COALESCE(old.display_name, ''), old.filename);
    INSERT INTO videos_fts(rowid, display_name, filename)
        VALUES (new.id, COALESCE(new.display_name, ''), new.filename);
END;

CREATE TRIGGER IF NOT EXISTS videos_fts_ad AFTER DELETE ON videos BEGIN
    INSERT INTO videos_fts(videos_fts, rowid, display_name, filename)
        VALUES ('delete', old.id, COALESCE(old.display_name, ''), old.filename);
END;
