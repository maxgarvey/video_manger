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
' Key handling (Player Group holds focus, not the Video node):
'   Up          – toggle the info/rating overlay
'   Down        – dismiss the overlay (no-op if hidden)
'   Left        – overlay visible: focus ♥ Like;  hidden: seek −10 s
'   Right       – overlay visible: focus ★ Fav;   hidden: seek +30 s
'   OK          – overlay visible: toggle selected rating; hidden: pause/play
'   Play        – pause/play (always)
'   Fwd / Rev   – seek ±30 s / ±10 s (always)
'   Back        – stop, save progress, navAction back
'
' All HTTP calls use HttpTask so they run on a background thread and never
' block playback or the UI render thread.

Sub init()
    m.video         = m.top.FindNode("video")
    m.progressTimer = m.top.FindNode("progressTimer")
    m.overlayTimer  = m.top.FindNode("overlayTimer")

    ' ── Colours for the rating buttons ──────────────────────────────────────
    ' Normal (unfocused, not active)
    m.LIKE_BG_NORMAL  = "0x1e1e1eFF"
    m.LIKE_FG_NORMAL  = "0x888888FF"
    m.FAV_BG_NORMAL   = "0x1e1e1eFF"
    m.FAV_FG_NORMAL   = "0x888888FF"
    ' Focused but not yet toggled on
    m.LIKE_BG_FOCUS   = "0x223322FF"
    m.LIKE_FG_FOCUS   = "0xAAFFAAFF"
    m.FAV_BG_FOCUS    = "0x332800FF"
    m.FAV_FG_FOCUS    = "0xFFEE88FF"
    ' Active (rating is set)
    m.LIKE_BG_ACTIVE  = "0x2a4a2aFF"
    m.LIKE_FG_ACTIVE  = "0x88FF88FF"
    m.FAV_BG_ACTIVE   = "0x4a3a00FF"
    m.FAV_FG_ACTIVE   = "0xFFCC00FF"

    ' Overlay label / rect nodes
    m.infoOverlay   = m.top.FindNode("infoOverlay")
    m.ovShow        = m.top.FindNode("ovShow")
    m.ovTitle       = m.top.FindNode("ovTitle")
    m.ovEp          = m.top.FindNode("ovEp")
    m.ovMeta        = m.top.FindNode("ovMeta")
    m.ovActors      = m.top.FindNode("ovActors")
    m.ovPosition    = m.top.FindNode("ovPosition")
    m.ovProgressBg  = m.top.FindNode("ovProgressBg")
    m.ovProgressFill= m.top.FindNode("ovProgressFill")
    m.likeBtnBg     = m.top.FindNode("likeBtnBg")
    m.likeBtnLbl    = m.top.FindNode("likeBtnLbl")
    m.favBtnBg      = m.top.FindNode("favBtnBg")
    m.favBtnLbl     = m.top.FindNode("favBtnLbl")

    m.videoID       = Invalid
    m.serverURL     = ""
    m.overlayVisible= False
    m.ratingFocus   = 0    ' 0 = Like, 1 = Fav
    m.currentRating = 0    ' 0 = none, 1 = like, 2 = fav

    m.video.ObserveField("state", "onVideoState")
    m.progressTimer.ObserveField("fire", "onProgressTimer")
    m.overlayTimer.ObserveField("fire", "onOverlayTimer")
End Sub

' Called by MainScene after AppendChild.
' We focus the Player Group itself (not the Video node) so that all key
' events come directly to our onKeyEvent handler for full control.
Sub onActivated()
    m.top.SetFocus(True)
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

    ' Initialise rating state from the API payload.
    m.currentRating = 0
    If videoData.rating <> Invalid
        m.currentRating = videoData.rating
    End If

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

    ' Pre-populate overlay labels so they're ready when Up is pressed.
    populateOverlayLabels(videoData)
    updateRatingVisual()
End Sub

' ── Overlay: populate metadata labels ─────────────────────────────────────────

Sub populateOverlayLabels(v As Dynamic)
    ' Line 1: show name (or title if no show)
    showName = ""
    If v.show <> Invalid And v.show <> ""
        showName = v.show
    End If
    If showName <> ""
        m.ovShow.text = showName
    Else
        m.ovShow.text = v.title
    End If

    ' Line 2: episode title in quotes (or video title if show is set)
    episodeTitle = ""
    If v.show <> Invalid And v.show <> "" And v.title <> Invalid And v.title <> ""
        episodeTitle = Chr(34) + v.title + Chr(34)
    Else If v.episode_title <> Invalid And v.episode_title <> ""
        episodeTitle = Chr(34) + v.episode_title + Chr(34)
    End If
    m.ovTitle.text = episodeTitle

    ' Line 3: Season · Episode
    epLine = ""
    If v.season <> Invalid And v.season > 0
        epLine = "Season " + v.season.ToStr()
    End If
    If v.episode <> Invalid And v.episode > 0
        If epLine <> "" Then epLine = epLine + "  ·  "
        epLine = epLine + "Episode " + v.episode.ToStr()
    End If
    m.ovEp.text = epLine

    ' Line 4: Genre · Channel · Studio
    metaParts = []
    If v.genre   <> Invalid And v.genre   <> "" Then metaParts.Push(v.genre)
    If v.channel <> Invalid And v.channel <> "" Then metaParts.Push(v.channel)
    If v.studio  <> Invalid And v.studio  <> "" Then metaParts.Push(v.studio)
    m.ovMeta.text = metaParts.Join("  ·  ")

    ' Line 5: Actors · Air date
    actorsText = ""
    If v.actors <> Invalid And v.actors <> ""
        actorsText = v.actors
    End If
    If v.air_date <> Invalid And v.air_date <> ""
        If actorsText <> ""
            actorsText = actorsText + "  ·  Aired: " + v.air_date
        Else
            actorsText = "Aired: " + v.air_date
        End If
    End If
    m.ovActors.text = actorsText
End Sub

' ── Overlay: show / hide / update ─────────────────────────────────────────────

Sub showOverlay()
    updateOverlayPosition()
    updateRatingVisual()
    m.infoOverlay.visible = True
    m.overlayVisible = True
    ' Auto-dismiss after 5 s.
    m.overlayTimer.control = "stop"
    m.overlayTimer.control = "start"
End Sub

Sub hideOverlay()
    m.infoOverlay.visible = False
    m.overlayVisible = False
    m.overlayTimer.control = "stop"
End Sub

Sub onOverlayTimer()
    hideOverlay()
End Sub

Sub updateOverlayPosition()
    position = m.video.position
    duration = m.video.duration

    If position = Invalid Or duration = Invalid Or duration <= 0
        m.ovPosition.text = ""
        m.ovProgressFill.width = 0
        Return
    End If

    m.ovPosition.text = formatDuration(position) + "  /  " + formatDuration(duration)

    ' Scale progress fill to match the background bar width (1380 px).
    fraction = position / duration
    If fraction > 1.0 Then fraction = 1.0
    If fraction < 0.0 Then fraction = 0.0
    m.ovProgressFill.width = Int(1380 * fraction)
End Sub

Sub updateRatingVisual()
    ' ♥ Like button
    If m.currentRating >= 1
        m.likeBtnBg.color  = m.LIKE_BG_ACTIVE
        m.likeBtnLbl.color = m.LIKE_FG_ACTIVE
    Else If m.ratingFocus = 0
        m.likeBtnBg.color  = m.LIKE_BG_FOCUS
        m.likeBtnLbl.color = m.LIKE_FG_FOCUS
    Else
        m.likeBtnBg.color  = m.LIKE_BG_NORMAL
        m.likeBtnLbl.color = m.LIKE_FG_NORMAL
    End If

    ' ★ Fav button
    If m.currentRating = 2
        m.favBtnBg.color  = m.FAV_BG_ACTIVE
        m.favBtnLbl.color = m.FAV_FG_ACTIVE
    Else If m.ratingFocus = 1
        m.favBtnBg.color  = m.FAV_BG_FOCUS
        m.favBtnLbl.color = m.FAV_FG_FOCUS
    Else
        m.favBtnBg.color  = m.FAV_BG_NORMAL
        m.favBtnLbl.color = m.FAV_FG_NORMAL
    End If
End Sub

' ── Rating ────────────────────────────────────────────────────────────────────

Sub toggleRating(selected As Integer)
    ' Toggle off if already set, otherwise apply.
    newRating = selected
    If m.currentRating = selected Then newRating = 0

    m.currentRating = newRating
    updateRatingVisual()

    ' Reset the auto-dismiss timer so the user can see the visual feedback.
    m.overlayTimer.control = "stop"
    m.overlayTimer.control = "start"

    If m.videoID = Invalid Or m.serverURL = "" Then Return
    url  = m.serverURL + "/videos/" + m.videoID.ToStr() + "/rating"
    body = "rating=" + newRating.ToStr()
    postAsync(url, body)
End Sub

' ── Progress saving ───────────────────────────────────────────────────────────

Sub onProgressTimer()
    saveProgress()
    If m.overlayVisible Then updateOverlayPosition()
End Sub

Sub saveProgress()
    If m.videoID = Invalid Or m.serverURL = "" Then Return
    position = m.video.position
    If position = Invalid Then Return
    url  = m.serverURL + "/videos/" + m.videoID.ToStr() + "/progress"
    body = "position=" + Str(position).Trim()
    postAsync(url, body)
End Sub

Sub postAsync(url As String, body As String)
    task = CreateObject("roSGNode", "HttpTask")
    task.url    = url
    task.method = "POST"
    task.body   = body
    task.control = "RUN"
    m.postTask = task
End Sub

' ── Video state changes ───────────────────────────────────────────────────────

Sub onVideoState()
    state = m.video.state

    If state = "finished"
        m.progressTimer.control = "stop"
        hideOverlay()
        If m.videoID <> Invalid And m.serverURL <> ""
            postAsync(m.serverURL + "/videos/" + m.videoID.ToStr() + "/progress", "position=0")
            postAsync(m.serverURL + "/videos/" + m.videoID.ToStr() + "/watched",  "")
        End If
        m.top.navAction = {type: "back"}

    Else If state = "error"
        Print "Player: video error for id="; m.videoID.ToStr()
        m.progressTimer.control = "stop"
        hideOverlay()
        saveProgress()
        m.top.navAction = {type: "back"}
    End If
End Sub

' ── Key handling ─────────────────────────────────────────────────────────────

Function onKeyEvent(key As String, press As Boolean) As Boolean
    If Not press Then Return False

    ' Back always exits the player.
    If key = "back"
        m.progressTimer.control = "stop"
        m.video.control = "stop"
        saveProgress()
        hideOverlay()
        m.top.navAction = {type: "back"}
        Return True
    End If

    ' Up: toggle info/rating overlay.
    If key = "up"
        If m.overlayVisible
            hideOverlay()
        Else
            showOverlay()
        End If
        Return True
    End If

    ' Down: dismiss overlay.
    If key = "down"
        If m.overlayVisible Then hideOverlay()
        Return True
    End If

    ' Left / Right: button navigation when overlay is visible; seek otherwise.
    If key = "left"
        If m.overlayVisible
            m.ratingFocus = 0
            updateRatingVisual()
            resetOverlayTimer()
        Else
            seekBy(-10)
        End If
        Return True
    End If

    If key = "right"
        If m.overlayVisible
            m.ratingFocus = 1
            updateRatingVisual()
            resetOverlayTimer()
        Else
            seekBy(30)
        End If
        Return True
    End If

    ' OK: toggle selected rating when overlay is up; pause/play otherwise.
    If key = "OK"
        If m.overlayVisible
            If m.ratingFocus = 0
                toggleRating(1)
            Else
                toggleRating(2)
            End If
        Else
            togglePlayPause()
        End If
        Return True
    End If

    ' Play / Pause dedicated remote buttons.
    If key = "play"
        togglePlayPause()
        Return True
    End If

    ' Fast-forward / Rewind dedicated buttons.
    If key = "fwd"
        seekBy(30)
        Return True
    End If
    If key = "rev"
        seekBy(-10)
        Return True
    End If

    Return False
End Function

Sub togglePlayPause()
    If m.video.state = "playing"
        m.video.control = "pause"
    Else
        m.video.control = "play"
    End If
End Sub

Sub seekBy(deltaSecs As Integer)
    position = m.video.position
    If position = Invalid Then Return
    target = position + deltaSecs
    If target < 0 Then target = 0
    m.video.seek = target
    If m.overlayVisible Then updateOverlayPosition()
End Sub

Sub resetOverlayTimer()
    m.overlayTimer.control = "stop"
    m.overlayTimer.control = "start"
End Sub
