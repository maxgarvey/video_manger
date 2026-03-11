' components/Search.brs
'
' Full-text search screen with live search-as-you-type.
'
' UX flow:
'   1. Keyboard starts focused.  Each keystroke restarts a 500 ms debounce timer.
'   2. When the timer fires, the current query is sent to /api/videos?q=<query>.
'   3. PLAY submits immediately (skips the debounce wait).
'   4. Results populate the thumbnail grid; focus stays on the keyboard so the
'      user can keep refining the query without pressing any extra buttons.
'   5. D-pad Down / navigating into the grid → OK plays the selected video.
'   6. BACK from the keyboard fires navAction {type:"back"} to MainScene.
'
' The server handles partial matching:
'   – ≥ 3 chars: FTS5 trigram (substring match across display_name, filename, tags)
'   – 1–2 chars: LIKE fallback (%term%)

Sub init()
    m.keyboard      = m.top.FindNode("keyboard")
    m.thumbGrid     = m.top.FindNode("thumbGrid")
    m.statusLabel   = m.top.FindNode("statusLabel")
    m.debounceTimer = m.top.FindNode("debounceTimer")

    m.items       = []
    m.fetchTask   = Invalid
    m.gridFocused = False

    m.thumbGrid.ObserveField("itemSelected",    "onItemSelected")
    m.keyboard.ObserveField("text",             "onTextChange")
    m.debounceTimer.ObserveField("fire",        "onDebounceTimer")
End Sub

' Called by MainScene after AppendChild — safe to call SetFocus here.
Sub onActivated()
    m.keyboard.SetFocus(True)
    m.gridFocused = False
End Sub

' ── Key handling ──────────────────────────────────────────────────────────────

Function onKeyEvent(key As String, press As Boolean) As Boolean
    If Not press Then Return False

    ' PLAY = search immediately (keyboard) or play focused item (grid).
    If key = "play"
        If m.gridFocused
            idx = m.thumbGrid.itemFocused
            If idx >= 0 And idx < m.items.Count()
                m.top.navAction = {type: "play", videoData: m.items[idx]}
            End If
        Else
            m.debounceTimer.control = "stop"
            doSearch()
        End If
        Return True
    End If

    ' DOWN from keyboard → move focus into results grid (if visible).
    If key = "down"
        If Not m.gridFocused And m.thumbGrid.visible And m.items.Count() > 0
            m.thumbGrid.SetFocus(True)
            m.gridFocused = True
            m.statusLabel.text = "OK to play  ·  Up to edit search  ·  Back to cancel"
            Return True
        End If
        Return False
    End If

    ' UP from grid top row → bubble back to keyboard.
    If key = "up"
        If m.gridFocused
            m.keyboard.SetFocus(True)
            m.gridFocused = False
            updateStatusLabel()
            Return True
        End If
        Return False
    End If

    ' BACK: if in grid return to keyboard; if in keyboard nav away.
    If key = "back"
        If m.gridFocused
            m.keyboard.SetFocus(True)
            m.gridFocused = False
            updateStatusLabel()
            Return True
        End If
        m.top.navAction = {type: "back"}
        Return True
    End If

    Return False
End Function

Sub updateStatusLabel()
    count = m.items.Count()
    If count = 0 Then Return
    suffix = "s"
    If count = 1 Then suffix = ""
    m.statusLabel.text = count.ToStr() + " result" + suffix + " — Down to browse"
End Sub

' ── Live search ───────────────────────────────────────────────────────────────

Sub onTextChange()
    query = m.keyboard.text
    If query = Invalid Then query = ""
    query = query.Trim()

    If query = ""
        ' Clear results and cancel any pending search.
        m.debounceTimer.control = "stop"
        If m.fetchTask <> Invalid
            m.fetchTask.control = "STOP"
            m.fetchTask = Invalid
        End If
        m.statusLabel.text = ""
        m.thumbGrid.visible = False
        m.items = []
        If m.gridFocused
            m.keyboard.SetFocus(True)
            m.gridFocused = False
        End If
        Return
    End If

    ' Reset the debounce window on every keystroke.
    m.debounceTimer.control = "stop"
    m.debounceTimer.control = "start"
End Sub

Sub onDebounceTimer()
    doSearch()
End Sub

' ── Search ────────────────────────────────────────────────────────────────────

Sub doSearch()
    query = m.keyboard.text
    If query = Invalid Then query = ""
    query = query.Trim()
    If query = "" Then Return

    ' Cancel any in-flight fetch.
    If m.fetchTask <> Invalid
        m.fetchTask.control = "STOP"
        m.fetchTask = Invalid
    End If

    m.statusLabel.text = "Searching…"

    url = m.top.serverURL + "/api/videos?q=" + urlEncode(query)

    m.fetchTask = CreateObject("roSGNode", "HttpTask")
    If m.fetchTask = Invalid
        m.statusLabel.text = "Internal error: HttpTask unavailable"
        Return
    End If
    m.fetchTask.url = url
    m.fetchTask.ObserveField("result", "onFetchDone")
    m.fetchTask.ObserveField("errMsg", "onFetchError")
    m.fetchTask.control = "RUN"
End Sub

Sub onFetchDone()
    data = ParseJSON(m.fetchTask.result)

    If data = Invalid Or data.Count() = 0
        m.statusLabel.text = "No results."
        m.thumbGrid.visible = False
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

    count  = data.Count()
    suffix = "s"
    If count = 1 Then suffix = ""
    m.statusLabel.text = count.ToStr() + " result" + suffix + " — Down to browse"

    root = CreateObject("roSGNode", "ContentNode")
    For i = 0 To labels.Count() - 1
        item = root.CreateChild("ContentNode")
        item.title = labels[i]
        If thumbURLs[i] <> ""
            item.HDGRIDPOSTERURL = thumbURLs[i]
        End If
    End For
    m.thumbGrid.content = root
    m.thumbGrid.visible = True
End Sub

Sub onFetchError()
    m.statusLabel.text = "Error: " + m.fetchTask.errMsg
    m.thumbGrid.visible = False
End Sub

' ── Selection ─────────────────────────────────────────────────────────────────

Sub onItemSelected()
    idx = m.thumbGrid.itemSelected
    If idx < 0 Or idx >= m.items.Count() Then Return
    m.top.navAction = {type: "play", videoData: m.items[idx]}
End Sub
