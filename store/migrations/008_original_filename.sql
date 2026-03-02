-- Store the original filename at first import so renames don't lose it.
ALTER TABLE videos ADD COLUMN original_filename TEXT NOT NULL DEFAULT '';

-- Backfill existing rows: assume current filename is the original.
UPDATE videos SET original_filename = filename WHERE original_filename = '';
