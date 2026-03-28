import { beforeEach, afterEach, describe, it, expect, vi } from 'vitest';
import vm from 'node:vm';
import fs from 'node:fs';
import path from 'node:path';

const scriptSrc = fs.readFileSync(
  path.resolve(__dirname, '../../static/js/ctx-menu.js'), 'utf-8'
);

// Stub external dependencies before loading the script.
globalThis.htmx = { ajax: vi.fn(), process: vi.fn() };
globalThis.applyRandDirPrefs = vi.fn();
globalThis.fetch = vi.fn();
globalThis.alert = vi.fn();

// Mock EventSource for SSE-based bulk move tests.
function MockEventSource() {
  MockEventSource._last = this;
  this._listeners = {};
  this.close = vi.fn();
}
MockEventSource._last = null;
MockEventSource.prototype.addEventListener = function(event, fn) {
  this._listeners[event] = this._listeners[event] || [];
  this._listeners[event].push(fn);
};
MockEventSource.prototype._emit = function(event, data) {
  (this._listeners[event] || []).forEach(function(fn) { fn({data: data}); });
};
Object.defineProperty(MockEventSource.prototype, 'onmessage', {
  set: function(fn) { this._onmessage = fn; },
  get: function() { return this._onmessage; }
});
MockEventSource.prototype._dataMsg = function(data) {
  if (this._onmessage) this._onmessage({data: data});
};
globalThis.EventSource = MockEventSource;

// Load the script once — var declarations become permanent globals.
vm.runInThisContext(scriptSrc);

// Minimal DOM fixture containing the elements that the extracted functions query.
function buildFixture() {
  document.body.innerHTML = `
    <button id="ms-select-btn"></button>
    <span id="ms-count">0 selected</span>
    <span id="ms-progress"></span>
    <ul id="video-list">
      <li data-video-id="1" data-dir-id="d1" data-filename="one.mp4" data-dir-path="/movies">
        <span class="vid-select-cb" data-vid="1" style="display:none">\u2610</span>
        <span class="ms-cb"></span>
        <span data-title="Video One"></span>
      </li>
      <li data-video-id="2" data-dir-id="d1" data-filename="two.mp4" data-dir-path="/movies">
        <span class="vid-select-cb" data-vid="2" style="display:none">\u2610</span>
        <span class="ms-cb"></span>
        <span data-title="Video Two"></span>
      </li>
      <li data-video-id="3" data-dir-id="d2" data-filename="three.mp4" data-dir-path="/series">
        <span class="vid-select-cb" data-vid="3" style="display:none">\u2610</span>
        <span class="ms-cb"></span>
        <span data-title="Video Three"></span>
      </li>
    </ul>
    <div id="ctx-menu" style="display:none">
      <div id="ctx-title"></div>
      <div id="ctx-move-panel" style="display:none"></div>
      <div id="ctx-tag-panel" style="display:none">
        <input id="ctx-tag-input" />
        <span id="ctx-tag-status"></span>
        <div id="ctx-tag-chips"></div>
      </div>
      <div id="ctx-del-panel" style="display:none"></div>
    </div>
  `;
}

function fakeEvent(overrides) {
  return Object.assign({
    preventDefault: vi.fn(),
    stopPropagation: vi.fn(),
    clientX: 100,
    clientY: 200,
  }, overrides);
}

function okResponse(body) {
  return Promise.resolve({
    ok: true,
    text: function() { return Promise.resolve(body || ''); },
    json: function() { return Promise.resolve(JSON.parse(body || '[]')); },
  });
}

// ────────────────────────────────────────────────────────────────────

describe('ctx-menu.js', function() {
  beforeEach(function() {
    buildFixture();
    // Reset state
    _msIDs.clear();
    _ctx.id = null;
    _ctx.dirID = null;
    _ctx.filename = null;
    _ctx.dirPath = null;
    // Reset mocks
    vi.mocked(globalThis.htmx.ajax).mockReset();
    vi.mocked(globalThis.htmx.process).mockReset();
    vi.mocked(globalThis.applyRandDirPrefs).mockReset();
    vi.mocked(globalThis.alert).mockReset();
    vi.mocked(globalThis.fetch).mockReset();
    globalThis.fetch.mockImplementation(function() { return okResponse(); });
  });

  // ── showCtxMenu ────────────────────────────────────────────────────

  describe('showCtxMenu', function() {
    it('populates _ctx from li dataset', function() {
      var li = document.querySelector('[data-video-id="1"]');
      showCtxMenu(fakeEvent(), li);
      expect(_ctx.id).toBe(1);
      expect(_ctx.dirID).toBe('d1');
      expect(_ctx.filename).toBe('one.mp4');
      expect(_ctx.dirPath).toBe('/movies');
    });

    it('displays filename when not multi-selecting', function() {
      var li = document.querySelector('[data-video-id="1"]');
      showCtxMenu(fakeEvent(), li);
      expect(document.getElementById('ctx-title').textContent).toBe('Video One');
    });

    it('shows the context menu', function() {
      var li = document.querySelector('[data-video-id="1"]');
      showCtxMenu(fakeEvent(), li);
      expect(document.getElementById('ctx-menu').style.display).toBe('block');
    });

    it('resets sub-panels to hidden', function() {
      document.getElementById('ctx-move-panel').style.display = 'block';
      var li = document.querySelector('[data-video-id="1"]');
      showCtxMenu(fakeEvent(), li);
      expect(document.getElementById('ctx-move-panel').style.display).toBe('none');
      expect(document.getElementById('ctx-tag-panel').style.display).toBe('none');
      expect(document.getElementById('ctx-del-panel').style.display).toBe('none');
    });

    it('auto-adds right-clicked video to _msIDs when multi-select has items', function() {
      _msIDs.add(1);
      var li = document.querySelector('[data-video-id="2"]');
      showCtxMenu(fakeEvent(), li);
      expect(_msIDs.has(2)).toBe(true);
      expect(_msIDs.size).toBe(2);
    });

    it('does not duplicate if right-clicked video is already selected', function() {
      _msIDs.add(1);
      _msIDs.add(2);
      var li = document.querySelector('[data-video-id="2"]');
      showCtxMenu(fakeEvent(), li);
      expect(_msIDs.size).toBe(2);
    });

    it('displays selection count when multi-select active', function() {
      _msIDs.add(1);
      _msIDs.add(2);
      var li = document.querySelector('[data-video-id="2"]');
      showCtxMenu(fakeEvent(), li);
      expect(document.getElementById('ctx-title').textContent).toBe('2 videos selected');
    });

    it('shows count including auto-added video', function() {
      _msIDs.add(1);
      var li = document.querySelector('[data-video-id="3"]');
      showCtxMenu(fakeEvent(), li);
      expect(document.getElementById('ctx-title').textContent).toBe('2 videos selected');
    });
  });

  // ── hideCtxMenu ────────────────────────────────────────────────────

  describe('hideCtxMenu', function() {
    it('hides the context menu', function() {
      document.getElementById('ctx-menu').style.display = 'block';
      hideCtxMenu();
      expect(document.getElementById('ctx-menu').style.display).toBe('none');
    });
  });

  // ── ctxMoveToggle ──────────────────────────────────────────────────

  describe('ctxMoveToggle', function() {
    it('toggles move panel visibility', function() {
      ctxMoveToggle();
      expect(document.getElementById('ctx-move-panel').style.display).toBe('block');
      ctxMoveToggle();
      expect(document.getElementById('ctx-move-panel').style.display).toBe('none');
    });

    it('hides other panels when opening', function() {
      document.getElementById('ctx-del-panel').style.display = 'block';
      document.getElementById('ctx-tag-panel').style.display = 'block';
      ctxMoveToggle();
      expect(document.getElementById('ctx-del-panel').style.display).toBe('none');
      expect(document.getElementById('ctx-tag-panel').style.display).toBe('none');
    });

    it('marks current directory as disabled for single video', async function() {
      _ctx.dirID = '10';
      globalThis.fetch.mockImplementation(function() {
        return Promise.resolve({
          ok: true,
          json: function() { return Promise.resolve([{id: 10, path: '/movies'}, {id: 20, path: '/series'}]); }
        });
      });
      ctxMoveToggle();
      await vi.waitFor(function() {
        var btns = document.getElementById('ctx-move-panel').querySelectorAll('button');
        expect(btns.length).toBe(2);
      });
      var btns = document.getElementById('ctx-move-panel').querySelectorAll('button');
      expect(btns[0].disabled).toBe(true);
      expect(btns[0].textContent).toContain('(current)');
      expect(btns[1].disabled).toBe(false);
    });

    it('does not mark any directory as current during multi-select (size > 1)', async function() {
      _ctx.dirID = '10';
      _msIDs.add(1);
      _msIDs.add(2);
      globalThis.fetch.mockImplementation(function() {
        return Promise.resolve({
          ok: true,
          json: function() { return Promise.resolve([{id: 10, path: '/movies'}, {id: 20, path: '/series'}]); }
        });
      });
      ctxMoveToggle();
      await vi.waitFor(function() {
        var btns = document.getElementById('ctx-move-panel').querySelectorAll('button');
        expect(btns.length).toBe(2);
      });
      var btns = document.getElementById('ctx-move-panel').querySelectorAll('button');
      expect(btns[0].disabled).toBe(false);
      expect(btns[1].disabled).toBe(false);
    });

    it('shows error on fetch failure', async function() {
      globalThis.fetch.mockImplementation(function() {
        return Promise.reject(new Error('Network error'));
      });
      ctxMoveToggle();
      await vi.waitFor(function() {
        expect(document.getElementById('ctx-move-panel').innerHTML).toContain('Failed to load');
      });
      expect(document.getElementById('ctx-move-panel').innerHTML).toContain('Network error');
    });
  });

  // ── ctxDoMove — bulk ───────────────────────────────────────────────

  describe('ctxDoMove - bulk move', function() {
    it('sends a single bulk-move request with all video IDs', async function() {
      _msIDs.add(1);
      _msIDs.add(2);
      _msIDs.add(3);
      var capturedURL, capturedBody;
      globalThis.fetch.mockImplementation(function(url, opts) {
        capturedURL = url;
        capturedBody = opts ? opts.body : null;
        return okResponse('{"job_id":"test-job"}');
      });
      ctxDoMove('99');
      await vi.waitFor(function() {
        expect(capturedURL).toBe('/videos/bulk-move');
      });
      expect(capturedBody.get('dir_id')).toBe('99');
      var ids = capturedBody.get('video_ids').split(',');
      expect(ids).toContain('1');
      expect(ids).toContain('2');
      expect(ids).toContain('3');
    });

    it('passes dir_id in FormData', async function() {
      _msIDs.add(1);
      var capturedBody;
      globalThis.fetch.mockImplementation(function(url, opts) {
        capturedBody = opts.body;
        return okResponse('{"job_id":"test-job"}');
      });
      ctxDoMove('42');
      await vi.waitFor(function() {
        expect(capturedBody).toBeDefined();
      });
      expect(capturedBody.get('dir_id')).toBe('42');
    });

    it('connects EventSource to the job events endpoint', async function() {
      _msIDs.add(1);
      MockEventSource._last = null;
      globalThis.fetch.mockImplementation(function() {
        return okResponse('{"job_id":"abc123"}');
      });
      ctxDoMove('99');
      await vi.waitFor(function() {
        expect(MockEventSource._last).not.toBeNull();
      });
    });

    it('shows per-file progress from SSE messages', async function() {
      _msIDs.add(1);
      MockEventSource._last = null;
      var prog = document.getElementById('ms-progress');
      globalThis.fetch.mockImplementation(function() {
        return okResponse('{"job_id":"abc123"}');
      });
      ctxDoMove('99');
      await vi.waitFor(function() {
        expect(MockEventSource._last).not.toBeNull();
      });
      MockEventSource._last._dataMsg('Moving 1/1: test.mp4');
      expect(prog.textContent).toBe('Moving 1/1: test.mp4');
    });

    it('clears selection after done event', async function() {
      _msIDs.add(1);
      MockEventSource._last = null;
      globalThis.fetch.mockImplementation(function() {
        return okResponse('{"job_id":"abc123"}');
      });
      ctxDoMove('99');
      await vi.waitFor(function() {
        expect(MockEventSource._last).not.toBeNull();
      });
      MockEventSource._last._emit('done', '{"moved":1,"fails":0,"total":1}');
      expect(_msIDs.size).toBe(0);
    });

    it('calls htmx.ajax to refresh video list after done event', async function() {
      _msIDs.add(1);
      MockEventSource._last = null;
      globalThis.fetch.mockImplementation(function() {
        return okResponse('{"job_id":"abc123"}');
      });
      ctxDoMove('99');
      await vi.waitFor(function() {
        expect(MockEventSource._last).not.toBeNull();
      });
      MockEventSource._last._emit('done', '{"moved":1,"fails":0,"total":1}');
      expect(globalThis.htmx.ajax).toHaveBeenCalledWith(
        'GET', '/videos', {target: '#video-list', swap: 'innerHTML'}
      );
    });

    it('shows progress text before request completes', async function() {
      _msIDs.add(1);
      _msIDs.add(2);
      var prog = document.getElementById('ms-progress');
      globalThis.fetch.mockImplementation(function() {
        return okResponse('{"job_id":"abc123"}');
      });
      ctxDoMove('99');
      // Progress text is set synchronously before the fetch resolves
      expect(prog.textContent).toContain('2 videos');
    });

    it('shows fail count from done event', async function() {
      _msIDs.add(1);
      _msIDs.add(2);
      MockEventSource._last = null;
      var prog = document.getElementById('ms-progress');
      globalThis.fetch.mockImplementation(function() {
        return okResponse('{"job_id":"abc123"}');
      });
      ctxDoMove('99');
      await vi.waitFor(function() {
        expect(MockEventSource._last).not.toBeNull();
      });
      MockEventSource._last._emit('done', '{"moved":1,"fails":1,"total":2}');
      expect(prog.textContent).toContain('1 failed');
    });

    it('hides the context menu', function() {
      _msIDs.add(1);
      document.getElementById('ctx-menu').style.display = 'block';
      ctxDoMove('99');
      expect(document.getElementById('ctx-menu').style.display).toBe('none');
    });
  });

  // ── ctxDoMove — single ─────────────────────────────────────────────

  describe('ctxDoMove - single move', function() {
    it('moves single video when _msIDs is empty', async function() {
      _ctx.id = 1;
      var calledUrl;
      globalThis.fetch.mockImplementation(function(url) {
        calledUrl = url;
        return okResponse('<div>new list</div>');
      });
      ctxDoMove('55');
      await vi.waitFor(function() {
        expect(calledUrl).toBeDefined();
      });
      expect(calledUrl).toBe('/videos/1/move');
    });

    it('calls applyRandDirPrefs after success', async function() {
      _ctx.id = 1;
      globalThis.fetch.mockImplementation(function() {
        return Promise.resolve({
          ok: true,
          text: function() { return Promise.resolve('<div>updated</div>'); }
        });
      });
      ctxDoMove('55');
      await vi.waitFor(function() {
        expect(globalThis.applyRandDirPrefs).toHaveBeenCalled();
      });
    });

    it('updates video-list innerHTML on success', async function() {
      _ctx.id = 1;
      globalThis.fetch.mockImplementation(function() {
        return Promise.resolve({
          ok: true,
          text: function() { return Promise.resolve('<li>refreshed</li>'); }
        });
      });
      ctxDoMove('55');
      await vi.waitFor(function() {
        expect(document.getElementById('video-list').innerHTML).toBe('<li>refreshed</li>');
      });
    });
  });

  // ── msClearSelection ───────────────────────────────────────────────

  describe('msClearSelection', function() {
    it('clears _msIDs set', function() {
      _msIDs.add(1);
      _msIDs.add(2);
      msClearSelection();
      expect(_msIDs.size).toBe(0);
    });

    it('removes lib-selected class from all li elements', function() {
      var li = document.querySelector('[data-video-id="1"]');
      li.classList.add('lib-selected');
      msClearSelection();
      expect(li.classList.contains('lib-selected')).toBe(false);
    });

    it('resets checkbox text', function() {
      var cb = document.querySelector('[data-video-id="1"] .vid-select-cb');
      cb.textContent = '\u2611';
      var li = document.querySelector('[data-video-id="1"]');
      li.classList.add('lib-selected');
      msClearSelection();
      expect(cb.textContent).toBe('\u2610');
    });

    it('resets count display', function() {
      document.getElementById('ms-count').textContent = '3 selected';
      msClearSelection();
      expect(document.getElementById('ms-count').textContent).toBe('0 selected');
    });
  });

  // ── msToggle ───────────────────────────────────────────────────────

  describe('msToggle', function() {
    it('adds video to selection', function() {
      var li = document.querySelector('[data-video-id="1"]');
      msToggle(1, li);
      expect(_msIDs.has(1)).toBe(true);
      expect(li.classList.contains('lib-selected')).toBe(true);
    });

    it('removes video from selection on second call', function() {
      var li = document.querySelector('[data-video-id="1"]');
      msToggle(1, li);
      msToggle(1, li);
      expect(_msIDs.has(1)).toBe(false);
      expect(li.classList.contains('lib-selected')).toBe(false);
    });

    it('updates count display', function() {
      var li1 = document.querySelector('[data-video-id="1"]');
      var li2 = document.querySelector('[data-video-id="2"]');
      msToggle(1, li1);
      expect(document.getElementById('ms-count').textContent).toBe('1 selected');
      msToggle(2, li2);
      expect(document.getElementById('ms-count').textContent).toBe('2 selected');
    });
  });
});
