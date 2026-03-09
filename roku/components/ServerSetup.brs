' components/ServerSetup.brs
'
' First-run server URL entry screen.
'
' Roku's KeyboardDialog is the recommended way to collect text input
' on the Roku remote.  We create it dynamically and observe its responses.

Sub init()
    m.errorLabel = m.top.FindNode("errorLabel")

    ' Create the standard keyboard dialog and add it as a child so it appears
    ' on screen.
    m.keyboard = CreateObject("roSGNode", "KeyboardDialog")

    If m.keyboard = Invalid
        ' Fall back to a simple on-screen message if the dialog isn't available
        ' (should not happen on any Roku with OS 7+).
        Print "ServerSetup: KeyboardDialog not available"
        m.errorLabel.text = "Error: keyboard dialog unavailable. Please update your Roku OS."
        Return
    End If

    m.keyboard.title = "Video Manger Server URL"
    m.keyboard.message = "Enter the server address (e.g. http://192.168.1.100:8080)"

    ' Pre-populate with the "http://" prefix as a convenience.
    m.keyboard.text = "http://"

    ' Add "Connect" and "Cancel" buttons to the dialog button bar.
    ' buttons = ["Connect", "Cancel"]
    ' m.keyboard.buttons = buttons

    ' Observe the dialog's button selection.
    ' m.keyboard.ObserveField("buttonSelected", "onButtonSelected")
    m.keyboard.ObserveField("wasClosed", "onKeyboardClosed")

    m.top.AppendChild(m.keyboard)

    ' Give focus to the keyboard.
    m.keyboard.SetFocus(True)
End Sub

' ── Button handler ────────────────────────────────────────────────────────────

Sub onButtonSelected()
    idx = m.keyboard.buttonSelected

    ' Button indices match the array passed to m.keyboard.buttons:
    '   0 = "Connect"
    '   1 = "Cancel"
    If idx = 0
        ' "Connect" pressed.
        serverURL = m.keyboard.text.Trim()
        validateAndSave(serverURL)
    Else If idx = 1
        ' "Cancel" pressed.  If there is no saved URL, pressing Cancel exits
        ' the channel (nothing else to do); otherwise it goes back.
        m.top.navAction = {type: "back"}
    End If
End Sub

Sub onKeyboardClosed()
    serverURL = m.keyboard.text
    validateAndSave(serverURL)
End Sub

' ── Validation ────────────────────────────────────────────────────────────────

Sub validateAndSave(serverURL)
    ' Basic validation: must start with http:// or https://
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

Sub showError(msg)
    m.errorLabel.text = msg
End Sub

' ── Key handling ─────────────────────────────────────────────────────────────

' TRY THIS IF BROKEN:
' Function onKeyEvent(key, press) As Boolean
Function onKeyEvent(key, press)
    If press And key = "back"
        ' On the setup screen, Back exits the channel since there's nowhere
        ' else to go.
        m.top.navAction = {type: "back"}
        Return True
    End If
    Return False
End Function
