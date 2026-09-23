/* UdiAgenda — logica del pannello.
   Comunica con il programma Go tramite WebView2 (window.chrome.webview)
   oppure, in fase di sviluppo su altri sistemi, tramite HTTP. */

(function () {
  "use strict";

  // ---------------------------------------------------------------- ponte
  var pending = {};
  var seq = 0;
  var useWebView = !!(window.chrome && window.chrome.webview && window.chrome.webview.postMessage);

  window.__udiagendaReply = function (id, ok, payload) {
    var p = pending[id];
    if (!p) return;
    delete pending[id];
    if (ok) p.resolve(payload);
    else p.reject(new Error(String(payload)));
  };

  function call(method, params) {
    if (useWebView) {
      return new Promise(function (resolve, reject) {
        var id = ++seq;
        pending[id] = { resolve: resolve, reject: reject };
        window.chrome.webview.postMessage(JSON.stringify({ id: id, method: method, params: params === undefined ? null : params }));
        setTimeout(function () {
          if (pending[id]) { delete pending[id]; reject(new Error("nessuna risposta dal programma")); }
        }, 15000);
      });
    }
    return fetch("/rpc/" + method, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(params === undefined ? null : params)
    }).then(function (r) { return r.json(); }).then(function (res) {
      if (res && res.ok) return res.data;
      throw new Error((res && res.error) || "errore sconosciuto");
    });
  }

  // ---------------------------------------------------------------- utilità
  var $ = function (s) { return document.querySelector(s); };
  var state = null;
  var editingId = 0;

  function esc(s) {
    return String(s == null ? "" : s)
      .replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;").replace(/'/g, "&#39;");
  }

  function isoDate(d) {
    var p = function (n) { return (n < 10 ? "0" : "") + n; };
    return d.getFullYear() + "-" + p(d.getMonth() + 1) + "-" + p(d.getDate());
  }

  function plusDays(n) {
    var d = new Date();
    d.setHours(12, 0, 0, 0);
    d.setDate(d.getDate() + n);
    return isoDate(d);
  }

  var toastTimer = null;
  function toast(msg) {
    var el = $("#toast");
    el.textContent = msg;
    el.hidden = false;
    clearTimeout(toastTimer);
    toastTimer = setTimeout(function () { el.hidden = true; }, 2200);
  }

  // ---------------------------------------------------------------- tema
  function applyTheme(s) {
    var t = (s && s.settings && s.settings.theme) || "auto";
    var dark;
    if (t === "dark") dark = true;
    else if (t === "light") dark = false;
    else if (useWebView && s && typeof s.systemDark === "boolean") dark = s.systemDark;
    else dark = window.matchMedia && window.matchMedia("(prefers-color-scheme: dark)").matches;
    document.documentElement.setAttribute("data-theme", dark ? "dark" : "light");
    var acc = (s && s.settings && s.settings.accent) || "blue";
    document.documentElement.setAttribute("data-accent", acc);
  }
  if (window.matchMedia) {
    var mq = window.matchMedia("(prefers-color-scheme: dark)");
    if (mq.addEventListener) mq.addEventListener("change", function () { applyTheme(state); });
  }

  // ---------------------------------------------------------------- render
  var ICON_EDIT = '<svg viewBox="0 0 20 20" aria-hidden="true"><path d="M13.9 3.1a1.9 1.9 0 0 1 2.7 2.7l-.9.9-2.7-2.7.9-.9Zm-1.7 1.8 2.7 2.7-6.6 6.6c-.2.2-.4.3-.6.4l-3 .9a.5.5 0 0 1-.6-.6l.9-3c.1-.2.2-.5.4-.6l6.8-6.4Z"/></svg>';
  var ICON_PLUS = '<svg viewBox="0 0 20 20" aria-hidden="true"><path d="M9.25 4.75a.75.75 0 0 1 1.5 0v4.5h4.5a.75.75 0 0 1 0 1.5h-4.5v4.5a.75.75 0 0 1-1.5 0v-4.5h-4.5a.75.75 0 0 1 0-1.5h4.5v-4.5Z"/></svg>';
  var ICON_DEL = '<svg viewBox="0 0 20 20" aria-hidden="true"><path d="M8.2 3h3.6a1 1 0 0 1 1 1v.6h3a.7.7 0 1 1 0 1.4h-.6l-.8 8.5a2.2 2.2 0 0 1-2.2 2H7.8a2.2 2.2 0 0 1-2.2-2L4.8 6h-.6a.7.7 0 0 1 0-1.4h3V4a1 1 0 0 1 1-1Zm-2 3 .8 8.4c.04.4.4.7.8.7h4.4c.4 0 .76-.3.8-.7L13.8 6H6.2Zm2.4-1.4h2.8V4.4H8.6v.2Z"/></svg>';

  function cardHTML(t, done) {
    var meta = [];
    meta.push('<span>' + esc(t.dateLabel) + '</span>');
    if (t.category) meta.push('<span class="tag">' + esc(t.category) + '</span>');
    return '' +
      '<div class="card lv-' + esc(t.level) + (done ? ' done' : '') + '" data-id="' + t.id + '">' +
        '<button class="check" title="' + (done ? 'Riporta tra le attività da fare' : 'Segna come completata') + '" aria-label="Completa"></button>' +
        '<div class="card-body">' +
          (done ? '' : '<div class="badge">' + esc(t.badge) + '</div>') +
          '<div class="card-title">' + esc(t.title) + '</div>' +
          '<div class="meta">' + meta.join("") + '</div>' +
          (t.note ? '<div class="note">' + esc(t.note) + '</div>' : '') +
        '</div>' +
        '<div class="card-actions">' +
          (done ? '' : '<button class="icon-btn edit" title="Modifica" aria-label="Modifica">' + ICON_EDIT + '</button>') +
          '<button class="icon-btn del" title="Elimina" aria-label="Elimina">' + ICON_DEL + '</button>' +
        '</div>' +
      '</div>';
  }

  // ------------------------------------------------------------- sezioni
  var SECTIONS = {
    compiti:   { title: "AGENDA COMPITI",   add: "Nuova attività", kind: "compito" },
    verifiche: { title: "AGENDA VERIFICHE", add: "Nuova verifica", kind: "verifica" },
    materie:   { title: "REGISTRO MATERIE", add: "Nuovo voto",     kind: "" }
  };
  var section = "compiti";
  try {
    var saved = localStorage.getItem("udiagenda.section");
    if (saved && SECTIONS[saved]) section = saved;
  } catch (e) { /* archiviazione non disponibile: si parte dai compiti */ }

  function setSection(name) {
    if (!SECTIONS[name]) return;
    section = name;
    try { localStorage.setItem("udiagenda.section", name); } catch (e) {}
    if (state) render(state);
    $("#list").scrollTop = 0;
  }

  function render(s) {
    state = s;
    applyTheme(s);

    var conf = SECTIONS[section] || SECTIONS.compiti;
    $("#head-title").textContent = conf.title;
    $("#add-label").textContent = conf.add;
    Array.prototype.forEach.call(document.querySelectorAll("#tabs .tab"), function (b) {
      b.classList.toggle("on", b.dataset.sec === section);
    });
    tabCount("#n-compiti", s.counts.compiti);
    tabCount("#n-verifiche", s.counts.verifiche);
    tabCount("#n-materie", s.counts.grades);

    if (section === "materie") renderRegistro(s);
    else renderTasks(s, conf.kind);

    renderSettings(s);
  }

  function tabCount(sel, n) {
    var el = $(sel);
    el.textContent = n > 0 ? n : "";
    el.classList.toggle("show", n > 0);
  }

  function ofKind(list, kind) {
    return (list || []).filter(function (t) { return (t.kind || "compito") === kind; });
  }

  // ------------------------------------------------- agende compiti/verifiche
  function renderTasks(s, kind) {
    var tasks = ofKind(s.tasks, kind);
    var done = ofKind(s.completed, kind);
    var overdue = tasks.filter(function (t) { return t.level === "overdue"; }).length;
    var verifica = kind === "verifica";

    var parts = [s.todayLabel];
    if (!tasks.length) parts.push(verifica ? "nessuna verifica" : "niente in sospeso");
    else if (tasks.length === 1) parts.push(verifica ? "1 verifica" : "1 impegno");
    else parts.push(tasks.length + (verifica ? " verifiche" : " impegni"));
    if (overdue > 0) parts.push(overdue === 1 ? "1 scaduto" : overdue + " scaduti");
    $("#subtitle").textContent = parts.join(" · ");

    var html = "";
    if (!tasks.length) {
      html = '<div class="empty"><div class="big">✓</div><p>' +
        (verifica ? "Nessuna verifica in programma." : "Nessun impegno in programma.") +
        '<br>Premi <b>' + esc(SECTIONS[section].add) + '</b> per aggiungerne una.</p></div>';
    } else {
      var lastLevel = null;
      var counts = {};
      tasks.forEach(function (t) { counts[t.level] = (counts[t.level] || 0) + 1; });
      tasks.forEach(function (t) {
        if (t.level !== lastLevel) {
          lastLevel = t.level;
          html += '<div class="group-head lv-' + esc(t.level) + '" style="--lv:var(--' + esc(t.level) + ')">' +
                  '<span class="dot"></span>' + esc(t.groupTitle) +
                  '<span class="n">' + counts[t.level] + '</span></div>';
        }
        html += cardHTML(t, false);
      });
    }

    if (s.settings.showCompleted && done.length) {
      html += '<div class="divider">COMPLETATI</div>';
      done.forEach(function (t) { html += cardHTML(t, true); });
    }
    $("#list").innerHTML = html;
  }

  // ------------------------------------------------------- registro materie
  function renderRegistro(s) {
    var subs = s.subjects || [];
    $("#subtitle").textContent =
      (s.counts.subjects === 1 ? "1 materia" : s.counts.subjects + " materie") + " · " +
      (s.counts.grades === 1 ? "1 voto" : s.counts.grades + " voti");

    var html = "";
    if (s.averageLabel) {
      html += '<div class="avg-banner g-' + esc(s.averageClass) + '">' +
        '<span class="avg-circle">' + esc(s.averageLabel) + '</span>' +
        '<span class="avg-text"><b>Media generale</b><span>' +
        s.counts.grades + (s.counts.grades === 1 ? " voto in " : " voti in ") +
        s.counts.subjects + (s.counts.subjects === 1 ? " materia" : " materie") +
        '</span></span></div>';
    }
    if (!subs.length) {
      html += '<div class="empty"><div class="big">＋</div><p>Nessuna materia.<br>' +
        'Premi <b>Nuovo voto</b>: scrivi la materia e viene creata da sola.</p></div>';
    }
    subs.forEach(function (m) {
      var sub = m.teacher ? m.teacher : (m.count === 1 ? "1 voto" : m.count + " voti");
      html += '<div class="subject" data-sid="' + m.id + '">' +
        '<div class="subject-head g-' + esc(m.class) + '">' +
          '<span class="avg-circle' + (m.averageLabel ? '' : ' empty') + '">' +
            esc(m.averageLabel || "–") + '</span>' +
          '<div class="subject-text">' +
            '<span class="subject-name">' + esc(m.name) + '</span>' +
            '<span class="subject-sub">' + esc(sub) + '</span>' +
          '</div>' +
          '<div class="subject-actions">' +
            '<button class="icon-btn add-grade" title="Aggiungi un voto" aria-label="Aggiungi un voto">' + ICON_PLUS + '</button>' +
            '<button class="icon-btn ren" title="Rinomina o elimina" aria-label="Rinomina">' + ICON_EDIT + '</button>' +
          '</div>' +
        '</div>';
      if (m.grades.length) {
        html += '<div class="grades">';
        m.grades.forEach(function (g) {
          html += '<div class="grade-row g-' + esc(g.class) + '" data-gid="' + g.id + '">' +
            '<span class="gv">' + esc(g.label) + '</span>' +
            '<div class="grade-body"><span>' + esc(g.dateLabel) + '</span>' +
              (g.type ? '<span class="tag">' + esc(g.type) + '</span>' : '') +
              (g.note ? '<span class="grade-note">' + esc(g.note) + '</span>' : '') +
            '</div>' +
            '<div class="grade-actions">' +
              '<button class="icon-btn edit-grade" title="Modifica" aria-label="Modifica">' + ICON_EDIT + '</button>' +
              '<button class="icon-btn del del-grade" title="Elimina" aria-label="Elimina">' + ICON_DEL + '</button>' +
            '</div></div>';
        });
        html += '</div>';
      }
      html += '</div>';
    });
    $("#list").innerHTML = html;
  }

  // ------------------------------------------------------------ impostazioni
  function renderSettings(s) {
    var cats = {};
    (s.tasks || []).concat(s.completed || []).forEach(function (t) { if (t.category) cats[t.category] = 1; });
    (s.subjects || []).forEach(function (m) { cats[m.name] = 1; });
    $("#categories").innerHTML = Object.keys(cats).map(function (c) {
      return '<option value="' + esc(c) + '">';
    }).join("");
    $("#subjects").innerHTML = (s.subjects || []).map(function (m) {
      return '<option value="' + esc(m.name) + '">';
    }).join("");

    $("#s-autostart").checked = !!s.settings.startWithWindows;
    $("#s-completed").checked = !!s.settings.showCompleted;
    $("#s-hidetab").checked = !!s.settings.hideTab;
    seg("#s-theme", s.settings.theme);
    seg("#s-accent", s.settings.accent);
    var swOn = document.querySelector("#s-accent button.on");
    $("#s-accent-name").textContent = swOn ? swOn.title : "";
    seg("#s-side", s.settings.tabSide);
    seg("#s-pos", s.settings.tabPosition);
    $("#s-width").value = s.settings.panelWidth;
    $("#s-width-val").textContent = s.settings.panelWidth + " px";
    $("#about").textContent = "UdiAgenda " + s.version + " · dati salvati in " + s.dataDir;
  }

  function seg(sel, value) {
    var box = $(sel);
    Array.prototype.forEach.call(box.querySelectorAll("button"), function (b) {
      b.classList.toggle("on", b.dataset.v === value);
    });
  }

  function refresh() { return call("state").then(render); }

  function mutate(method, params) {
    return call(method, params).then(render).catch(function (e) { toast(e.message); refresh(); });
  }

  // ---------------------------------------------------------------- schede
  function openSheet(sel) {
    closeSheets();
    var el = $(sel);
    el.hidden = false;
    el.setAttribute("aria-hidden", "false");
    // finché la scheda è aperta il pannello non si richiude da solo
    if (useWebView) call("setModal", { open: true });
  }
  function closeSheets() {
    ["#sheet-task", "#sheet-settings", "#sheet-grade", "#sheet-subject"].forEach(function (s) {
      var el = $(s);
      el.hidden = true;
      el.setAttribute("aria-hidden", "true");
    });
    resetDeleteSubject();
    if (useWebView) call("setModal", { open: false });
  }
  function sheetOpen() {
    return ["#sheet-task", "#sheet-settings", "#sheet-grade", "#sheet-subject"].some(function (s) {
      return !$(s).hidden;
    });
  }

  function openTaskSheet(task) {
    editingId = task ? task.id : 0;
    $("#sheet-task-title").textContent = task ? "Modifica attività" : "Nuova attività";
    $("#btn-save").textContent = task ? "Salva modifiche" : "Salva";
    seg("#f-kind", task ? (task.kind || "compito") : (SECTIONS[section].kind || "compito"));
    $("#f-title").value = task ? task.title : "";
    $("#f-date").value = task ? task.dueDate : plusDays(0);
    $("#f-time").value = task ? (task.dueTime || "") : "";
    $("#f-category").value = task ? (task.category || "") : "";
    $("#f-note").value = task ? (task.note || "") : "";
    $("#form-error").hidden = true;
    var extra = !!(task && (task.dueTime || task.category || task.note));
    $("#more").hidden = !extra;
    $("#btn-more").setAttribute("aria-expanded", extra ? "true" : "false");
    syncChips();
    openSheet("#sheet-task");
    setTimeout(function () { $("#f-title").focus(); }, 30);
  }

  function syncChips() {
    var v = $("#f-date").value;
    Array.prototype.forEach.call(document.querySelectorAll("#date-chips .chip"), function (c) {
      c.classList.toggle("on", plusDays(parseInt(c.dataset.days, 10)) === v);
    });
  }

  // ------------------------------------------------------- schede del registro
  var editingGrade = 0;
  var editingSubject = 0;
  var confirmDelete = false;

  function findSubject(id) {
    var found = null;
    (state && state.subjects || []).forEach(function (m) { if (m.id === id) found = m; });
    return found;
  }
  function findGrade(id) {
    var found = null;
    (state && state.subjects || []).forEach(function (m) {
      m.grades.forEach(function (g) { if (g.id === id) found = { grade: g, subject: m }; });
    });
    return found;
  }

  function syncGradeChips() {
    var v = ($("#g-value").value || "").replace(",", ".");
    Array.prototype.forEach.call(document.querySelectorAll("#grade-chips .gchip"), function (c) {
      c.classList.toggle("on", c.dataset.v === v);
    });
  }

  function openGradeSheet(g, subjectName) {
    editingGrade = g ? g.id : 0;
    $("#sheet-grade-title").textContent = g ? "Modifica voto" : "Nuovo voto";
    $("#btn-save-grade").textContent = g ? "Salva modifiche" : "Salva";
    $("#g-subject").value = subjectName || "";
    $("#g-value").value = g ? String(g.value) : "";
    $("#g-date").value = g ? g.date : plusDays(0);
    $("#g-note").value = g ? (g.note || "") : "";
    seg("#g-type", g ? (g.type || "") : "");
    $("#grade-error").hidden = true;
    syncGradeChips();
    openSheet("#sheet-grade");
    setTimeout(function () {
      ($("#g-subject").value ? $("#g-value") : $("#g-subject")).focus();
    }, 30);
  }

  function openGradeSheetById(id) {
    var f = findGrade(id);
    if (f) openGradeSheet(f.grade, f.subject.name);
  }

  function openSubjectSheet(id) {
    var m = findSubject(id);
    if (!m) return;
    editingSubject = id;
    $("#m-name").value = m.name;
    $("#m-teacher").value = m.teacher || "";
    $("#subject-error").hidden = true;
    resetDeleteSubject();
    openSheet("#sheet-subject");
    setTimeout(function () { $("#m-name").focus(); $("#m-name").select(); }, 30);
  }

  function resetDeleteSubject() {
    confirmDelete = false;
    var b = $("#btn-del-subject");
    if (b) b.textContent = "Elimina materia e voti";
  }

  function showErr(sel, msg) {
    var el = $(sel);
    el.textContent = msg;
    el.hidden = false;
  }

  // ---------------------------------------------------------------- eventi
  document.addEventListener("click", function (ev) {
    var el;

    if ((el = ev.target.closest(".js-close-sheet"))) { closeSheets(); return; }

    if ((el = ev.target.closest(".tab"))) { setSection(el.dataset.sec); return; }

    if ((el = ev.target.closest(".gchip"))) {
      $("#g-value").value = el.dataset.v;
      syncGradeChips();
      return;
    }

    if ((el = ev.target.closest(".chip"))) {
      $("#f-date").value = plusDays(parseInt(el.dataset.days, 10));
      syncChips();
      return;
    }

    if ((el = ev.target.closest(".swatches button"))) {
      seg("#s-accent", el.dataset.v);
      $("#s-accent-name").textContent = el.title;
      document.documentElement.setAttribute("data-accent", el.dataset.v);
      saveSettings();
      return;
    }

    if ((el = ev.target.closest(".seg button"))) {
      var box = el.parentElement;
      seg("#" + box.id, el.dataset.v);
      // solo i selettori delle impostazioni salvano subito
      if (box.id.indexOf("s-") === 0) saveSettings();
      return;
    }

    var grow = ev.target.closest(".grade-row");
    if (grow) {
      var gid = parseInt(grow.dataset.gid, 10);
      if (ev.target.closest(".del-grade")) {
        grow.classList.add("leaving");
        setTimeout(function () { mutate("deleteGrade", { id: gid }); }, 150);
        return;
      }
      openGradeSheetById(gid);
      return;
    }

    var subj = ev.target.closest(".subject");
    if (subj) {
      var sid = parseInt(subj.dataset.sid, 10);
      if (ev.target.closest(".add-grade")) {
        var m = findSubject(sid);
        openGradeSheet(null, m ? m.name : "");
        return;
      }
      if (ev.target.closest(".ren") || ev.target.closest(".subject-head")) {
        openSubjectSheet(sid);
        return;
      }
    }

    var card = ev.target.closest(".card");
    if (card) {
      var id = parseInt(card.dataset.id, 10);
      var done = card.classList.contains("done");
      if (ev.target.closest(".check")) {
        if (!done) {
          card.classList.add("leaving");
          setTimeout(function () { mutate("setDone", { id: id, done: true }); }, 140);
        } else {
          mutate("setDone", { id: id, done: false });
        }
        return;
      }
      if (ev.target.closest(".del")) {
        card.classList.add("leaving");
        setTimeout(function () { mutate("delete", { id: id }); }, 140);
        return;
      }
      if (ev.target.closest(".edit") || (!done && ev.target.closest(".card-body"))) {
        var t = (state.tasks || []).filter(function (x) { return x.id === id; })[0];
        if (t) openTaskSheet(t);
        return;
      }
    }
  });

  $("#btn-add").addEventListener("click", function () {
    if (section === "materie") openGradeSheet(null, "");
    else openTaskSheet(null);
  });
  $("#btn-settings").addEventListener("click", function () { openSheet("#sheet-settings"); });
  $("#btn-close").addEventListener("click", function () { closeSheets(); call("closePanel"); });
  $("#btn-more").addEventListener("click", function () {
    var open = $("#more").hidden;
    $("#more").hidden = !open;
    this.setAttribute("aria-expanded", open ? "true" : "false");
  });
  $("#f-date").addEventListener("change", syncChips);

  $("#form-task").addEventListener("submit", function (ev) {
    ev.preventDefault();
    var kindOn = document.querySelector("#f-kind button.on");
    var payload = {
      id: editingId,
      kind: kindOn ? kindOn.dataset.v : "compito",
      title: $("#f-title").value.trim(),
      dueDate: $("#f-date").value,
      dueTime: $("#f-time").value,
      category: $("#f-category").value.trim(),
      note: $("#f-note").value.trim()
    };
    if (!payload.title) { showFormError("Scrivi il nome dell'attività."); return; }
    if (!payload.dueDate) { showFormError("Scegli una data di scadenza."); return; }
    call(editingId ? "update" : "add", payload).then(function (s) {
      render(s);
      closeSheets();
      toast(editingId ? "Attività aggiornata" : "Attività aggiunta");
      editingId = 0;
    }).catch(function (e) { showFormError(e.message); });
  });

  $("#g-value").addEventListener("input", syncGradeChips);

  $("#form-grade").addEventListener("submit", function (ev) {
    ev.preventDefault();
    var typeOn = document.querySelector("#g-type button.on");
    var value = parseFloat(($("#g-value").value || "").replace(",", "."));
    var payload = {
      id: editingGrade,
      subject: $("#g-subject").value.trim(),
      value: isNaN(value) ? 0 : value,
      date: $("#g-date").value,
      type: typeOn ? typeOn.dataset.v : "",
      note: $("#g-note").value.trim()
    };
    if (!payload.subject) { showErr("#grade-error", "Scrivi il nome della materia."); return; }
    if (!payload.value) { showErr("#grade-error", "Scegli o scrivi il voto (da 1 a 6)."); return; }
    var wasEditing = editingGrade;
    call(wasEditing ? "updateGrade" : "addGrade", payload).then(function (s) {
      editingGrade = 0;
      render(s);
      closeSheets();
      toast(wasEditing ? "Voto aggiornato" : "Voto aggiunto");
    }).catch(function (e) { showErr("#grade-error", e.message); });
  });

  $("#form-subject").addEventListener("submit", function (ev) {
    ev.preventDefault();
    var name = $("#m-name").value.trim();
    if (!name) { showErr("#subject-error", "Scrivi il nome della materia."); return; }
    call("updateSubject", { id: editingSubject, name: name, teacher: $("#m-teacher").value.trim() }).then(function (s) {
      render(s);
      closeSheets();
      toast("Materia aggiornata");
    }).catch(function (e) { showErr("#subject-error", e.message); });
  });

  $("#btn-del-subject").addEventListener("click", function () {
    if (!confirmDelete) {
      confirmDelete = true;
      this.textContent = "Sicuro? Premi di nuovo";
      return;
    }
    var id = editingSubject;
    mutate("deleteSubject", { id: id }).then(function () {
      closeSheets();
      toast("Materia eliminata");
    });
  });

  function showFormError(msg) {
    var el = $("#form-error");
    el.textContent = msg;
    el.hidden = false;
  }

  function currentSettings() {
    return {
      startWithWindows: $("#s-autostart").checked,
      showCompleted: $("#s-completed").checked,
      hideTab: $("#s-hidetab").checked,
      theme: (document.querySelector("#s-theme .on") || {}).dataset.v || "auto",
      tabSide: (document.querySelector("#s-side .on") || {}).dataset.v || "right",
      tabPosition: (document.querySelector("#s-pos .on") || {}).dataset.v || "center",
      panelWidth: parseInt($("#s-width").value, 10) || 400,
      accent: (document.querySelector("#s-accent button.on") || {}).dataset ?
        document.querySelector("#s-accent button.on").dataset.v : "blue"
    };
  }
  function saveSettings() { return mutate("saveSettings", currentSettings()); }

  $("#s-autostart").addEventListener("change", saveSettings);
  $("#s-completed").addEventListener("change", saveSettings);
  $("#s-hidetab").addEventListener("change", saveSettings);
  $("#s-width").addEventListener("input", function () { $("#s-width-val").textContent = this.value + " px"; });
  $("#s-width").addEventListener("change", saveSettings);
  $("#btn-clear-done").addEventListener("click", function () {
    mutate("clearCompleted").then(function () { toast("Completati eliminati"); });
  });
  $("#btn-quit").addEventListener("click", function () { call("quit"); });

  document.addEventListener("keydown", function (ev) {
    if (ev.key === "Escape") {
      if (sheetOpen()) { closeSheets(); editingId = 0; editingGrade = 0; }
      else call("closePanel");
    }
    if (ev.key === "n" && (ev.ctrlKey || ev.metaKey)) {
      ev.preventDefault();
      if (section === "materie") openGradeSheet(null, "");
      else openTaskSheet(null);
    }
    if (ev.key === "Enter" && ev.target.id === "f-title") {
      ev.preventDefault();
      $("#form-task").dispatchEvent(new Event("submit", { cancelable: true }));
    }
  });

  // il programma Go richiama questa funzione ogni volta che il pannello si apre,
  // così le priorità vengono ricalcolate (per esempio dopo il cambio di giorno).
  window.__udiagendaRefresh = function () {
    refresh();
    closeSheets();
  };
  window.__udiagendaFocusNew = function () { openTaskSheet(null); };

  refresh().catch(function (e) {
    $("#list").innerHTML = '<div class="empty"><p>Impossibile leggere i dati.<br>' + esc(e.message) + '</p></div>';
  });
})();
