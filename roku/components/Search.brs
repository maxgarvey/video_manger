' components/Search.brs
'
' Full-text search screen.
'
' UX flow:
'   1. Keyboard starts focused.  User types query (OK = add char).
'   2. PLAY submits the query → fetches /api/videos?q=<query> via HttpTask.
'   3. Results populate the LabelList; focus moves to the list automatically.
'   4. User navigates with D-pad, OK plays the selected video.
'   5. BACK from the list returns focus to the keyboard for a new search.
'   6. BACK from the keyboard fires navAction {type:"back"} to MainScene.

Sub init()
    m.keyboard    = m.top.FindNode("keyboard")
    m.list        = m.top.FindNode("list")
    m.statusLabel = m.top.FindNode("statusLabel")

    m.items       = []
    m.fetchTask   = Invalid
    m.listFocused = False

    m.list.ObserveField("itemSelected", "onItemSelected")
End Sub

' Called by MainScene after AppendChild — safe to call SetFocus here.
Sub onActivated()
    m.keyboard.SetFocus(True)
    m.listFocused = False
End Sub

' ── Key handling ──────────────────────────────────────────────────────────────

Function onKeyEvent(key As String, press As Boolean) As Boolean
    If Not press Then Return False

    If key = "play"
        doSearch()
        Return True
    End If

    If key = "back"
        If m.listFocused
            ' Return focus to keyboard so the user can refine the query.
            m.keyboard.SetFocus(True)
            m.listFocused = False
        Else
            m.top.navAction = {type: "back"}
        End If
        Return True
    End If

    Return False
End Function

' ── Search ────────────────────────────────────────────────────────────────────

Sub doSearch()
    query = m.keyboard.text.Trim()
    If query = "" Or query = Invalid
        m.statusLabel.text = "Enter a search term first."
        Return
    End If

    ' Cancel any in-flight fetch.
    If m.fetchTask <> Invalid
        m.fetchTask.control = "STOP"
        m.fetchTask = Invalid
    End If

    m.statusLabel.text = "Searching…"
    m.items = []
    populateList([])

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

    suffix = "s"
    If data.Count() = 1 Then suffix = ""
    m.statusLabel.text = data.Count().ToStr() + " result" + suffix
    populateList(labels)

    ' Move focus to the results list automatically.
    m.list.SetFocus(True)
    m.listFocused = True
End Sub

Sub onFetchError()
    m.statusLabel.text = "Error: " + m.fetchTask.errMsg
End Sub

' ── List helpers ──────────────────────────────────────────────────────────────

Sub populateList(labels As Object)
    root = CreateObject("roSGNode", "ContentNode")
    For Each label In labels
        item = root.CreateChild("ContentNode")
        item.title = label
    End For
    m.list.content = root
End Sub

Sub onItemSelected()
    idx = m.list.itemSelected
    If idx < 0 Or idx >= m.items.Count() Then Return
    m.top.navAction = {type: "play", videoData: m.items[idx]}
End Sub
