// Minimal WebUI client: posts to /api/chat and renders the SSE stream.
(() => {
  const messages = document.getElementById("messages");
  const form = document.getElementById("composer");
  const input = document.getElementById("input");
  const send = document.getElementById("send");
  const status = document.getElementById("status");
  const sessionId = "web-" + Math.random().toString(36).slice(2, 8);

  // WebUI update notice.
  const notice = document.getElementById("webui-notice");
  const noticeText = document.getElementById("webui-notice-text");
  const updateBtn = document.getElementById("webui-update");
  const dismissBtn = document.getElementById("webui-dismiss");

  function shortHash(h) {
    return h ? h.slice(0, 12) : "未知";
  }

  function label(version, hash) {
    return version ? version : shortHash(hash);
  }

  // checkWebUIUpdate asks the server whether the extracted UI is stale.
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
    } catch {
      // Status is best-effort; ignore failures.
    }
  }

  updateBtn.addEventListener("click", async () => {
    updateBtn.disabled = true;
    try {
      const resp = await fetch("/api/webui/update", { method: "POST" });
      if (resp.ok) {
        // The new UI is on disk; reload to pick it up.
        location.reload();
        return;
      }
      const data = await resp.json().catch(() => ({}));
      noticeText.textContent = "更新失败：" + (data.error || resp.status);
    } catch (err) {
      noticeText.textContent = "更新失败：" + err.message;
    } finally {
      updateBtn.disabled = false;
    }
  });

  dismissBtn.addEventListener("click", () => {
    notice.classList.add("hidden");
  });

  function addMessage(role, text) {
    const el = document.createElement("div");
    el.className = "msg " + role;
    el.textContent = text;
    messages.appendChild(el);
    messages.scrollTop = messages.scrollHeight;
    return el;
  }

  input.addEventListener("input", () => {
    input.style.height = "auto";
    input.style.height = Math.min(input.scrollHeight, 160) + "px";
  });

  input.addEventListener("keydown", (e) => {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      form.requestSubmit();
    }
  });

  form.addEventListener("submit", async (e) => {
    e.preventDefault();
    const text = input.value.trim();
    if (!text) return;

    input.value = "";
    input.style.height = "auto";
    addMessage("user", text);
    const assistant = addMessage("assistant", "");
    const tools = new Map();

    send.disabled = true;
    status.textContent = "思考中…";

    try {
      const resp = await fetch("/api/chat", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ session_id: sessionId, message: text }),
      });
      if (!resp.ok || !resp.body) {
        throw new Error("HTTP " + resp.status);
      }
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
          handleEvent(JSON.parse(line.slice(5).trim()), assistant, tools);
        }
      }
    } catch (err) {
      addMessage("error", "请求失败: " + err.message);
    } finally {
      send.disabled = false;
      status.textContent = "就绪";
      input.focus();
    }
  });

  function handleEvent(ev, assistant, tools) {
    switch (ev.type) {
      case "text":
        assistant.textContent += ev.delta;
        messages.scrollTop = messages.scrollHeight;
        break;
      case "tool_call": {
        const el = document.createElement("div");
        el.className = "msg tool";
        el.textContent = "▶ " + ev.name + "(" + ev.arguments + ")";
        messages.appendChild(el);
        tools.set(ev.name, el);
        messages.scrollTop = messages.scrollHeight;
        break;
      }
      case "tool_result": {
        const el = tools.get(ev.name);
        if (el) el.textContent += "\n◀ " + (ev.error ? "error: " + ev.error : ev.output);
        messages.scrollTop = messages.scrollHeight;
        break;
      }
      case "error":
        addMessage("error", ev.error);
        break;
    }
  }

  input.focus();
  checkWebUIUpdate();
})();
