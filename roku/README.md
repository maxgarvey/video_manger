# Video Manger – Roku Channel

A BrightScript SceneGraph channel for browsing and playing your personal video
library via the Video Manger Go server.

---

## Requirements

- A Roku device with **developer mode enabled** (any model with Roku OS 7.2+)
- The Video Manger server running and accessible on your LAN
- The server started **without** a password, or with a password — but note that
  the Roku channel does not currently support cookie-based authentication, so
  **run the server without `-password`** for Roku access:
  ```
  ./video_manger -db video_manger.db -dir /path/to/videos
  ```

---

## Sideloading the Channel

Sideloading packages the `roku/` directory as a ZIP and uploads it to your
Roku's developer console.

### Step 1 – Enable Developer Mode on your Roku

1. On your Roku remote press: **Home × 3, Up × 2, Right, Left, Right, Left, Right**
2. Select **Enable installer and restart**.
3. After restart, note the Roku's **IP address** (Settings → Network → About).

### Step 2 – Create placeholder images

The manifest references icon and splash images.  See `images/PLACEHOLDER.txt`
for exact dimensions and ImageMagick commands to generate minimal placeholders.

Required files:
```
roku/images/icon_hd.png    (540 × 405 px)
roku/images/icon_sd.png    (290 × 218 px)   ← optional but recommended
roku/images/splash_fhd.png (1920 × 1080 px)
roku/images/splash_hd.png  (1280 × 720 px)  ← optional
```

### Step 3 – Package and upload

From the repository root:

```bash
cd roku
zip -r ../video_manger_roku.zip . -x "*.DS_Store" -x "__MACOSX/*"
```

Then open **http://<roku-ip>** in your browser, log in with the developer
credentials you set (default username `rokudev`, password you chose), and
upload `video_manger_roku.zip` via the **Package Installer** page.

### Step 4 – Launch the channel

The channel appears in the channel list under **My Channels**.  Open it and
follow the on-screen setup prompt.

---

## First-Run: Setting the Server URL

The first time the channel launches it will show a keyboard dialog asking for
the server URL.  Enter the full base URL of your Video Manger server, e.g.:

```
http://192.168.1.100:8080
```

Tips:
- Include the port number.
- Do **not** add a trailing slash.
- The URL is saved to the Roku registry and persists across channel restarts.
- To change it later, clear the channel's registry by uninstalling and
  reinstalling, or add a "Change Server" option to the menu (future work).

The server registers itself via mDNS as `video-manger.local`, but Roku devices
do not resolve mDNS names — use the numeric IP address instead.

---

## Navigation

| Action | Remote Button |
|--------|---------------|
| Select item | OK / Select |
| Go back | Back |
| Scroll list | Up / Down |
| Play/pause | Play |
| Seek | Rewind / Fast-forward |

---

## File Structure

```
roku/
├── manifest                        Channel metadata
├── README.md                       This file
├── images/
│   ├── PLACEHOLDER.txt             Image size requirements
│   ├── icon_hd.png                 (you supply this)
│   ├── icon_sd.png                 (you supply this)
│   ├── splash_fhd.png              (you supply this)
│   └── splash_hd.png               (you supply this)
├── source/
│   ├── main.brs                    Channel entry-point (roSGScreen loop)
│   └── api.brs                     HTTP helpers: httpGetJSON, httpPost, urlEncode
└── components/
    ├── MainScene.xml / .brs        Root scene, navigation stack manager
    ├── ContentList.xml / .brs      Reusable list view (menu/shows/seasons/episodes/videos/recent)
    ├── Player.xml / .brs           Video playback + progress saving
    └── ServerSetup.xml / .brs      First-run URL entry keyboard dialog
```

---

## API Endpoints Used

| Endpoint | Used by |
|----------|---------|
| `GET /api/videos` | ContentList (videos mode) |
| `GET /api/videos?type=Movie` | ContentList (movies mode) |
| `GET /api/random` | ContentList (menu → Random) |
| `GET /api/shows` | ContentList (shows mode) |
| `GET /api/shows/{show}/seasons` | ContentList (seasons mode) |
| `GET /api/shows/{show}/seasons/{season}/episodes` | ContentList (episodes mode) |
| `GET /api/recently-watched` | ContentList (recent mode) |
| `GET /video/{id}` | Player (video stream) |
| `GET /videos/{id}/thumbnail` | Player (poster art) |
| `POST /videos/{id}/progress` | Player (every 10 s + on back/finish) |
| `POST /videos/{id}/watched` | Player (on natural finish) |

---

## Troubleshooting

**"No videos found" on every screen**
- Confirm the server IP and port are correct.
- Test from a browser on the same network: `http://<ip>:<port>/api/videos`

**Channel crashes on launch**
- Ensure `icon_hd.png` and `splash_fhd.png` exist in `roku/images/`.

**Video won't play**
- Roku requires video streams to support **HTTP Range requests** for seeking.
  The Video Manger server (`/video/{id}`) uses `http.ServeFile` which supports
  Range requests natively.
- Roku does **not** play all codecs.  Supported: H.264 (MP4/MKV), H.265, VP9,
  AV1, AAC/MP3/AC3 audio.  Convert unsupported files with the server's built-in
  ffmpeg converter.

**Resume position not saving**
- Check that the server is reachable for POST requests (same URL as GET).
- The server must not be running with password protection unless you add
  session cookie support to the Roku channel.
