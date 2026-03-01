# Plan: video_sort setting actually respected (#39)

`serveVideoList` reads `video_sort` from settings and routes to `ListVideosByRating`
when `sortOrder == "rating"`. This already works for the unfiltered case.

The bug: when a tag filter or search query is active, the sort setting is ignored —
`ListVideosByTag` and `SearchVideos` always sort by display name. The switch in
`serveVideoList` picks the list function based on which filter is active, not which
sort is requested. The sort takes lower precedence than the filter.

## Fix

After loading videos with whatever filter applies, if `sortOrder == "rating"`, sort
the in-memory slice by rating descending (then name). This is simpler than adding
sort parameters to every store query, and the library is small enough that in-memory
sort is fine.

```go
if sortOrder == "rating" {
    sort.Slice(videos, func(i, j int) bool {
        if videos[i].Rating != videos[j].Rating {
            return videos[i].Rating > videos[j].Rating
        }
        return videos[i].Title() < videos[j].Title()
    })
}
```

Add `"sort"` to imports (already present via `"sort"` package or use `slices.SortFunc`
from Go 1.21+). The project already uses Go 1.21+ features.
