' components/HttpTask.brs
'
' Runs on a Task thread — safe to call blocking roUrlTransfer methods here.

Sub init()
    m.top.functionName = "runCPUTask"
End Sub

Sub runCPUTask()
    Print "HttpTask: start url="; m.top.url; " method="; m.top.method
    http = CreateObject("roUrlTransfer")
    If http = Invalid
        m.top.errMsg = "Could not create roUrlTransfer"
        Print "HttpTask: ERROR could not create roUrlTransfer"
        Return
    End If

    http.SetUrl(m.top.url)
    http.SetCertificatesFile("common:/certs/ca-bundle.crt")

    If m.top.method = "POST"
        http.AddHeader("Content-Type", "application/x-www-form-urlencoded")
        statusCode = http.PostFromString(m.top.body)
        m.top.status = statusCode
        If statusCode < 200 Or statusCode >= 300
            m.top.errMsg = "POST " + m.top.url + " returned HTTP " + statusCode.ToStr()
        End If
    Else
        ' GET
        http.AddHeader("Accept", "application/json")
        response = http.GetToString()

        If response = "" Or response = Invalid
            m.top.errMsg = "Empty response from " + m.top.url
            Return
        End If

        ' Validate the JSON before sending it back, but pass the raw string
        ' because SceneGraph has no "dynamic" field type that holds both
        ' arrays and associative arrays.
        parsed = ParseJSON(response)
        If parsed = Invalid
            m.top.errMsg = "JSON parse error from " + m.top.url + " body=" + Left(response, 120)
            Return
        End If

        Print "HttpTask: GET success, setting result"
        m.top.result = response
    End If
End Sub
