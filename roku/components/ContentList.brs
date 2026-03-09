' components/ContentList.brs
'
' Logic for the ContentList component.  Handles all list modes, data loading,
' selection, and back-key navigation.

Sub init()
    ' m.items holds the raw API payload for each displayed row so we can pass
    ' the full object to the Player without re-fetching.
    m.items = []

    m.list = m.top.FindNode("list")
    m.titleLabel = m.top.FindNode("titleLabel")
    m.statusLabel = m.top.FindNode("statusLabel")

    ' Observe list selection.
    m.list.ObserveField("itemSelected", "onItemSelected")

    ' NOTE: key events are handled via onKeyEvent() below.  There is no need
    ' to explicitly observe focusedChild for that purpose.
End Sub

' ── Field change handlers ────────────────────────────────────────────────────

Sub onServerURLChange()
    ' Re-load whenever the server URL is (re-)set, but only if mode is also set.
    If m.top.mode <> "" And m.top.serverURL <> ""
        loadContent()
    End If
End Sub

Sub onModeChange()
    ' Re-load whenever the mode changes, but only if we have a server URL.
    If m.top.serverURL <> ""
        loadContent()
    End If
End Sub

' ── Content loading ──────────────────────────────────────────────────────────

' loadContent() dispatches to the appropriate loader based on the current mode.
'
' NOTE ON THREADING: These HTTP calls are made synchronously on the component's
' render thread.  For a personal LAN server with fast local network responses
' this is acceptable.  A production channel serving content over the internet
' would move the fetch into a Task node and return results via a field observer.
Sub loadContent()
    mode = m.top.mode
    m.statusLabel.text = "Loading…"
    m.items = []

    If mode = "menu"
        loadMenu()
    Else If mode = "shows"
        loadShows()
    Else If mode = "seasons"
        loadSeasons()
    Else If mode = "episodes"
        loadEpisodes()
    Else If mode = "videos"
        loadVideos()
    Else If mode = "recent"
        loadRecent()
    Else
        m.statusLabel.text = "Unknown mode: " + mode
    End If
End Sub

' ── Menu mode ────────────────────────────────────────────────────────────────

Sub loadMenu()
    m.titleLabel.text = "Video Manger"
    m.statusLabel.text = ""

    ' Static menu items; m.items stores routing metadata for each.
    labels = ["Recently Watched", "TV Shows", "Movies", "All Videos", "Random", "Change Server"]
    m.items = [
        {menuAction: "recent"},
        {menuAction: "shows"},
        {menuAction: "videos", videoType: "Movie"},
        {menuAction: "videos", videoType: ""},
        {menuAction: "random"},
        {menuAction: "changeServer"}
    ]

    populateList(labels)
End Sub

' ── Shows mode ───────────────────────────────────────────────────────────────

Sub loadShows()
    m.titleLabel.text = "TV Shows"

    url = m.top.serverURL + "/api/shows"
    data = httpGetJSON(url)

    If data = Invalid Or data.Count() = 0
        m.statusLabel.text = "No shows found."
        populateList([])
        Return
    End If

    labels = []
    m.items = []
    For Each show In data
        label = show.title
        If show.season_count > 0
            label = label + " (" + show.season_count.ToStr() + " season"
            If show.season_count <> 1
                label = label + "s"
            End If
            label = label + ")"
        End If
        labels.Push(label)
        m.items.Push(show)
    End For

    m.statusLabel.text = ""
    populateList(labels)
End Sub

' ── Seasons mode ─────────────────────────────────────────────────────────────

Sub loadSeasons()
    showName = m.top.showName
    m.titleLabel.text = showName + " – Seasons"

    url = m.top.serverURL + "/api/shows/" + urlEncode(showName) + "/seasons"
    data = httpGetJSON(url)

    If data = Invalid Or data.Count() = 0
        m.statusLabel.text = "No seasons found."
        populateList([])
        Return
    End If

    labels = []
    m.items = []
    For Each season In data
        label = "Season " + season.number.ToStr()
        label = label + " (" + season.episode_count.ToStr() + " episode"
        If season.episode_count <> 1
            label = label + "s"
        End If
        label = label + ")"
        labels.Push(label)
        m.items.Push(season)
    End For

    m.statusLabel.text = ""
    populateList(labels)
End Sub

' ── Episodes mode ────────────────────────────────────────────────────────────

Sub loadEpisodes()
    showName = m.top.showName
    seasonNum = m.top.seasonNumber
    m.titleLabel.text = showName + " – Season " + seasonNum.ToStr()

    url = m.top.serverURL + "/api/shows/" + urlEncode(showName) + "/seasons/" + seasonNum.ToStr() + "/episodes"
    data = httpGetJSON(url)

    If data = Invalid Or data.Count() = 0
        m.statusLabel.text = "No episodes found."
        populateList([])
        Return
    End If

    labels = []
    m.items = []
    For Each ep In data
        ' Format: "E01 – Episode Title (45:00)"
        label = "E" + Right("0" + ep.episode.ToStr(), 2)
        If ep.episode_title <> "" And ep.episode_title <> Invalid
            label = label + " – " + ep.episode_title
        Else If ep.title <> "" And ep.title <> Invalid
            label = label + " – " + ep.title
        End If
        If ep.duration_s <> Invalid And ep.duration_s > 0
            label = label + " (" + formatDuration(ep.duration_s) + ")"
        End If
        labels.Push(label)
        m.items.Push(ep)
    End For

    m.statusLabel.text = ""
    populateList(labels)
End Sub

' ── Videos mode ──────────────────────────────────────────────────────────────

Sub loadVideos()
    videoType = m.top.videoType
    If videoType <> "" And videoType <> Invalid
        m.titleLabel.text = videoType + "s"
        url = m.top.serverURL + "/api/videos?type=" + urlEncode(videoType)
    Else
        m.titleLabel.text = "All Videos"
        url = m.top.serverURL + "/api/videos"
    End If

    data = httpGetJSON(url)

    If data = Invalid Or data.Count() = 0
        m.statusLabel.text = "No videos found."
        populateList([])
        Return
    End If

    labels = []
    m.items = []
    For Each v In data
        label = v.title
        If v.duration_s <> Invalid And v.duration_s > 0
            label = label + " (" + formatDuration(v.duration_s) + ")"
        End If
        labels.Push(label)
        m.items.Push(v)
    End For

    m.statusLabel.text = ""
    populateList(labels)
End Sub

' ── Recently-watched mode ────────────────────────────────────────────────────

Sub loadRecent()
    m.titleLabel.text = "Recently Watched"

    url = m.top.serverURL + "/api/recently-watched"
    data = httpGetJSON(url)

    If data = Invalid Or data.Count() = 0
        m.statusLabel.text = "Nothing watched yet."
        populateList([])
        Return
    End If

    labels = []
    m.items = []
    For Each entry In data
        label = entry.title

        ' Show resume position if it exists and is meaningful (> 30 s).
        If entry.position_s <> Invalid And entry.position_s > 30
            label = label + " [at " + formatDuration(entry.position_s) + "]"
        End If

        labels.Push(label)
        m.items.Push(entry)
    End For

    m.statusLabel.text = ""
    populateList(labels)
End Sub

' ── List helpers ─────────────────────────────────────────────────────────────

' populateList builds a ContentNode tree from a string array and assigns it
' to the LabelList.  Roku's LabelList requires a ContentNode with one child
' ContentNode per item, each with a "title" field set.
Sub populateList(labels As Object)
    root = CreateObject("roSGNode", "ContentNode")

    For Each label In labels
        item = root.CreateChild("ContentNode")
        item.title = label
    End For

    m.list.content = root

    ' Restore focus to the list after content loads.
    m.list.SetFocus(True)
End Sub

' ── Selection handler ────────────────────────────────────────────────────────

Sub onItemSelected()
    idx = m.list.itemSelected
    If idx < 0 Or idx >= m.items.Count()
        Return
    End If

    item = m.items[idx]
    mode = m.top.mode

    If mode = "menu"
        handleMenuSelect(item)
    Else If mode = "shows"
        ' Drill into the seasons for this show.
        m.top.navAction = {
            type: "push",
            comp: "ContentList",
            params: {
                mode: "seasons",
                showName: item.title,
                serverURL: m.top.serverURL
            }
        }
    Else If mode = "seasons"
        ' Drill into episodes for this season.
        m.top.navAction = {
            type: "push",
            comp: "ContentList",
            params: {
                mode: "episodes",
                showName: m.top.showName,
                seasonNumber: item.number,
                serverURL: m.top.serverURL
            }
        }
    Else If mode = "episodes" Or mode = "videos"
        ' Play the selected video.
        m.top.navAction = {type: "play", videoData: item}
    Else If mode = "recent"
        ' Play with resume position already embedded in item as position_s.
        m.top.navAction = {type: "play", videoData: item}
    End If
End Sub

Sub handleMenuSelect(item As Object)
    action = item.menuAction

    If action = "recent"
        m.top.navAction = {
            type: "push",
            comp: "ContentList",
            params: {mode: "recent", serverURL: m.top.serverURL}
        }
    Else If action = "shows"
        m.top.navAction = {
            type: "push",
            comp: "ContentList",
            params: {mode: "shows", serverURL: m.top.serverURL}
        }
    Else If action = "videos"
        m.top.navAction = {
            type: "push",
            comp: "ContentList",
            params: {
                mode: "videos",
                videoType: item.videoType,
                serverURL: m.top.serverURL
            }
        }
    Else If action = "random"
        ' Fetch a random video and play it immediately.
        url = m.top.serverURL + "/api/random"
        video = httpGetJSON(url)
        If video = Invalid
            m.statusLabel.text = "Could not fetch random video."
            Return
        End If
        m.top.navAction = {type: "play", videoData: video}
    Else If action = "changeServer"
        m.top.navAction = {type: "push", comp: "ServerSetup", params: {}}
    End If
End Sub

' ── Key handling ─────────────────────────────────────────────────────────────

' onKeyEvent is the standard way to intercept remote-control key presses in a
' SceneGraph component.
Function onKeyEvent(key As String, press As Boolean) As Boolean
    If press And key = "back"
        m.top.navAction = {type: "back"}
        Return True  ' consume the event so Roku doesn't exit the channel
    End If
    Return False
End Function
