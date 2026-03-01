# Plan: Deduplicate handleAddDirectory / handleCreateDirectory (#37)

The two handlers share identical logic: validate path, AddDirectory, syncDir,
serveDirList. The only difference is handleCreateDirectory calls os.MkdirAll first.

## Fix

Extract the shared tail into an `addAndSyncDir(w, r, path)` helper. Both handlers
call it after their pre-condition step.
