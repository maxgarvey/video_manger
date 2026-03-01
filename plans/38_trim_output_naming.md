# Plan: Trim output naming — avoid silent overwrite (#38)

Currently `handleTrim` always writes `{base}_trim{ext}`. The ffmpeg `-y` flag
means it overwrites without asking. A second trim produces a different result but
silently replaces the first.

## Fix

Before calling ffmpeg, check whether the proposed output path already exists.
If it does, append `_2`, `_3`, … until a free name is found.

```go
func freeOutputName(dir, base, suffix, ext string) string {
    name := base + suffix + ext
    if _, err := os.Stat(filepath.Join(dir, name)); os.IsNotExist(err) {
        return name
    }
    for i := 2; ; i++ {
        name = fmt.Sprintf("%s%s_%d%s", base, suffix, i, ext)
        if _, err := os.Stat(filepath.Join(dir, name)); os.IsNotExist(err) {
            return name
        }
    }
}
```

Use in `handleTrim`: `outName = freeOutputName(dir, base, "_trim", ext)`
Also use it in `handleConvert` for the same reason.
