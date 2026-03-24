/* ctx-menu.js — Multi-select + context menu logic
 *
 * Loaded as a classic <script> in the browser (all declarations become globals).
 * Tests load this via vm.runInThisContext so the same globals are accessible.
 */

var _msIDs = new Set();

function msToggleMode() {
  var active = document.body.classList.toggle('multi-select-mode');
  document.getElementById('ms-select-btn').classList.toggle('active', active);
  if (!active) msClearSelection();
  document.querySelectorAll('#video-list .vid-select-cb').forEach(function(cb) {
    cb.style.display = active ? 'inline-flex' : 'none';
  });
}

function msToggle(id, li) {
  if (_msIDs.has(id)) {
    _msIDs.delete(id);
    li.classList.remove('lib-selected');
    li.querySelector('.vid-select-cb').textContent = '\u2610';
  } else {
    _msIDs.add(id);
    li.classList.add('lib-selected');
    li.querySelector('.vid-select-cb').textContent = '\u2611';
  }
  document.getElementById('ms-count').textContent = _msIDs.size + ' selected';
}

function msClearSelection() {
  _msIDs.clear();
  document.querySelectorAll('#video-list li.lib-selected').forEach(function(li) {
    li.classList.remove('lib-selected');
    var cb = li.querySelector('.vid-select-cb');
    if (cb) cb.textContent = '\u2610';
  });
  document.getElementById('ms-count').textContent = '0 selected';
}

function msBulkMove() {
  if (!_msIDs.size) return;
  fetch('/directories/options')
    .then(function(r) { return r.text(); })
    .then(function(html) {
      var wrap = document.createElement('div');
      wrap.style.cssText = 'position:fixed;top:50%;left:50%;transform:translate(-50%,-50%);background:#1c1c1c;border:1px solid #444;border-radius:8px;padding:1.25rem;z-index:9999;min-width:260px;box-shadow:0 8px 28px rgba(0,0,0,0.75)';
      wrap.innerHTML = '<h3 style="font-size:0.85rem;color:#ccc;margin:0 0 0.75rem 0">Move ' + _msIDs.size + ' video' + (_msIDs.size > 1 ? 's' : '') + ' to:</h3>'
        + '<select id="ms-bulk-dir" class="input-dark" style="width:100%;margin-bottom:0.6rem"><option value="">— choose folder —</option>' + html + '</select>'
        + '<div style="display:flex;gap:0.5rem;justify-content:flex-end">'
        + '<button class="btn-sm btn-ghost" onclick="this.closest(\'div\').parentNode.remove()">Cancel</button>'
        + '<button class="btn-sm btn-success" onclick="msBulkMoveConfirm(this.closest(\'div\'),this)">Move</button></div>';
      document.body.appendChild(wrap);
    });
}

function msBulkMoveConfirm(wrap, btn) {
  var dirID = document.getElementById('ms-bulk-dir').value;
  if (!dirID) return;
  btn.disabled = true;
  var ids = Array.from(_msIDs);
  var done = 0;
  var prog = document.getElementById('ms-progress');
  function next() {
    if (!ids.length) {
      if (prog) prog.textContent = 'Done!';
      setTimeout(function(){ if(prog) prog.textContent=''; }, 2000);
      wrap.remove();
      msClearSelection();
      htmx.ajax('GET', '/videos', {target: '#video-list', swap: 'innerHTML'});
      return;
    }
    var id = ids.shift();
    done++;
    if (prog) prog.textContent = 'Moving ' + done + '/' + (done + ids.length) + '\u2026';
    var fd = new FormData(); fd.append('dir_id', dirID);
    fetch('/videos/' + id + '/move', {method: 'POST', body: fd})
      .then(function(){ next(); })
      .catch(function(){ next(); });
  }
  next();
}

function msBulkTag() {
  if (!_msIDs.size) return;
  var name = prompt('Tag name to add to ' + _msIDs.size + ' video(s):');
  if (!name || !name.trim()) return;
  name = name.trim();
  var ids = Array.from(_msIDs);
  var done = 0;
  var prog = document.getElementById('ms-progress');
  function next() {
    if (!ids.length) {
      if (prog) prog.textContent = 'Done!';
      setTimeout(function(){ if(prog) prog.textContent=''; }, 2000);
      return;
    }
    var id = ids.shift();
    done++;
    if (prog) prog.textContent = 'Tagging ' + done + '/' + (done + ids.length) + '\u2026';
    var fd = new FormData(); fd.append('tag', name);
    fetch('/videos/' + id + '/tags', {method: 'POST', body: fd})
      .then(function(){ next(); })
      .catch(function(){ next(); });
  }
  next();
}

// ── Context menu ──────────────────────────────────────────────────────

var _ctx = {id: null, dirID: null, filename: null, dirPath: null};

function showCtxMenu(e, li) {
  e.preventDefault();
  e.stopPropagation();
  _ctx.id       = li.dataset.videoId;
  _ctx.dirID    = li.dataset.dirId;
  _ctx.filename = li.dataset.filename;
  _ctx.dirPath  = li.dataset.dirPath;

  // If multi-select is active, auto-include the right-clicked video
  if (_msIDs.size > 0 && !_msIDs.has(_ctx.id)) {
    _msIDs.add(_ctx.id);
    var cb = li.querySelector('.ms-cb');
    if (cb) cb.checked = true;
    li.classList.add('ms-selected');
  }

  var titleEl = li.querySelector('[data-title]');
  if (_msIDs.size > 0) {
    document.getElementById('ctx-title').textContent = _msIDs.size + ' videos selected';
  } else {
    document.getElementById('ctx-title').textContent = titleEl ? titleEl.dataset.title : _ctx.filename;
  }

  // Reset sub-panels
  document.getElementById('ctx-move-panel').style.display = 'none';
  document.getElementById('ctx-tag-panel').style.display  = 'none';
  document.getElementById('ctx-del-panel').style.display  = 'none';

  var menu = document.getElementById('ctx-menu');
  menu.style.left = e.clientX + 'px';
  menu.style.top  = e.clientY + 'px';
  menu.style.display = 'block';

  // Clamp to viewport after layout
  requestAnimationFrame(function() {
    var r = menu.getBoundingClientRect();
    if (r.right  > window.innerWidth  - 4) menu.style.left = (e.clientX - r.width)  + 'px';
    if (r.bottom > window.innerHeight - 4) menu.style.top  = (e.clientY - r.height) + 'px';
  });
}

function hideCtxMenu() {
  document.getElementById('ctx-menu').style.display = 'none';
}

// ── Move ──────────────────────────────────────────────────────────
function ctxMoveToggle() {
  var panel = document.getElementById('ctx-move-panel');
  document.getElementById('ctx-del-panel').style.display = 'none';
  document.getElementById('ctx-tag-panel').style.display = 'none';
  if (panel.style.display !== 'none') { panel.style.display = 'none'; return; }
  panel.innerHTML = '<div class="ctx-item" style="color:#555;cursor:default">Loading\u2026</div>';
  panel.style.display = 'block';
  fetch('/api/directories')
    .then(function(r) {
      if (!r.ok) throw new Error('HTTP ' + r.status);
      return r.json();
    })
    .then(function(dirs) {
      panel.innerHTML = '';
      if (!dirs.length) {
        panel.innerHTML = '<div class="ctx-item" style="color:#555;cursor:default">No directories registered</div>';
        return;
      }
      dirs.forEach(function(d) {
        var isCurrent = _msIDs.size > 1 ? false : String(d.id) === String(_ctx.dirID);
        var btn = document.createElement('button');
        btn.className = 'ctx-item';
        btn.style.cssText = 'width:100%;text-align:left;padding-left:1.4rem';
        btn.title = d.path;
        var label = d.path.split('/').filter(Boolean).pop() || d.path;
        if (isCurrent) {
          btn.textContent = label;
          var cur = document.createElement('span');
          cur.textContent = ' (current)';
          cur.style.cssText = 'color:#555;font-size:0.72em';
          btn.appendChild(cur);
          btn.disabled = true;
          btn.style.opacity = '0.45';
          btn.style.cursor = 'default';
        } else {
          btn.textContent = '\u229e ' + label;
          btn.onclick = function() { ctxDoMove(d.id); };
        }
        panel.appendChild(btn);
      });
    })
    .catch(function(err) {
      panel.innerHTML = '<div class="ctx-item" style="color:#f87;cursor:default">Failed to load: ' + err.message + '</div>';
    });
}

function ctxDoMove(dirID) {
  hideCtxMenu();

  // Bulk move when multi-select is active
  if (_msIDs.size > 0) {
    var ids = Array.from(_msIDs);
    var done = 0;
    var total = ids.length;
    var prog = document.getElementById('ms-progress');
    function next() {
      if (!ids.length) {
        if (prog) { prog.textContent = 'Done!'; setTimeout(function(){ prog.textContent=''; }, 2000); }
        msClearSelection();
        htmx.ajax('GET', '/videos', {target: '#video-list', swap: 'innerHTML'});
        return;
      }
      var id = ids.shift();
      done++;
      if (prog) prog.textContent = 'Moving ' + done + '/' + total + '\u2026';
      var fd = new FormData(); fd.append('dir_id', dirID);
      fetch('/videos/' + id + '/move', {method: 'POST', body: fd})
        .then(function(){ next(); })
        .catch(function(){ next(); });
    }
    next();
    return;
  }

  // Single video move (original behavior)
  var id = _ctx.id;
  var form = new FormData();
  form.append('dir_id', dirID);
  fetch('/videos/' + id + '/move', {method: 'POST', body: form})
    .then(function(r) {
      if (!r.ok) return r.text().then(function(t) { alert('Move failed: ' + t); });
      return r.text().then(function(html) {
        document.getElementById('video-list').innerHTML = html;
        htmx.process(document.getElementById('video-list'));
        applyRandDirPrefs();
      });
    })
    .catch(function(err) { alert('Move failed: ' + err.message); });
}

// ── Tag ───────────────────────────────────────────────────────────
function ctxTagToggle() {
  var panel = document.getElementById('ctx-tag-panel');
  document.getElementById('ctx-move-panel').style.display = 'none';
  document.getElementById('ctx-del-panel').style.display  = 'none';
  if (panel.style.display !== 'none') { panel.style.display = 'none'; return; }
  document.getElementById('ctx-tag-input').value = '';
  document.getElementById('ctx-tag-status').textContent = '';
  panel.style.display = 'block';
  document.getElementById('ctx-tag-input').focus();
  // Populate existing tag chips
  var chips = document.getElementById('ctx-tag-chips');
  chips.innerHTML = '';
  fetch('/api/tags')
    .then(function(r) { return r.json(); })
    .then(function(tags) {
      tags.forEach(function(t) {
        if (t.name.indexOf(':') !== -1) return; // skip system tags
        var btn = document.createElement('button');
        btn.className = 'preset-chip';
        btn.textContent = t.name;
        btn.onclick = function() { ctxDoTag(t.name); };
        chips.appendChild(btn);
      });
    });
}

function ctxDoTag(name) {
  name = (name || '').trim();
  if (!name) return;
  var id = _ctx.id;
  var status = document.getElementById('ctx-tag-status');
  var form = new FormData();
  form.append('tag', name);
  fetch('/videos/' + id + '/tags', {method: 'POST', body: form})
    .then(function(r) {
      if (!r.ok) return r.text().then(function(t) { status.style.color = '#f87'; status.textContent = t; });
      status.style.color = '#4a9';
      status.textContent = '\u2713 Tagged "' + name + '"';
      document.getElementById('ctx-tag-input').value = '';
      // Refresh the tag filter strip so the new tag appears
      htmx.ajax('GET', '/tags', {target: '#tag-filters', swap: 'innerHTML'});
    });
}

// ── Rename ────────────────────────────────────────────────────────
function ctxRename() {
  var id = _ctx.id, old = _ctx.filename, dir = _ctx.dirPath;
  hideCtxMenu();
  var newName = prompt('Rename to:', old);
  if (!newName || newName === old) return;
  var form = new FormData();
  form.append('name', newName);
  fetch('/videos/' + id + '/rename', {method: 'POST', body: form})
    .then(function(r) {
      if (!r.ok) return r.text().then(function(t) { alert('Rename failed: ' + t); });
      return r.text().then(function(html) {
        document.getElementById('video-list').innerHTML = html;
        htmx.process(document.getElementById('video-list'));
        applyRandDirPrefs();
      });
    });
}

// ── Delete ────────────────────────────────────────────────────────
function ctxDeleteToggle() {
  var panel = document.getElementById('ctx-del-panel');
  document.getElementById('ctx-move-panel').style.display = 'none';
  panel.style.display = panel.style.display !== 'none' ? 'none' : 'block';
}

function ctxDoDelete(mode) {
  var id = _ctx.id;
  hideCtxMenu();
  var url = '/videos/' + id + (mode === 'file' ? '/file' : '');
  fetch(url, {method: 'DELETE'})
    .then(function(r) { return r.text(); })
    .then(function(html) {
      document.getElementById('video-list').innerHTML = html;
      htmx.process(document.getElementById('video-list'));
      applyRandDirPrefs();
    });
}
