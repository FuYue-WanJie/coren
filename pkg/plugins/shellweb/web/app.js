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

  // ---------- message model ----------
  //
  // Rendering is driven by an ordered array of items rather than appending to
  // the DOM as events arrive. This keeps tool calls in the order they happened
  // and lets consecutive collapsible items (reasoning + tools) render as one
  // visually connected group while each still expands on its own.
  //
  // Item shapes:
  //   { kind:"user",      text }
  //   { kind:"assistant", text }
  //   { kind:"reasoning", text }
  //   { kind:"tool", id, name, args, output, error, done }
  //   { kind:"error",     text }
  let items = [];

  // trackId is the tool-call id used to match a result to its call.
  function findTool(id) {
    return items.find((it) => it.kind === "tool" && it.id === id);
  }

  // lastOfKind returns the index of the last item of a kind, or -1.
  function lastIndexOfKind(kind) {
    for (let i = items.length - 1; i >= 0; i--) {
      if (items[i].kind === kind) return i;
    }
    return -1;
  }

  function isCollapsible(it) {
    return it.kind === "reasoning" || it.kind === "tool";
  }

  // pushUser adds a local user message.
  function pushUser(text) {
    items.push({ kind: "user", text });
    renderMessages();
  }

  // pushAssistantDelta appends text to the trailing assistant item, creating a
  // new one when the previous item is not assistant (e.g. a tool just ran).
  function pushAssistantDelta(delta) {
    const last = items[items.length - 1];
    if (last && last.kind === "assistant") {
      last.text += delta;
    } else {
      items.push({ kind: "assistant", text: delta });
    }
    renderMessages();
  }

  // pushReasoningDelta appends to the trailing reasoning item, or starts one.
  function pushReasoningDelta(delta) {
    const last = items[items.length - 1];
    if (last && last.kind === "reasoning") {
      last.text += delta;
    } else {
      items.push({ kind: "reasoning", text: delta });
    }
    renderMessages();
  }

  function pushToolCall(id, name, args) {
    items.push({ kind: "tool", id, name, args, output: "", error: "", done: false });
    renderMessages();
  }

  function completeTool(id, name, output, error) {
    let tool = findTool(id);
    if (!tool) {
      // Some events omit a matching call id; fall back to the last unfinished
      // tool with the same name.
      for (let i = items.length - 1; i >= 0; i--) {
        if (items[i].kind === "tool" && !items[i].done && items[i].name === name) {
          tool = items[i];
          break;
        }
      }
    }
    if (tool) {
      tool.output = output || "";
      tool.error = error || "";
      tool.done = true;
      renderMessages();
    }
  }

  function pushError(text) {
    items.push({ kind: "error", text });
    renderMessages();
  }

  function clearMessages() {
    items = [];
    messagesEl.innerHTML = "";
  }

  // renderMessages rebuilds the message DOM from items, grouping consecutive
  // collapsible items into a single connected block.
  function renderMessages() {
    messagesEl.innerHTML = "";
    let i = 0;
    while (i < items.length) {
      const it = items[i];
      if (isCollapsible(it)) {
        // Collect a run of consecutive collapsible items into one group.
        const group = document.createElement("div");
        group.className = "collapse-group";
        let first;
        let last;
        while (i < items.length && isCollapsible(items[i])) {
          const node = collapsibleNode(items[i]);
          group.appendChild(node);
          if (!first) first = node;
          last = node;
          i++;
        }
        first.classList.add("group-first");
        last.classList.add("group-last");
        messagesEl.appendChild(group);
      } else {
        messagesEl.appendChild(messageNode(it));
        i++;
      }
    }
    messagesEl.scrollTop = messagesEl.scrollHeight;
  }

  // messageNode builds a non-collapsible message bubble.
  function messageNode(it) {
    const el = document.createElement("div");
    if (it.kind === "error") {
      el.className = "msg error";
      el.textContent = it.text;
      return el;
    }
    el.className = "msg " + it.kind;
    el.textContent = it.text;
    return el;
  }

  // collapsibleNode builds one collapsible block. Its expanded state is kept in
  // the item so re-renders preserve what the user opened.
  function collapsibleNode(it) {
    const wrap = document.createElement("div");
    wrap.className = "collapsible " + it.kind;

    const header = document.createElement("button");
    header.type = "button";
    header.className = "collapsible-header";
    const chevron = document.createElement("span");
    chevron.className = "material-icons collapsible-chevron";
    chevron.textContent = "expand_more";
    const title = document.createElement("span");
    title.className = "collapsible-title";
    title.textContent = it.kind === "reasoning"
      ? "思考过程"
      : toolLabel(it);
    header.append(chevron, title);

    const body = document.createElement("div");
    body.className = "collapsible-body";
    body.textContent = it.kind === "reasoning" ? it.text : toolBody(it);
    body.hidden = !it.open;

    chevron.style.transform = it.open ? "rotate(180deg)" : "";
    header.addEventListener("click", () => {
      it.open = !it.open;
      body.hidden = !it.open;
      chevron.style.transform = it.open ? "rotate(180deg)" : "";
    });

    wrap.append(header, body);
    return wrap;
  }

  // toolLabel describes a tool block's header.
  function toolLabel(it) {
    if (it.error) return "工具出错 " + it.name;
    if (it.done) return "工具结果 " + it.name;
    return "调用工具 " + it.name;
  }

  // toolBody renders the call arguments and, once finished, the output.
  function toolBody(it) {
    let text = "▶ " + it.name + "(" + (it.args || "") + ")";
    if (it.done) {
      text += "\n◀ " + (it.error ? "error: " + it.error : it.output);
    }
    return text;
  }

  function showEmptyHint() {
    if (items.length === 0) {
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
      buildHistory(data.messages || []);
      renderMessages();
      showEmptyHint();
      messagesEl.scrollTop = messagesEl.scrollHeight;
      if (isMobile()) drawer.open = false;
    } catch { /* ignore */ }
  }

  // buildHistory converts stored session messages into render items.
  function buildHistory(messages) {
    items = [];
    for (const m of messages || []) {
      switch (m.role) {
        case "user":
          items.push({ kind: "user", text: m.text });
          break;
        case "assistant":
          if (m.text && m.text.trim()) items.push({ kind: "assistant", text: m.text.trim() });
          break;
        case "tool_call":
          items.push({ kind: "tool", id: m.tool_call_id, name: m.tool_name, args: m.text || "", output: "", error: "", done: false });
          break;
        case "tool_result": {
          const tool = findTool(m.tool_call_id);
          if (tool) {
            tool.output = m.text || "";
            tool.error = m.error || "";
            tool.done = true;
          } else {
            items.push({ kind: "tool", id: m.tool_call_id, name: m.tool_name, args: "", output: m.text || "", error: m.error || "", done: true });
          }
          break;
        }
      }
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
    // MDUI text-field also triggers form submit on plain Enter; this guards the
    // Shift+Enter case so it inserts a newline instead of submitting.
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      submitTurn();
    }
  });

  // The send button is a plain icon button (not a form submit), so wire it up.
  send.addEventListener("click", submitTurn);

  async function submitTurn() {
    const text = (input.value || "").trim();
    if (!text || streaming) return;

    input.value = "";
    const hint = messagesEl.querySelector(".empty-hint");
    if (hint) hint.remove();
    pushUser(text);
    let assistant = null; // item index of the current assistant bubble

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
          handleEvent(JSON.parse(line.slice(5).trim()));
        }
      }
    } catch (err) {
      pushError("请求失败: " + err.message);
    } finally {
      // Trim leading blank lines the model often emits before its reply.
      const last = items[items.length - 1];
      if (last && last.kind === "assistant") {
        if (last.text.trim()) last.text = last.text.trim();
        else items.pop();
        renderMessages();
      }
      streaming = false;
      send.disabled = false;
      status.textContent = "就绪";
      input.focus();
      loadSessions();
    }
  }

  function handleEvent(ev) {
    switch (ev.type) {
      case "text":
        pushAssistantDelta(ev.delta);
        break;
      case "reasoning":
        pushReasoningDelta(ev.delta);
        break;
      case "tool_call":
        pushToolCall(ev.id || ev.name, ev.name, ev.arguments);
        break;
      case "tool_result":
        completeTool(ev.id || ev.name, ev.name, ev.output, ev.error);
        break;
      case "rejected":
        pushError("已拒绝：" + (ev.reason || ""));
        break;
      case "error":
        pushError(ev.error);
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
