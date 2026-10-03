// Minimal WebUI client: authenticates with a session token, posts to /api/chat,
// and renders the SSE stream.
(() => {
  const messages = document.getElementById("messages");
  const form = document.getElementById("composer");
  const input = document.getElementById("input");
  const send = document.getElementById("send");
  const status = document.getElementById("status");
  const login = document.getElementById("login");
  const loginForm = document.getElementById("login-form");
  const password = document.getElementById("password");
  const loginError = document.getElementById("login-error");
  const sessionId = "web-" + Math.random().toString(36).slice(2, 8);

  const TOKEN_KEY = "coren_token";

  function getToken() {
    return localStorage.getItem(TOKEN_KEY) || "";
  }

  function setToken(token) {
    if (token) localStorage.setItem(TOKEN_KEY, token);
    else localStorage.removeItem(TOKEN_KEY);
  }

  // showLogin reveals the password prompt and hides the composer.
  function showLogin(message) {
    loginError.textContent = message || "";
    login.classList.remove("hidden");
    form.style.display = "none";
    password.focus();
  }

  // hideLogin restores the normal chat UI.
  function hideLogin() {
    login.classList.add("hidden");
    loginError.textContent = "";
    form.style.display = "";
    input.focus();
  }

  loginForm.addEventListener("submit", async (e) => {
    e.preventDefault();
    try {
      const resp = await fetch("/api/login", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ password: password.value }),
      });
      if (!resp.ok) {
        showLogin("密码错误，请重试");
        password.value = "";
        return;
      }
      const data = await resp.json();
      setToken(data.token);
      password.value = "";
      hideLogin();
    } catch (err) {
      showLogin("登录失败: " + err.message);
    }
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
        headers: {
          "Content-Type": "application/json",
          Authorization: "Bearer " + getToken(),
        },
        body: JSON.stringify({ session_id: sessionId, message: text }),
      });
      if (resp.status === 401) {
        setToken("");
        showLogin("会话已过期，请重新登录");
        return;
      }
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
      case "reasoning":
        status.textContent = "思考中…";
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

  // Probe auth state: if the server requires a password and our token is
  // missing or invalid, show the login prompt; otherwise start chatting.
  async function boot() {
    try {
      const resp = await fetch("/api/authcheck", {
        headers: { Authorization: "Bearer " + getToken() },
      });
      if (resp.status === 401) {
        setToken("");
        showLogin("请登录");
        return;
      }
      const data = await resp.json();
      if (data.required && !getToken()) {
        showLogin("请登录");
        return;
      }
      hideLogin();
    } catch {
      hideLogin();
    }
    input.focus();
  }

  boot();
})();
