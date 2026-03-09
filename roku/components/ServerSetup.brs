' components/ServerSetup.brs
'
' First-run server URL entry screen.
'
' Uses a Keyboard node (inline widget) for text input.  KeyboardDialog requires
' Scene-level placement and cannot receive focus when added to a Group child.
'
' Focus is given to the Keyboard in onActivated(), which is called by MainScene
' after AppendChild — SetFocus has no effect before the node is in the scene tree.

Sub init()
    m.errorLabel = m.top.FindNode("errorLabel")
    m.keyboard   = m.top.FindNode("keyboard")

    ' Pre-populate with the default server address.
    m.keyboard.text = "http://192.168.86.26:8080"
End Sub

' Called by MainScene after AppendChild — safe to call SetFocus here.
Sub onActivated()
    m.keyboard.SetFocus(True)
End Sub

' ── Key handling ──────────────────────────────────────────────────────────────
'
' The Keyboard node consumes D-pad and OK for character input.  We catch PLAY
' (the remote's play button) for form submission since OK is taken by the widget.
' BACK exits the channel (nowhere else to go on first run).

Function onKeyEvent(key As String, press As Boolean) As Boolean
    If Not press Then Return False

    If key = "play"
        validateAndSave(m.keyboard.text)
        Return True
    End If

    If key = "back"
        m.top.navAction = {type: "back"}
        Return True
    End If

    Return False
End Function

' ── Validation ────────────────────────────────────────────────────────────────

Sub validateAndSave(serverURL As String)
    serverURL = serverURL.Trim()

    If serverURL = "" Or serverURL = Invalid
        showError("Please enter a server URL.")
        Return
    End If

    urlLower = LCase(serverURL)
    If Left(urlLower, 7) <> "http://" And Left(urlLower, 8) <> "https://"
        showError("URL must start with http:// or https://")
        Return
    End If

    ' Strip trailing slash for consistency.
    If Right(serverURL, 1) = "/"
        serverURL = Left(serverURL, Len(serverURL) - 1)
    End If

    ' Signal MainScene to persist the URL and navigate to the main menu.
    m.top.navAction = {type: "serverSaved", serverURL: serverURL}
End Sub

Sub showError(msg As String)
    m.errorLabel.text = msg
End Sub
