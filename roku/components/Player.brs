' components/Player.brs
'
' Lifecycle:
'   1.  videoData field is set by MainScene when the component is pushed.
'   2.  onVideoDataChange() builds a ContentNode and starts playback.
'   3.  The progress timer fires every 10 s to POST the current position.
'   4.  When playback finishes (state = "finished"), we POST position=0 to
'       reset progress, then fire navAction back.
'   5.  If the user presses Back, we stop playback, POST the current position,
'       and fire navAction back.
'
' All HTTP calls use HttpTask so they run on a background thread and never
' block playback or the UI render thread.

Sub init()
    m.video         = m.top.FindNode("video")
    m.progressTimer = m.top.FindNode("progressTimer")

    m.videoID  = Invalid
    m.serverURL = ""

    m.video.ObserveField("state", "onVideoState")
    m.progressTimer.ObserveField("fire", "onProgressTimer")
End Sub

' Called by MainScene after AppendChild — safe to call SetFocus here.
Sub onActivated()
    m.video.SetFocus(True)
End Sub

' ── Field handlers ────────────────────────────────────────────────────────────

Sub onVideoDataChange()
    videoData = m.top.videoData
    serverURL = m.top.serverURL

    If videoData = Invalid Or serverURL = "" Or serverURL = Invalid
        Return
    End If

    m.serverURL = serverURL
    m.videoID   = videoData.id

    content = CreateObject("roSGNode", "ContentNode")
    content.url = serverURL + videoData.stream_url

    displayTitle = videoData.title
    If displayTitle = "" Or displayTitle = Invalid
        displayTitle = "Video " + m.videoID.ToStr()
    End If
    content.title = displayTitle

    If videoData.duration_s <> Invalid And videoData.duration_s > 0
        content.length = videoData.duration_s
    End If

    If videoData.position_s <> Invalid And videoData.position_s > 30
        content.bookmarkposition = videoData.position_s
    End If

    If videoData.thumbnail_url <> "" And videoData.thumbnail_url <> Invalid
        content.hdposterurl = serverURL + videoData.thumbnail_url
        content.sdposterurl = serverURL + videoData.thumbnail_url
    End If

    m.video.content = content
    m.video.control = "play"

    m.progressTimer.control = "start"
End Sub

' ── Progress saving ───────────────────────────────────────────────────────────

Sub onProgressTimer()
    saveProgress()
End Sub

' saveProgress fires a background POST — does not block playback.
Sub saveProgress()
    If m.videoID = Invalid Or m.serverURL = ""
        Return
    End If

    position = m.video.position
    If position = Invalid
        Return
    End If

    url  = m.serverURL + "/videos/" + m.videoID.ToStr() + "/progress"
    body = "position=" + Str(position).Trim()
    postAsync(url, body)
End Sub

' postAsync creates a fire-and-forget HttpTask for a POST.
' We keep a reference in m.postTask so it isn't GC'd before completion.
Sub postAsync(url As String, body As String)
    task = CreateObject("roSGNode", "HttpTask")
    task.url    = url
    task.method = "POST"
    task.body   = body
    task.control = "RUN"
    m.postTask = task  ' hold reference until task finishes
End Sub

' ── Video state changes ───────────────────────────────────────────────────────

Sub onVideoState()
    state = m.video.state

    If state = "finished"
        m.progressTimer.control = "stop"

        If m.videoID <> Invalid And m.serverURL <> ""
            postAsync(m.serverURL + "/videos/" + m.videoID.ToStr() + "/progress", "position=0")
            postAsync(m.serverURL + "/videos/" + m.videoID.ToStr() + "/watched",  "")
        End If

        m.top.navAction = {type: "back"}

    Else If state = "error"
        Print "Player: video error for id="; m.videoID.ToStr()
        m.progressTimer.control = "stop"
        saveProgress()
        m.top.navAction = {type: "back"}
    End If
End Sub

' ── Key handling ─────────────────────────────────────────────────────────────

Function onKeyEvent(key As String, press As Boolean) As Boolean
    If press And key = "back"
        m.progressTimer.control = "stop"
        m.video.control = "stop"
        saveProgress()
        m.top.navAction = {type: "back"}
        Return True
    End If
    Return False
End Function
