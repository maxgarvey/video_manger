' components/MainScene.brs
'
' Controller for the root Scene.  Owns the navigation stack and routes navAction
' signals from child components.

' Registry section name and key used to persist the server URL across launches.
Sub init()
    Print "MainScene: init"
    m.REGISTRY_SECTION = "VideoManger"
    m.REGISTRY_KEY_URL = "serverURL"

    ' m.stack holds the array of currently-visible component nodes (bottom =
    ' index 0, top = last element).  Only the top node is shown; the rest are
    ' detached but kept so state is preserved on back-navigation.
    m.stack = []
    m.serverURL = ""

    ' Attempt to load a previously-saved server URL from persistent storage.
    section = CreateObject("roRegistrySection", m.REGISTRY_SECTION)
    If section.Exists(m.REGISTRY_KEY_URL)
        m.serverURL = section.Read(m.REGISTRY_KEY_URL)
    End If

    If m.serverURL = "" Or m.serverURL = Invalid
        ' First run: ask the user to enter the server URL.
        pushView("ServerSetup", {})
    Else
        ' We have a URL – go straight to the main menu.
        pushView("ContentList", {
            mode: "menu"
            serverURL: m.serverURL
        })
    End If
End Sub

' ---------------------------------------------------------------------------
' pushView(compName, params)
'
' Creates a new instance of the named component, applies params as field
' values, appends it to the navigation stack, and adds it as a child of the
' scene so it becomes visible.
' ---------------------------------------------------------------------------
Sub pushView(compName, params)
    node = CreateObject("roSGNode", compName)
    If node = Invalid
        Print "MainScene: could not create component "; compName
        Return
    End If

    ' Always inject the current server URL so child components don't need to
    ' read the registry themselves.
    If node.HasField("serverURL")
        node.serverURL = m.serverURL
    End If

    ' Apply caller-supplied field values (e.g. mode, showName, …).
    If params <> Invalid
        For Each key In params
            If node.HasField(key)
                node[key] = params[key]
            End If
        End For
    End If

    ' Watch the navAction field so the component can signal navigation events.
    node.ObserveField("navAction", "onNavAction")

    ' Hide the previous top-of-stack view to avoid rendering both at once.
    If m.stack.Count() > 0
        m.stack[m.stack.Count() - 1].visible = False
    End If

    m.stack.Push(node)
    m.top.AppendChild(node)

    ' Give focus to the new view.  Components that manage an internal focused
    ' child (e.g. ServerSetup → Keyboard, ContentList → LabelList) expose an
    ' `isActive` field; setting it here (after AppendChild) lets them call
    ' SetFocus on the right child now that they are in the scene tree.
    ' Components without `isActive` fall back to focusing the node itself.
    If node.HasField("isActive")
        node.isActive = True
    Else
        node.SetFocus(True)
    End If
End Sub

' ---------------------------------------------------------------------------
' popView()
'
' Removes the top component from the stack and restores the previous one.
' If the stack would become empty after a pop, the channel exits.
' ---------------------------------------------------------------------------
Sub popView()
    If m.stack.Count() = 0
        Return
    End If

    ' Detach the current top view.
    topNode = m.stack.Pop()
    m.top.RemoveChild(topNode)

    If m.stack.Count() = 0
        ' No more views – exit the channel.
        ' m.top IS the Scene node in MainScene.brs, so Exit() on it closes
        ' the channel and returns the user to the Roku home screen.
        m.top.Exit()
        Return
    End If

    ' Re-show the previous view and give it focus.
    prevNode = m.stack[m.stack.Count() - 1]
    prevNode.visible = True
    If prevNode.HasField("isActive")
        ' Toggle to ensure onChange fires even if already True.
        prevNode.isActive = False
        prevNode.isActive = True
    Else
        prevNode.SetFocus(True)
    End If
End Sub

' ---------------------------------------------------------------------------
' onNavAction()
'
' Called whenever any child component sets its navAction field.
' Dispatches to the correct navigation handler based on action type.
' ---------------------------------------------------------------------------
Sub onNavAction()
    ' The observer receives the new field value via m.top.findNode mechanism,
    ' but since multiple nodes can fire, we must read from the sender.
    ' Roku fires the callback with the field value as the argument when using
    ' the two-argument observeField form; here we use the single-arg form so
    ' we walk the stack to find who fired.
    action = Invalid
    For i = m.stack.Count() - 1 To 0 Step -1
        candidate = m.stack[i].navAction
        If candidate <> Invalid And Type(candidate) = "roAssociativeArray"
            action = candidate
            Exit For
        End If
    End For

    If action = Invalid Or action.Lookup("type") = Invalid
        Return
    End If

    actionType = action.type

    If actionType = "back"
        popView()

    Else If actionType = "push"
        compName = action.comp
        params = action.params
        If params = Invalid
            params = {}
        End If
        pushView(compName, params)

    Else If actionType = "play"
        videoData = action.videoData
        If videoData = Invalid
            Print "MainScene: play action missing videoData"
            Return
        End If
        pushView("Player", {
            videoData: videoData
            serverURL: m.serverURL
        })

    Else If actionType = "serverSaved"
        newURL = action.serverURL
        If newURL = Invalid Or newURL = ""
            Print "MainScene: serverSaved action has empty URL"
            Return
        End If

        ' Persist the URL to the registry.
        m.serverURL = newURL
        section = CreateObject("roRegistrySection", m.REGISTRY_SECTION)
        section.Write(m.REGISTRY_KEY_URL, m.serverURL)
        section.Flush()

        ' Replace the entire stack with the main menu.
        ' Pop ServerSetup (and anything else that might be there).
        While m.stack.Count() > 0
            old = m.stack.Pop()
            m.top.RemoveChild(old)
        End While

        pushView("ContentList", {
            mode: "menu"
            serverURL: m.serverURL
        })
    End If
End Sub
