' source/api.brs
'
' HTTP helpers for the Video Manger JSON API.
' These are plain BrightScript functions (not component methods) so they can be
' included by any component via <script uri="pkg:/source/api.brs" />.
'
' All network calls are synchronous.  Call them only from Task nodes or from
' init() when the component is first loaded (never from the render thread of a
' visible component – that would block the UI).

' ---------------------------------------------------------------------------
' httpGetJSON(url As String) As Dynamic
'
' Performs an HTTP GET, parses the response body as JSON, and returns the
' parsed value (Array or AssocArray).  Returns Invalid on any error.
' ---------------------------------------------------------------------------
Function httpGetJSON(url As String) As Dynamic
    http = CreateObject("roUrlTransfer")
    If http = Invalid
        Print "api.brs: could not create roUrlTransfer"
        Return Invalid
    End If

    http.SetUrl(url)

    ' Trust the standard Roku CA bundle so HTTPS works against servers that
    ' have a valid certificate.  Also works fine for plain HTTP.
    http.SetCertificatesFile("common:/certs/ca-bundle.crt")

    ' Enable gzip for responses – reduces bandwidth on large episode lists.
    http.AddHeader("Accept-Encoding", "gzip")
    http.AddHeader("Accept", "application/json")

    response = http.GetToString()

    If response = "" Or response = Invalid
        Print "api.brs: empty response from "; url
        Return Invalid
    End If

    parsed = ParseJSON(response)
    If parsed = Invalid
        Print "api.brs: JSON parse failed for "; url; " body="; Left(response, 200)
        Return Invalid
    End If

    Return parsed
End Function

' ---------------------------------------------------------------------------
' httpPost(url As String, body As String) As Boolean
'
' Performs an HTTP POST with application/x-www-form-urlencoded body.
' Returns True on HTTP 2xx, False otherwise.
' ---------------------------------------------------------------------------
Function httpPost(url As String, body As String) As Boolean
    http = CreateObject("roUrlTransfer")
    If http = Invalid
        Print "api.brs: could not create roUrlTransfer for POST"
        Return False
    End If

    http.SetUrl(url)
    http.SetCertificatesFile("common:/certs/ca-bundle.crt")
    http.AddHeader("Content-Type", "application/x-www-form-urlencoded")

    ' PostFromString returns the HTTP status code as an integer.
    statusCode = http.PostFromString(body)

    ' Treat any 2xx code as success.
    success = (statusCode >= 200 And statusCode < 300)
    If Not success
        Print "api.brs: POST "; url; " returned status "; statusCode.ToStr()
    End If
    Return success
End Function

' ---------------------------------------------------------------------------
' urlEncode(s As String) As String
'
' Percent-encodes a string so it is safe to embed in a URL path segment or
' query value.  Uses roUrlTransfer.Escape() which follows RFC 3986.
' ---------------------------------------------------------------------------
Function urlEncode(s As String) As String
    http = CreateObject("roUrlTransfer")
    If http = Invalid
        ' Fallback: return the raw string (will break for names with spaces/
        ' special chars, but is better than crashing).
        Return s
    End If
    Return http.Escape(s)
End Function

' ---------------------------------------------------------------------------
' formatDuration(seconds As Float) As String
'
' Converts a duration in seconds to a human-readable "H:MM:SS" or "M:SS"
' string, e.g. 3723.0 → "1:02:03".
' ---------------------------------------------------------------------------
Function formatDuration(seconds As Float) As String
    total = Int(seconds)
    sec = total Mod 60
    mins = (total \ 60) Mod 60
    hrs = total \ 3600

    secStr  = Right("0" + Str(sec).Trim(), 2)
    minsStr = Right("0" + Str(mins).Trim(), 2)

    If hrs > 0
        Return Str(hrs).Trim() + ":" + minsStr + ":" + secStr
    Else
        Return Str(mins).Trim() + ":" + secStr
    End If
End Function
