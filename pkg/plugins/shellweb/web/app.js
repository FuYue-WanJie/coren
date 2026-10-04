// Coren WebUI client: session management, /api/chat SSE streaming, MICL-driven UI.
(() => {
  const messagesEl = document.getElementById("messages");
  const form = document.getElementById("composer");
  const input = document.getElementById("input");
  const send = document.getElementById("send");
  const status = document.getElementById("status");
  const sessionList = document.getElementById("session-list");
  const newSessionBtn = document.getElementById("new-session");
  const menuToggle = document.getElementById("menu-toggle");
  const drawer = document.getElementById("drawer");
  const scrim = document.getElementById("scrim");
  const appbarTitle = document.getElementById("appbar-title");

  // WebUI update notice.
  const notice = document.getElementById("webui-notice");
  const noticeText = document.getElementById("webui-notice-text");
  const updateBtn = document.getElementById("webui-update");
  const dismissBtn = document.getElementById("webui-dismiss");

  let currentSessionId = null; // null until the first message creates one
  let streaming = false;

  // ---------- helpers ----------

  function shortHash(h) { return h ? h.slice(0, 12) : "未知"; }
  function label(version, hash) { return version ? version : shortHash(hash); }

  function addMessage(role, text) {
    const el = document.createElement("div");
    el.className = "msg " + role;
    el.textContent = text;
    messagesEl.appendChild(el);
    messagesEl.scrollTop = messagesEl.scrollHeight;
    return el;
  }

  function clearMessages() { messagesEl.innerHTML = ""; }

  function showEmptyHint() {
    if (messagesEl.children.length === 0) {
      const el = document.createElement("div");
      el.className = "empty-hint";
      el.textContent = "开始一段新对话";
      messagesEl.appendChild(el);
    }
  }

  // ---------- session management ----------

  async function loadSessions() {
    try {
      const resp = await fetch("/api/sessions");
      if (!resp.ok) return;
      const data = await resp.json();
      renderSessions(data.sessions || []);
    } catch { /* ignore */ }
  }

  function renderSessions(sessions) {
    sessionList.innerHTML = "";
    for (const s of sessions) {
      const li = document.createElement("li");
      li.className = "micl-list__item session-item" + (s.id === currentSessionId ? " active" : "");
      li.dataset.id = s.id;

      const text = document.createElement("div");
      text.className = "session-item-text";
      const title = document.createElement("div");
      title.className = "session-item-title";
      title.textContent = s.title || "新会话";
      const meta = document.createElement("div");
      meta.className = "session-item-meta";
      meta.textContent = (s.message_count || 0) + " 条消息";
      text.append(title, meta);

      const actions = document.createElement("div");
      actions.className = "session-item-actions";
      const renameBtn = document.createElement("button");
      renameBtn.className = "icon-btn small";
      renameBtn.innerHTML = '<span class="material-symbols-outlined">edit</span>';
      renameBtn.title = "重命名";
      renameBtn.addEventListener("click", (e) => { e.stopPropagation(); renameSession(s); });
      const delBtn = document.createElement("button");
      delBtn.className = "icon-btn small";
      delBtn.innerHTML = '<span class="material-symbols-outlined">delete</span>';
      delBtn.title = "删除";
      delBtn.addEventListener("click", (e) => { e.stopPropagation(); deleteSession(s.id); });
      actions.append(renameBtn, delBtn);

      li.append(text, actions);
      li.addEventListener("click", () => openSession(s.id));
      sessionList.appendChild(li);
    }
  }

  async function newSession() {
    currentSessionId = null;
    clearMessages();
    showEmptyHint();
    appbarTitle.textContent = "Coren";
    renderSessionsActive(null);
    input.focus();
  }

  function renderSessionsActive(id) {
    for (const li of sessionList.querySelectorAll(".session-item")) {
      li.classList.toggle("active", li.dataset.id === id);
    }
  }

  async function openSession(id) {
    if (streaming) return;
    try {
      const resp = await fetch("/api/sessions/" + encodeURIComponent(id));
      if (!resp.ok) return;
      const data = await resp.json();
      currentSessionId = id;
      renderSessionsActive(id);
      appbarTitle.textContent = data.title || "Coren";
      clearMessages();
      for (const m of data.messages || []) renderHistory(m);
      if ((data.messages || []).length === 0) showEmptyHint();
      messagesEl.scrollTop = messagesEl.scrollHeight;
      if (window.matchMedia("(max-width: 720px)").matches) setDrawer(false);
    } catch { /* ignore */ }
  }

  function renderHistory(m) {
    switch (m.role) {
      case "user":
        addMessage("user", m.text);
        break;
      case "assistant":
        if (m.text) addMessage("assistant", m.text);
        break;
      case "tool_call":
        addMessage("tool", "▶ " + m.tool_name + "(" + (m.text || "") + ")");
        break;
      case "tool_result":
        addMessage("tool", "◀ " + (m.error ? "error: " + m.error : m.text));
        break;
    }
  }

  async function renameSession(s) {
    const title = prompt("重命名会话", s.title || "");
    if (title === null || title.trim() === "") return;
    await fetch("/api/sessions/" + encodeURIComponent(s.id), {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ title: title.trim() }),
    });
    loadSessions();
    if (s.id === currentSessionId) appbarTitle.textContent = title.trim();
  }

  async function deleteSession(id) {
    if (!confirm("删除这个会话？一天内可从回收区恢复。")) return;
    await fetch("/api/sessions/" + encodeURIComponent(id), { method: "DELETE" });
    if (id === currentSessionId) await newSession();
    loadSessions();
  }

  // Ensure a session exists before the first message (lazy creation).
  async function ensureSession() {
    if (currentSessionId) return currentSessionId;
    const resp = await fetch("/api/sessions", { method: "POST" });
    const data = await resp.json();
    currentSessionId = data.id;
    return currentSessionId;
  }

  // ---------- chat ----------

  function autoGrow() {
    input.style.height = "auto";
    input.style.height = Math.min(input.scrollHeight, 180) + "px";
  }

  input.addEventListener("input", autoGrow);
  input.addEventListener("keyup", autoGrow);

  input.addEventListener("keydown", (e) => {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      form.requestSubmit();
    }
  });

  form.addEventListener("submit", async (e) => {
    e.preventDefault();
    const text = input.value.trim();
    if (!text || streaming) return;

    input.value = "";
    input.style.height = "auto";
    // Drop the empty hint on the first real message.
    const hint = messagesEl.querySelector(".empty-hint");
    if (hint) hint.remove();
    addMessage("user", text);
    let assistantEl = null;
    let reasoningEl = null;
    const tools = new Map();

    streaming = true;
    send.disabled = true;
    status.textContent = "思考中…";

    try {
      const sessionId = await ensureSession();
      const resp = await fetch("/api/chat", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ session_id: sessionId, message: text }),
      });
      if (!resp.ok || !resp.body) throw new Error("HTTP " + resp.status);

      const reader = resp.body.getReader();
      const decoder = new TextDecoder();
      let buffer = "";

      while (true) {
        const { value, done } = await reader.read();
        if (done) break;
        buffer += decoder.decode(value, { stream: true });
        const parts = buffer.split("\n\n");
        buffer = parts.pop();
        for (const part of parts) {
          const line = part.split("\n").find((l) => l.startsWith("data:"));
          if (!line) continue;
          const ev = JSON.parse(line.slice(5).trim());
          const ui = { tools, getAssistant: () => assistantEl, setAssistant: (el) => { assistantEl = el; }, getReasoning: () => reasoningEl, setReasoning: (el) => { reasoningEl = el; } };
          handleEvent(ev, ui);
        }
      }
    } catch (err) {
      addMessage("error", "请求失败: " + err.message);
    } finally {
      streaming = false;
      send.disabled = false;
      status.textContent = "就绪";
      input.focus();
      loadSessions();
    }
  });

  function handleEvent(ev, ui) {
    switch (ev.type) {
      case "text": {
        let el = ui.getAssistant();
        if (!el) {
          el = addMessage("assistant", "");
          ui.setAssistant(el);
        }
        el.textContent += ev.delta;
        messagesEl.scrollTop = messagesEl.scrollHeight;
        break;
      }
      case "reasoning": {
        let el = ui.getReasoning();
        if (!el) {
          el = addMessage("reasoning", "");
          ui.setReasoning(el);
        }
        el.textContent += ev.delta;
        messagesEl.scrollTop = messagesEl.scrollHeight;
        break;
      }
      case "tool_call": {
        const el = document.createElement("div");
        el.className = "msg tool";
        el.textContent = "▶ " + ev.name + "(" + ev.arguments + ")";
        messagesEl.appendChild(el);
        ui.tools.set(ev.name, el);
        messagesEl.scrollTop = messagesEl.scrollHeight;
        break;
      }
      case "tool_result": {
        const el = ui.tools.get(ev.name);
        if (el) el.textContent += "\n◀ " + (ev.error ? "error: " + ev.error : ev.output);
        messagesEl.scrollTop = messagesEl.scrollHeight;
        break;
      }
      case "error":
        addMessage("error", ev.error);
        break;
    }
  }

  // ---------- WebUI update ----------

  async function checkWebUIUpdate() {
    try {
      const resp = await fetch("/api/webui/status");
      if (!resp.ok) return;
      const data = await resp.json();
      if (!data.enabled || !data.update_available) return;
      noticeText.textContent =
        "WebUI 有更新：你修改时基于 " + label(data.disk_version, data.disk_hash) +
        "，当前内置版本 " + label(data.builtin_version, data.builtin_hash) + "。";
      notice.classList.remove("hidden");
    } catch { /* ignore */ }
  }

  updateBtn.addEventListener("click", async () => {
    updateBtn.disabled = true;
    try {
      const resp = await fetch("/api/webui/update", { method: "POST" });
      if (resp.ok) { location.reload(); return; }
      const data = await resp.json().catch(() => ({}));
      noticeText.textContent = "更新失败：" + (data.error || resp.status);
    } catch (err) {
      noticeText.textContent = "更新失败：" + err.message;
    } finally {
      updateBtn.disabled = false;
    }
  });

  dismissBtn.addEventListener("click", () => notice.classList.add("hidden"));

  // ---------- layout ----------

  menuToggle.addEventListener("click", () => {
    setDrawer(drawer.classList.contains("collapsed"));
  });
  scrim.addEventListener("click", () => setDrawer(false));
  newSessionBtn.addEventListener("click", newSession);
  document.addEventListener("keydown", (e) => {
    if (e.key === "Escape" && !drawer.classList.contains("collapsed")) setDrawer(false);
  });

  // setDrawer opens/closes the drawer; on mobile it toggles the scrim too.
  // The collapsed state is remembered across reloads.
  function setDrawer(open) {
    const mobile = window.matchMedia("(max-width: 720px)").matches;
    if (open) {
      drawer.classList.remove("collapsed");
      if (mobile) scrim.classList.remove("hidden");
    } else {
      drawer.classList.add("collapsed");
      scrim.classList.add("hidden");
    }
    try { localStorage.setItem("coren_drawer_collapsed", open ? "0" : "1"); } catch {}
  }

  // ---------- boot ----------

  // Restore the remembered drawer state (default: open on desktop, closed on mobile).
  try {
    const saved = localStorage.getItem("coren_drawer_collapsed");
    const mobile = window.matchMedia("(max-width: 720px)").matches;
    if (saved === "1" || (saved === null && mobile)) {
      drawer.classList.add("collapsed");
    }
  } catch { /* ignore */ }

  loadSessions();
  newSession();
  checkWebUIUpdate();
})();
