' components/ContentList.brs
'
' All HTTP calls run via HttpTask (a Task node) on a background thread so the
' UI render thread is never blocked.

Sub init()
    m.items        = []
    m.pendingMode  = ""
    m.fetchTask    = Invalid
    m.randomTask   = Invalid
    m.useThumbGrid = False

    m.list        = m.top.FindNode("list")
    m.thumbGrid   = m.top.FindNode("thumbGrid")
    m.titleLabel  = m.top.FindNode("titleLabel")
    m.statusLabel = m.top.FindNode("statusLabel")

    m.list.ObserveField("itemSelected",      "onItemSelected")
    m.thumbGrid.ObserveField("itemSelected", "onThumbGridSelected")
End Sub

' Called by MainScene after AppendChild — safe to call SetFocus here.
Sub onActivated()
    If m.useThumbGrid
        m.thumbGrid.SetFocus(True)
    Else
        m.list.SetFocus(True)
    End If
End Sub

' ── Field change handlers ─────────────────────────────────────────────────────

Sub onServerURLChange()
    If m.top.mode <> "" And m.top.serverURL <> ""
        loadContent()
    End If
End Sub

Sub onModeChange()
    If m.top.serverURL <> ""
        loadContent()
    End If
End Sub

' ── Content loading ───────────────────────────────────────────────────────────

Sub loadContent()
    mode = m.top.mode
    m.statusLabel.text = "Loading…"
    m.items = []

    If mode = "menu"
        ' Menu is static — no HTTP needed.
        loadMenu()
        Return
    End If

    url = buildURL(mode)
    If url = ""
        m.statusLabel.text = "Unknown mode: " + mode
        Return
    End If

    startFetch(url, mode)
End Sub

Function buildURL(mode As String) As String
    s = m.top.serverURL
    If mode = "shows"
        Return s + "/api/shows"
    Else If mode = "seasons"
        Return s + "/api/shows/" + urlEncode(m.top.showName) + "/seasons"
    Else If mode = "episodes"
        Return s + "/api/shows/" + urlEncode(m.top.showName) + "/seasons/" + m.top.seasonNumber.ToStr() + "/episodes"
    Else If mode = "videos"
        t = m.top.videoType
        If t <> "" And t <> Invalid
            Return s + "/api/videos?type=" + urlEncode(t)
        End If
        Return s + "/api/videos"
    Else If mode = "recent"
        Return s + "/api/recently-watched"
    End If
    Return ""
End Function

Sub startFetch(url As String, mode As String)
    ' Stop any in-flight request for a previous mode.
    If m.fetchTask <> Invalid
        m.fetchTask.control = "STOP"
        m.fetchTask = Invalid
    End If

    m.pendingMode = mode

    Print "ContentList: startFetch url="; url; " mode="; mode
    m.fetchTask = CreateObject("roSGNode", "HttpTask")
    If m.fetchTask = Invalid
        Print "ContentList: ERROR - could not create HttpTask"
        m.statusLabel.text = "Internal error: HttpTask missing"
        Return
    End If
    m.fetchTask.url = url
    m.fetchTask.ObserveField("result", "onFetchDone")
    m.fetchTask.ObserveField("errMsg", "onFetchError")
    m.fetchTask.control = "RUN"
    Print "ContentList: HttpTask started"
End Sub

Sub onFetchDone()
    Print "ContentList: onFetchDone fired"
    data = ParseJSON(m.fetchTask.result)
    mode = m.pendingMode

    If mode = "shows"
        processShows(data)
    Else If mode = "seasons"
        processSeasons(data)
    Else If mode = "episodes"
        processEpisodes(data)
    Else If mode = "videos"
        processVideos(data)
    Else If mode = "recent"
        processRecent(data)
    End If
End Sub

Sub onFetchError()
    msg = m.fetchTask.errMsg
    Print "ContentList: onFetchError msg="; msg
    m.statusLabel.text = "Error: " + msg
End Sub

' ── Menu mode ─────────────────────────────────────────────────────────────────

Sub loadMenu()
    m.titleLabel.text = "Video Manger"
    m.statusLabel.text = ""

    labels = ["Recently Watched", "TV Shows", "Movies", "All Videos", "Search", "Random", "Change Server"]
    m.items = [
        {menuAction: "recent"},
        {menuAction: "shows"},
        {menuAction: "videos", videoType: "Movie"},
        {menuAction: "videos", videoType: ""},
        {menuAction: "search"},
        {menuAction: "random"},
        {menuAction: "changeServer"}
    ]

    populateList(labels)
End Sub

' ── Data processors (called from onFetchDone on the render thread) ────────────

Sub processShows(data As Dynamic)
    m.titleLabel.text = "TV Shows"
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

Sub processSeasons(data As Dynamic)
    m.titleLabel.text = m.top.showName + " – Seasons"
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

Sub processEpisodes(data As Dynamic)
    m.titleLabel.text = m.top.showName + " – Season " + m.top.seasonNumber.ToStr()
    If data = Invalid Or data.Count() = 0
        m.statusLabel.text = "No episodes found."
        populateList([])
        Return
    End If

    labels    = []
    thumbURLs = []
    m.items   = []
    For Each ep In data
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
        thumbURL = ""
        If ep.thumbnail_url <> Invalid And ep.thumbnail_url <> ""
            thumbURL = m.top.serverURL + ep.thumbnail_url
        End If
        thumbURLs.Push(thumbURL)
    End For

    m.statusLabel.text = ""
    populateThumbGrid(labels, thumbURLs)
End Sub

Sub processVideos(data As Dynamic)
    videoType = m.top.videoType
    If videoType <> "" And videoType <> Invalid
        m.titleLabel.text = videoType + "s"
    Else
        m.titleLabel.text = "All Videos"
    End If

    If data = Invalid Or data.Count() = 0
        m.statusLabel.text = "No videos found."
        populateList([])
        Return
    End If

    labels    = []
    thumbURLs = []
    m.items   = []
    For Each v In data
        label = v.title
        If v.duration_s <> Invalid And v.duration_s > 0
            label = label + " (" + formatDuration(v.duration_s) + ")"
        End If
        labels.Push(label)
        m.items.Push(v)
        thumbURL = ""
        If v.thumbnail_url <> Invalid And v.thumbnail_url <> ""
            thumbURL = m.top.serverURL + v.thumbnail_url
        End If
        thumbURLs.Push(thumbURL)
    End For

    m.statusLabel.text = ""
    populateThumbGrid(labels, thumbURLs)
End Sub

Sub processRecent(data As Dynamic)
    m.titleLabel.text = "Recently Watched"
    If data = Invalid Or data.Count() = 0
        m.statusLabel.text = "Nothing watched yet."
        populateList([])
        Return
    End If

    labels    = []
    thumbURLs = []
    m.items   = []
    For Each entry In data
        label = entry.title
        If entry.position_s <> Invalid And entry.position_s > 30
            label = label + " [at " + formatDuration(entry.position_s) + "]"
        End If
        labels.Push(label)
        m.items.Push(entry)
        thumbURL = ""
        If entry.thumbnail_url <> Invalid And entry.thumbnail_url <> ""
            thumbURL = m.top.serverURL + entry.thumbnail_url
        End If
        thumbURLs.Push(thumbURL)
    End For

    m.statusLabel.text = ""
    populateThumbGrid(labels, thumbURLs)
End Sub

' ── List helpers ──────────────────────────────────────────────────────────────

Sub populateList(labels As Object)
    root = CreateObject("roSGNode", "ContentNode")
    For Each label In labels
        item = root.CreateChild("ContentNode")
        item.title = label
    End For
    m.list.content = root
    m.list.visible = True
    m.thumbGrid.visible = False
    m.useThumbGrid = False
    m.list.SetFocus(True)
End Sub

' Populate the thumbnail grid for video-item modes (episodes, videos, recent).
' labels    – array of display strings
' thumbURLs – array of full thumbnail URL strings (empty string = no thumbnail)
Sub populateThumbGrid(labels As Object, thumbURLs As Object)
    root = CreateObject("roSGNode", "ContentNode")
    For i = 0 To labels.Count() - 1
        item = root.CreateChild("ContentNode")
        item.title = labels[i]
        If i < thumbURLs.Count() And thumbURLs[i] <> ""
            item.HDGRIDPOSTERURL = thumbURLs[i]
        End If
    End For
    m.thumbGrid.content = root
    m.thumbGrid.visible = True
    m.list.visible = False
    m.useThumbGrid = True
    m.thumbGrid.SetFocus(True)
End Sub

' ── Selection handler ─────────────────────────────────────────────────────────

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
    Else If mode = "episodes" Or mode = "videos" Or mode = "recent"
        m.top.navAction = {type: "play", videoData: item}
    End If
End Sub

' Thumbnail grid selection – only used for modes that play videos directly.
Sub onThumbGridSelected()
    idx = m.thumbGrid.itemSelected
    If idx < 0 Or idx >= m.items.Count() Then Return
    m.top.navAction = {type: "play", videoData: m.items[idx]}
End Sub

Sub handleMenuSelect(item As Object)
    action = item.menuAction
    serverURL = m.top.serverURL

    If action = "recent"
        m.top.navAction = {
            type: "push",
            comp: "ContentList",
            params: {mode: "recent", serverURL: serverURL}
        }
    Else If action = "shows"
        m.top.navAction = {
            type: "push",
            comp: "ContentList",
            params: {mode: "shows", serverURL: serverURL}
        }
    Else If action = "videos"
        m.top.navAction = {
            type: "push",
            comp: "ContentList",
            params: {
                mode: "videos",
                videoType: item.videoType,
                serverURL: serverURL
            }
        }
    Else If action = "search"
        m.top.navAction = {
            type: "push",
            comp: "Search",
            params: {serverURL: serverURL}
        }
    Else If action = "random"
        ' Fetch a random video asynchronously then play it.
        m.statusLabel.text = "Picking a random video…"
        m.randomTask = CreateObject("roSGNode", "HttpTask")
        m.randomTask.url = serverURL + "/api/random"
        m.randomTask.ObserveField("result", "onRandomResult")
        m.randomTask.ObserveField("errMsg",  "onRandomError")
        m.randomTask.control = "RUN"
    Else If action = "changeServer"
        m.top.navAction = {type: "push", comp: "ServerSetup", params: {}}
    End If
End Sub

Sub onRandomResult()
    video = ParseJSON(m.randomTask.result)
    m.statusLabel.text = ""
    If video = Invalid
        m.statusLabel.text = "Could not fetch random video."
        Return
    End If
    m.top.navAction = {type: "play", videoData: video}
End Sub

Sub onRandomError()
    m.statusLabel.text = "Connection error — check server address"
End Sub

' ── Key handling ──────────────────────────────────────────────────────────────

Function onKeyEvent(key As String, press As Boolean) As Boolean
    If press And key = "back"
        m.top.navAction = {type: "back"}
        Return True
    End If
    Return False
End Function
