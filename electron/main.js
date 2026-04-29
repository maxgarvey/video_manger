const { app, BrowserWindow, dialog } = require('electron');
const { spawn } = require('child_process');
const path = require('path');
const net = require('net');

const HTTP_PORT = 8080;
const HTTPS_PORT = 8081;
const READY_TIMEOUT_MS = 15000;

// Prevent second instances — SQLite WAL doesn't tolerate two writers.
if (!app.requestSingleInstanceLock()) {
  app.quit();
  process.exit(0);
}

let mainWindow = null;
let goProcess = null;

function getBinaryPath() {
  if (app.isPackaged) {
    return path.join(process.resourcesPath, 'video_manger');
  }
  // Development: binary is in the repo root (built by `make build`).
  return path.join(__dirname, '..', 'video_manger');
}

function getDbPath() {
  // When packaged, userData is ~/Library/Application Support/Video Manager —
  // a writable directory. cert.pem/key.pem are derived from the same dir.
  return path.join(app.getPath('userData'), 'video_manger.db');
}

function waitForPort(port) {
  return new Promise((resolve, reject) => {
    const deadline = Date.now() + READY_TIMEOUT_MS;
    function attempt() {
      const sock = net.createConnection({ port, host: '127.0.0.1' });
      sock.once('connect', () => { sock.destroy(); resolve(); });
      sock.once('error', () => {
        sock.destroy();
        if (Date.now() >= deadline) {
          reject(new Error(
            `Server did not start on port ${port} within ${READY_TIMEOUT_MS / 1000}s.\n` +
            `Port may already be in use. Check that no other instance is running.`
          ));
          return;
        }
        setTimeout(attempt, 250);
      });
    }
    attempt();
  });
}

function startGoServer() {
  const bin = getBinaryPath();
  const db = getDbPath();

  goProcess = spawn(bin, [
    `--http-port=${HTTP_PORT}`,
    `--https-port=${HTTPS_PORT}`,
    `--db=${db}`,
  ], { stdio: ['ignore', 'pipe', 'pipe'] });

  goProcess.stdout.on('data', (d) => process.stdout.write(d));
  goProcess.stderr.on('data', (d) => process.stderr.write(d));

  goProcess.on('error', (err) => {
    const msg = err.code === 'ENOENT'
      ? `Binary not found at:\n${bin}\n\nRun "make build" first.`
      : err.message;
    dialog.showErrorBox('Failed to start video_manger', msg);
    app.quit();
  });

  goProcess.on('exit', (code, signal) => {
    // Normal shutdown path: before-quit sends SIGTERM; Go exits 0 or with signal.
    if (signal === 'SIGTERM' || code === 0) return;
    dialog.showErrorBox(
      'video_manger stopped unexpectedly',
      `Exit code: ${code ?? '—'}  signal: ${signal ?? '—'}`
    );
    app.quit();
  });
}

function createWindow() {
  mainWindow = new BrowserWindow({
    width: 1400,
    height: 900,
    title: 'Video Manager',
    webPreferences: {
      nodeIntegration: false,
      contextIsolation: true,
    },
  });

  mainWindow.loadURL(`http://localhost:${HTTP_PORT}`);
  mainWindow.on('closed', () => { mainWindow = null; });
}

// Focus existing window if a second instance is launched.
app.on('second-instance', () => {
  if (!mainWindow) return;
  if (mainWindow.isMinimized()) mainWindow.restore();
  mainWindow.focus();
});

app.whenReady().then(async () => {
  startGoServer();

  try {
    await waitForPort(HTTP_PORT);
  } catch (err) {
    dialog.showErrorBox('Startup failed', err.message);
    app.quit();
    return;
  }

  createWindow();

  // macOS: re-create window when dock icon is clicked with no windows open.
  app.on('activate', () => {
    if (BrowserWindow.getAllWindows().length === 0) createWindow();
  });
});

app.on('window-all-closed', () => {
  if (process.platform !== 'darwin') app.quit();
});

app.on('before-quit', () => {
  if (goProcess) goProcess.kill('SIGTERM');
});
