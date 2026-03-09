' source/main.brs
'
' Channel entry-point.  Roku firmware calls Main() when the channel starts.
' We create an roSGScreen (the SceneGraph root), push MainScene onto it, then
' run the standard message-pump loop until the user exits.

Sub Main(args As Dynamic)
    ' args is an AA that may contain deeplink info (contentId, mediaType).
    ' We ignore it for now but could extend later for deep-linking.

    screen = CreateObject("roSGScreen")
    If screen = Invalid
        ' Should never happen on a real Roku, but guard defensively.
        Return
    End If

    ' The message port receives all events from the SceneGraph.
    port = CreateObject("roMessagePort")
    screen.SetMessagePort(port)

    ' MainScene is our root SceneGraph component (see components/MainScene.xml).
    scene = screen.CreateScene("MainScene")
    screen.Show()

    ' Standard Roku message loop.  We only need to watch for the screen being
    ' closed (e.g. the user presses Home); all navigation is handled inside the
    ' SceneGraph components themselves via observeField callbacks.
    While True
        msg = Wait(0, port)
        msgType = Type(msg)

        If msgType = "roSGScreenEvent"
            If msg.IsScreenClosed()
                ' User pressed Home or Back to the home screen.
                Return
            End If
        End If
    End While
End Sub
