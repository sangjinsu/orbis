const state = {
  socket: null,
  runId: "",
  sessionId: "",
  events: [],
  selectedIndex: -1,
  requestSeq: 0,
  pendingPrompt: "",
  stageCounts: {
    gateway: 0,
    queue: 0,
    reducer: 0,
    worker: 0,
    terminal: 0,
  },
};

const els = {
  socketState: document.querySelector("#socketState"),
  runState: document.querySelector("#runState"),
  sessionId: document.querySelector("#sessionId"),
  promptText: document.querySelector("#promptText"),
  connectBtn: document.querySelector("#connectBtn"),
  sendBtn: document.querySelector("#sendBtn"),
  statusBtn: document.querySelector("#statusBtn"),
  cancelBtn: document.querySelector("#cancelBtn"),
  clearBtn: document.querySelector("#clearBtn"),
  events: document.querySelector("#events"),
  eventCount: document.querySelector("#eventCount"),
  selectedEvent: document.querySelector("#selectedEvent"),
  runId: document.querySelector("#runId"),
  currentSession: document.querySelector("#currentSession"),
  lastAck: document.querySelector("#lastAck"),
  finalAnswer: document.querySelector("#finalAnswer"),
  payload: document.querySelector("#payload"),
  gatewayCount: document.querySelector("#gatewayCount"),
  queueCount: document.querySelector("#queueCount"),
  reducerCount: document.querySelector("#reducerCount"),
  workerCount: document.querySelector("#workerCount"),
  terminalCount: document.querySelector("#terminalCount"),
};

function defaultSessionID() {
  const stamp = new Date().toISOString().replace(/[-:.TZ]/g, "").slice(0, 14);
  return `debug_${stamp}`;
}

function wsURL() {
  const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
  return `${protocol}//${window.location.host}/ws`;
}

function nextRequestID(prefix) {
  state.requestSeq += 1;
  return `${prefix}_${state.requestSeq}`;
}

function setSocketState(label, className) {
  els.socketState.textContent = label;
  els.socketState.className = `pill ${className}`;
}

function setRunState(label) {
  els.runState.textContent = label;
}

function setControls() {
  const open = state.socket && state.socket.readyState === WebSocket.OPEN;
  els.connectBtn.disabled = open;
  els.sendBtn.disabled = !open;
  els.statusBtn.disabled = !open || !state.runId;
  els.cancelBtn.disabled = !open || !state.runId || isTerminalRun();
}

function isTerminalRun() {
  const terminal = ["RunCompleted", "RunFailed", "RunCancelled"];
  return state.events.some((event) => terminal.includes(event.event));
}

function connect() {
  if (state.socket && state.socket.readyState === WebSocket.OPEN) {
    return;
  }
  if (state.socket && state.socket.readyState === WebSocket.CONNECTING) {
    return;
  }
  state.sessionId = els.sessionId.value.trim() || defaultSessionID();
  els.sessionId.value = state.sessionId;
  state.socket = new WebSocket(wsURL());
  setSocketState("connecting", "state-idle");

  state.socket.addEventListener("open", () => {
    setSocketState("connected", "state-open");
    setControls();
    sendRequest("session.subscribe", { session_id: state.sessionId }, "sub");
    if (state.pendingPrompt) {
      const text = state.pendingPrompt;
      state.pendingPrompt = "";
      sendRequest("session.message", { session_id: state.sessionId, text }, "msg");
    }
  });

  state.socket.addEventListener("message", (message) => {
    const data = JSON.parse(message.data);
    if (data.type === "res") {
      handleResponse(data);
      return;
    }
    if (data.type === "event") {
      addEvent(data);
    }
  });

  state.socket.addEventListener("close", () => {
    setSocketState("closed", "state-closed");
    setControls();
  });

  state.socket.addEventListener("error", () => {
    setSocketState("socket error", "state-error");
    setControls();
  });
}

function sendRequest(method, params, prefix) {
  if (!state.socket || state.socket.readyState !== WebSocket.OPEN) {
    return;
  }
  state.socket.send(JSON.stringify({
    type: "req",
    id: nextRequestID(prefix),
    method,
    params,
  }));
}

function sendPrompt() {
  const text = els.promptText.value.trim();
  if (!text) {
    return;
  }
  state.sessionId = els.sessionId.value.trim() || defaultSessionID();
  els.sessionId.value = state.sessionId;
  clearEvents();
  if (!state.socket || state.socket.readyState !== WebSocket.OPEN) {
    state.pendingPrompt = text;
    connect();
    return;
  }
  sendRequest("session.message", { session_id: state.sessionId, text }, "msg");
}

function requestStatus() {
  if (!state.runId) {
    return;
  }
  sendRequest("run.status", { run_id: state.runId }, "status");
}

function cancelRun() {
  if (!state.runId) {
    return;
  }
  sendRequest("run.cancel", { run_id: state.runId }, "cancel");
}

function handleResponse(response) {
  if (!response.ok) {
    els.lastAck.textContent = response.error || "request failed";
    setRunState("request failed");
    setControls();
    return;
  }
  const payload = response.payload || {};
  els.lastAck.textContent = response.id;
  if (payload.run_id) {
    state.runId = payload.run_id;
    els.runId.textContent = state.runId;
    setRunState("ACK " + state.runId);
  }
  if (payload.session_id) {
    state.sessionId = payload.session_id;
    els.currentSession.textContent = state.sessionId;
  }
  if (payload.status) {
    setRunState(payload.status);
  }
  setControls();
}

function classify(eventName) {
  if (["UserMessageReceived", "SessionCreated"].includes(eventName)) {
    return "queue";
  }
  if (eventName.startsWith("Skill") || ["RunStarted", "RunStatusChanged", "TimerFired"].includes(eventName)) {
    return "reducer";
  }
  if (eventName.includes("LLM") || eventName.includes("Tool") || eventName === "AssistantDelta") {
    return "worker";
  }
  if (["FinalAnswerEmitted", "RunCompleted", "RunFailed", "RunCancelled"].includes(eventName)) {
    return "terminal";
  }
  return "gateway";
}

function addEvent(event) {
  const stage = classify(event.event);
  state.events.push({ ...event, stage });
  state.stageCounts[stage] += 1;
  if (event.run_id) {
    state.runId = event.run_id;
    els.runId.textContent = state.runId;
  }
  if (event.session_id) {
    state.sessionId = event.session_id;
    els.currentSession.textContent = state.sessionId;
  }
  if (event.event === "RunStatusChanged" && event.payload && event.payload.status) {
    setRunState(event.payload.status);
  }
  if (event.event === "RunCompleted") {
    setRunState("COMPLETED");
  }
  if (event.event === "RunFailed") {
    setRunState("FAILED");
  }
  if (event.event === "RunCancelled") {
    setRunState("CANCELLED");
  }
  if (event.event === "FinalAnswerEmitted" && event.payload && event.payload.text) {
    els.finalAnswer.textContent = event.payload.text;
  }
  renderEvents();
  renderCounts();
  selectEvent(state.events.length - 1);
  setControls();
}

function renderEvents() {
  els.events.innerHTML = "";
  state.events.forEach((event, index) => {
    const item = document.createElement("li");
    item.className = `event ${index === state.selectedIndex ? "is-selected" : ""}`;
    item.tabIndex = 0;
    item.dataset.index = String(index);
    const seq = document.createElement("span");
    seq.className = "seq";
    seq.textContent = `#${event.seq || "-"}`;
    const name = document.createElement("span");
    name.className = "name";
    name.textContent = event.event;
    const tag = document.createElement("span");
    tag.className = `tag ${event.stage}`;
    tag.textContent = event.stage;
    item.append(seq, name, tag);
    item.addEventListener("click", () => selectEvent(index));
    item.addEventListener("keydown", (keyboardEvent) => {
      if (keyboardEvent.key === "Enter" || keyboardEvent.key === " ") {
        keyboardEvent.preventDefault();
        selectEvent(index);
      }
    });
    els.events.appendChild(item);
  });
  els.eventCount.textContent = `${state.events.length} events`;
}

function renderCounts() {
  els.gatewayCount.textContent = state.stageCounts.gateway;
  els.queueCount.textContent = state.stageCounts.queue;
  els.reducerCount.textContent = state.stageCounts.reducer;
  els.workerCount.textContent = state.stageCounts.worker;
  els.terminalCount.textContent = state.stageCounts.terminal;
}

function selectEvent(index) {
  state.selectedIndex = index;
  const event = state.events[index];
  if (!event) {
    els.selectedEvent.textContent = "none";
    els.payload.textContent = "{}";
    return;
  }
  els.selectedEvent.textContent = event.event;
  els.payload.textContent = JSON.stringify(event.payload || {}, null, 2);
  for (const item of els.events.querySelectorAll(".event")) {
    item.classList.toggle("is-selected", Number(item.dataset.index) === index);
  }
}

function clearEvents() {
  state.events = [];
  state.selectedIndex = -1;
  state.runId = "";
  state.stageCounts = { gateway: 0, queue: 0, reducer: 0, worker: 0, terminal: 0 };
  els.runId.textContent = "-";
  els.currentSession.textContent = els.sessionId.value.trim() || "-";
  els.lastAck.textContent = "-";
  els.finalAnswer.textContent = "-";
  setRunState("no run");
  renderEvents();
  renderCounts();
  selectEvent(-1);
  setControls();
}

els.sessionId.value = defaultSessionID();
els.connectBtn.addEventListener("click", connect);
els.sendBtn.addEventListener("click", sendPrompt);
els.statusBtn.addEventListener("click", requestStatus);
els.cancelBtn.addEventListener("click", cancelRun);
els.clearBtn.addEventListener("click", clearEvents);
els.promptText.addEventListener("keydown", (event) => {
  if ((event.metaKey || event.ctrlKey) && event.key === "Enter") {
    sendPrompt();
  }
});

clearEvents();
