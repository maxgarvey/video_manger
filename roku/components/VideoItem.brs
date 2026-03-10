' components/VideoItem.brs
' ItemComponent for the MarkupGrid in ContentList.

Sub init()
    m.titleLbl = m.top.FindNode("titleLbl")
    m.thumb    = m.top.FindNode("thumb")
    m.focusBg  = m.top.FindNode("focusBg")
End Sub

Sub onContentSet()
    content = m.top.itemContent
    If content = Invalid Then Return

    m.titleLbl.text = content.title

    thumbURL = content.HDGRIDPOSTERURL
    If thumbURL <> Invalid And thumbURL <> ""
        m.thumb.uri = thumbURL
    Else
        m.thumb.uri = ""
    End If
End Sub

Sub onFocusChange()
    If m.top.focusPercent > 0.5
        m.focusBg.color  = "0x1a3a5aFF"
        m.titleLbl.color = "0xFFFFFFFF"
    Else
        m.focusBg.color  = "0x00000000"
        m.titleLbl.color = "0xCCCCCCFF"
    End If
End Sub
