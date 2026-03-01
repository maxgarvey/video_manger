# Plan: handleBrowseFS path restriction (#46)

`GET /fs?path=...` accepts any absolute path after `filepath.Clean`. Anyone on the
LAN can enumerate the full filesystem.

## Fix

Restrict the path to the user's home directory subtree. Any path that is not
`filepath.Rel`-able from the home directory, or that starts with `..`, is rejected
with 403.

This still allows browsing any subdirectory of the home directory, which covers all
practical use cases (adding a library directory).

If the user wants to add a directory outside their home dir, they can type the path
directly into the text field — the browser UI does not need to browse there.
