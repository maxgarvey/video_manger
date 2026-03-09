' components/Player.brs
'
' Logic for the video Player component.
'
' Lifecycle:
'   1.  videoData field is set by MainScene when the component is pushed.
'   2.  onVideoDataChange() builds a ContentNode and starts playback.
'   3.  The progress timer fires every 10 s to POST the current position.
'   4.  When playback finishes (state = "finished"), we POST position=0 to
'       reset progress, then fire navAction back.
'   5.  If the user presses Back, we stop playback, POST the current position,
'       and fire navAction back.

Sub init()
    m.video = m.top.FindNode("video")
    m.progressTimer = m.top.FindNode("progressTimer")

    ' The video ID is needed for progress API calls; store it once we receive
    ' videoData so we don't have to re-read the assocarray on every timer tick.
    m.videoID = Invalid
    m.serverURL = ""

    ' Observe Video node state changes (playing, finished, error, …).
    m.video.ObserveField("state", "onVideoState")

    ' Observe progress timer.
    m.progressTimer.ObserveField("fire", "onProgressTimer")
End Sub

' ── Field handlers ───────────────────────────────────────────────────────────

Sub onVideoDataChange()
    videoData = m.top.videoData
    serverURL = m.top.serverURL

    If videoData = Invalid Or serverURL = "" Or serverURL = Invalid
        Return
    End If

    m.serverURL = serverURL
    m.videoID = videoData.id

    ' Build the Roku ContentNode that the Video node requires.
    content = CreateObject("roSGNode", "ContentNode")
    content.url = serverURL + videoData.stream_url

    ' Title shown in the trick-play overlay.
    displayTitle = videoData.title
    If displayTitle = "" Or displayTitle = Invalid
        displayTitle = "Video " + m.videoID.ToStr()
    End If
    content.title = displayTitle

    ' Duration hint (in seconds) enables the progress bar.
    If videoData.duration_s <> Invalid And videoData.duration_s > 0
        content.length = videoData.duration_s
    End If

    ' Resume from saved position if it is > 30 s (anything shorter is treated
    ' as "start from beginning" to avoid replaying the last few seconds of ads/
    ' intros that the user already saw).
    If videoData.position_s <> Invalid And videoData.position_s > 30
        content.bookmarkposition = videoData.position_s
    End If

    ' Attach thumbnail for the trick-play info panel.
    If videoData.thumbnail_url <> "" And videoData.thumbnail_url <> Invalid
        content.hdposterurl = serverURL + videoData.thumbnail_url
        content.sdposterurl = serverURL + videoData.thumbnail_url
    End If

    ' Assign content and start playback.
    m.video.content = content
    m.video.control = "play"

    ' Start the progress timer.
    m.progressTimer.control = "start"
End Sub

' ── Progress saving ──────────────────────────────────────────────────────────

' onProgressTimer fires every 10 s.  We POST the current playback position to
' the server so the user can resume later.
Sub onProgressTimer()
    saveProgress()
End Sub

' saveProgress POSTs the current position to /videos/{id}/progress.
Sub saveProgress()
    If m.videoID = Invalid Or m.serverURL = ""
        Return
    End If

    position = m.video.position
    If position = Invalid
        Return
    End If

    url = m.serverURL + "/videos/" + m.videoID.ToStr() + "/progress"
    body = "position=" + Str(position).Trim()
    httpPost(url, body)
End Sub

' ── Video state changes ───────────────────────────────────────────────────────

Sub onVideoState()
    state = m.video.state

    If state = "finished"
        ' Playback completed naturally.  Reset the stored position to 0 so
        ' the video shows as fully watched in Recently Watched.
        m.progressTimer.control = "stop"

        If m.videoID <> Invalid And m.serverURL <> ""
            ' Mark watched with position=0 (fully seen).
            progressURL = m.serverURL + "/videos/" + m.videoID.ToStr() + "/progress"
            httpPost(progressURL, "position=0")

            ' Also hit the /watched endpoint to set watched_at timestamp.
            watchedURL = m.serverURL + "/videos/" + m.videoID.ToStr() + "/watched"
            httpPost(watchedURL, "")
        End If

        m.top.navAction = {type: "back"}

    Else If state = "error"
        Print "Player: video error for id="; m.videoID.ToStr()
        m.progressTimer.control = "stop"
        ' Save whatever position we have, then go back.
        saveProgress()
        m.top.navAction = {type: "back"}
    End If
End Sub

' ── Key handling ─────────────────────────────────────────────────────────────

Function onKeyEvent(key As String, press As Boolean) As Boolean
    If press And key = "back"
        ' User pressed Back during playback.
        m.progressTimer.control = "stop"
        m.video.control = "stop"

        ' Save the current position so the user can resume later.
        saveProgress()

        m.top.navAction = {type: "back"}
        Return True
    End If

    ' All other keys (play/pause, forward, rewind, etc.) are handled by the
    ' built-in Video node – return False to let them propagate.
    Return False
End Function
