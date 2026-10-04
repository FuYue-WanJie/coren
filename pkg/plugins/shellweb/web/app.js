// Coren WebUI client on MDUI: session management, /api/chat SSE streaming.
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
  const appbarTitle = document.getElementById("appbar-title");

  // WebUI update notice.
  const notice = document.getElementById("webui-notice");
  const noticeText = document.getElementById("webui-notice-text");
  const updateBtn = document.getElementById("webui-update");
  const dismissBtn = document.getElementById("webui-dismiss");

  let currentSessionId = null; // null until the first message creates one
  let streaming = false;

  const isMobile = () => window.matchMedia("(max-width: 840px)").matches;

  // Drawer is modal on mobile, inline on desktop.
  function applyDrawerMode() {
    if (isMobile()) {
      drawer.setAttribute("modal", "");
      drawer.setAttribute("close-on-esc", "");
      drawer.setAttribute("close-on-overlay-click", "");
    } else {
      drawer.removeAttribute("modal");
      drawer.removeAttribute("close-on-esc");
      drawer.removeAttribute("close-on-overlay-click");
    }
  }

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
      const item = document.createElement("mdui-list-item");
      item.setAttribute("rounded", "");
      item.dataset.id = s.id;
      if (s.id === currentSessionId) item.setAttribute("active", "");

      // Title + meta text.
      const text = document.createElement("div");
      text.style.flex = "1";
      text.style.minWidth = "0";
      const title = document.createElement("div");
      title.className = "session-item-title";
      title.textContent = s.title || "新会话";
      const meta = document.createElement("div");
      meta.className = "session-item-meta";
      meta.textContent = (s.message_count || 0) + " 条消息";
      text.append(title, meta);

      // Actions.
      const actions = document.createElement("div");
      actions.className = "session-actions";
      actions.setAttribute("slot", "end-icon");
      const renameBtn = document.createElement("mdui-button-icon");
      renameBtn.setAttribute("icon", "edit");
      renameBtn.title = "重命名";
      renameBtn.addEventListener("click", (e) => { e.stopPropagation(); renameSession(s); });
      const delBtn = document.createElement("mdui-button-icon");
      delBtn.setAttribute("icon", "delete");
      delBtn.title = "删除";
      delBtn.addEventListener("click", (e) => { e.stopPropagation(); deleteSession(s.id); });
      actions.append(renameBtn, delBtn);

      item.append(text, actions);
      item.addEventListener("click", () => openSession(s.id));
      sessionList.appendChild(item);
    }
  }

  async function newSession() {
    currentSessionId = null;
    clearMessages();
    showEmptyHint();
    appbarTitle.textContent = "Coren";
    for (const item of sessionList.querySelectorAll("mdui-list-item")) item.removeAttribute("active");
    if (isMobile()) drawer.open = false;
    input.focus();
  }

  async function openSession(id) {
    if (streaming) return;
    try {
      const resp = await fetch("/api/sessions/" + encodeURIComponent(id));
      if (!resp.ok) return;
      const data = await resp.json();
      currentSessionId = id;
      appbarTitle.textContent = data.title || "Coren";
      for (const item of sessionList.querySelectorAll("mdui-list-item")) {
        item.toggleAttribute("active", item.dataset.id === id);
      }
      clearMessages();
      for (const m of data.messages || []) renderHistory(m);
      if ((data.messages || []).length === 0) showEmptyHint();
      messagesEl.scrollTop = messagesEl.scrollHeight;
      if (isMobile()) drawer.open = false;
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
    const title = await mdui.prompt({
      headline: "重命名会话",
      defaultValue: s.title || "",
      confirmText: "确定",
      cancelText: "取消",
    });
    if (title === null || title === undefined || title.trim() === "") return;
    await fetch("/api/sessions/" + encodeURIComponent(s.id), {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ title: title.trim() }),
    });
    loadSessions();
    if (s.id === currentSessionId) appbarTitle.textContent = title.trim();
  }

  async function deleteSession(id) {
    const ok = await mdui.confirm({
      headline: "删除会话",
      description: "删除后一天内可从回收区恢复。",
      confirmText: "删除",
      cancelText: "取消",
    });
    if (!ok) return;
    await fetch("/api/sessions/" + encodeURIComponent(id), { method: "DELETE" });
    mdui.snackbar({ message: "已删除会话" });
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

  input.addEventListener("keydown", (e) => {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      form.requestSubmit();
    }
  });

  form.addEventListener("submit", async (e) => {
    e.preventDefault();
    const text = (input.value || "").trim();
    if (!text || streaming) return;

    input.value = "";
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
          const ui = {
            tools,
            getAssistant: () => assistantEl,
            setAssistant: (el) => { assistantEl = el; },
            getReasoning: () => reasoningEl,
            setReasoning: (el) => { reasoningEl = el; },
          };
          handleEvent(JSON.parse(line.slice(5).trim()), ui);
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
        if (!el) { el = addMessage("assistant", ""); ui.setAssistant(el); }
        el.textContent += ev.delta;
        messagesEl.scrollTop = messagesEl.scrollHeight;
        break;
      }
      case "reasoning": {
        let el = ui.getReasoning();
        if (!el) { el = addMessage("reasoning", ""); ui.setReasoning(el); }
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

  menuToggle.addEventListener("click", () => { drawer.open = !drawer.open; });
  newSessionBtn.addEventListener("click", newSession);
  window.addEventListener("resize", () => {
    applyDrawerMode();
    drawer.open = !isMobile();
  });

  // ---------- boot ----------

  applyDrawerMode();
  drawer.open = !isMobile();
  loadSessions();
  newSession();
  checkWebUIUpdate();
})();
