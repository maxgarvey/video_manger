# Plan: Store original filename

## Problem
When a user renames a video's display name, or when a file is moved (changing
its disk path), the original filename is lost. Useful for reference.

## Fix
- Migration 008: ALTER TABLE videos ADD COLUMN original_filename TEXT NOT NULL DEFAULT ''
  + backfill: UPDATE videos SET original_filename = filename WHERE original_filename = ''
- UpsertVideo: include original_filename in INSERT (= filename); ON CONFLICT DO UPDATE
  does NOT touch original_filename so it is preserved forever.
- Video struct: add OriginalFilename string
- scan helpers + all SELECT queries: include original_filename
- player.html rename section: show "Originally: <filename>" when original_filename
  differs from the current filename.
