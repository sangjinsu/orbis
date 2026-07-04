const NODE_KEYS = [
  "client",
  "gateway",
  "queue",
  "lane",
  "reducer",
  "dispatcher",
  "worker",
  "broker",
  "terminal",
];

const state = {
  socket: null,
  runId: "",
  sessionId: "",
  events: [],
  selectedIndex: -1,
  requestSeq: 0,
  pendingPrompt: "",
  animationQueue: Promise.resolve(),
  stageCounts: Object.fromEntries(NODE_KEYS.map((key) => [key, 0])),
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
  runtimeMap: document.querySelector("#runtimeMap"),
  eventToken: document.querySelector("#eventToken"),
  tokenLabel: document.querySelector("#tokenLabel"),
  nodeCounts: Object.fromEntries(NODE_KEYS.map((key) => [key, document.querySelector(`#${key}Count`)])),
  nodes: Object.fromEntries(NODE_KEYS.map((key) => [key, document.querySelector(`[data-node="${key}"]`)])),
  edges: Object.fromEntries([...document.querySelectorAll("[data-edge]")].map((edge) => [edge.dataset.edge, edge])),
  presets: [...document.querySelectorAll(".preset")],
};

const edgeByPair = {
  "client>gateway": "client-gateway",
  "gateway>queue": "gateway-queue",
  "queue>lane": "queue-lane",
  "lane>reducer": "lane-reducer",
  "reducer>dispatcher": "reducer-dispatcher",
  "dispatcher>worker": "dispatcher-worker",
  "worker>queue": "worker-queue",
  "reducer>broker": "reducer-broker",
  "broker>terminal": "broker-terminal",
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
  bump("client", "connect");

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
  bump("gateway", method);
}

function sendPrompt() {
  const text = els.promptText.value.trim();
  if (!text) {
    return;
  }
  state.sessionId = els.sessionId.value.trim() || defaultSessionID();
  els.sessionId.value = state.sessionId;
  clearEvents();
  bump("client", "prompt");
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
    pulseRoute(["gateway", "terminal"], "error");
    setControls();
    return;
  }
  const payload = response.payload || {};
  els.lastAck.textContent = response.id;
  pulseRoute(["gateway", "client"], "ACK");
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

function stageForEvent(eventName) {
  if (["SessionCreated", "UserMessageReceived"].includes(eventName)) {
    return "queue";
  }
  if (["RunStarted", "RunStatusChanged", "TimerFired"].includes(eventName) || eventName.startsWith("Skill")) {
    return "reducer";
  }
  if (eventName === "LLMCallStarted" || eventName === "ToolCallStarted") {
    return "dispatcher";
  }
  if (eventName.includes("LLM") || eventName.includes("Tool") || eventName === "AssistantDelta") {
    return "worker";
  }
  if (eventName === "FinalAnswerEmitted") {
    return "broker";
  }
  if (["RunCompleted", "RunFailed", "RunCancelled"].includes(eventName)) {
    return "terminal";
  }
  return "gateway";
}

function routeForEvent(eventName) {
  if (eventName === "UserMessageReceived" || eventName === "SessionCreated") {
    return ["client", "gateway", "queue", "lane"];
  }
  if (eventName === "RunStarted" || eventName === "RunStatusChanged" || eventName.startsWith("Skill")) {
    return ["lane", "reducer"];
  }
  if (eventName === "LLMCallStarted" || eventName === "ToolCallStarted") {
    return ["reducer", "dispatcher", "worker"];
  }
  if (eventName === "AssistantDelta") {
    return ["worker", "broker"];
  }
  if (eventName === "LLMResponseReceived" || eventName.startsWith("ToolCall")) {
    return ["worker", "queue", "lane", "reducer"];
  }
  if (eventName === "FinalAnswerEmitted") {
    return ["reducer", "broker"];
  }
  if (["RunCompleted", "RunFailed", "RunCancelled"].includes(eventName)) {
    return ["broker", "terminal"];
  }
  return ["gateway"];
}

function addEvent(event) {
  const stage = stageForEvent(event.event);
  state.events.push({ ...event, stage });
  bump(stage, event.event);
  pulseRoute(routeForEvent(event.event), event.event);
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
  selectEvent(state.events.length - 1);
  setControls();
}

function bump(stage, label) {
  if (!Object.prototype.hasOwnProperty.call(state.stageCounts, stage)) {
    return;
  }
  state.stageCounts[stage] += 1;
  renderCounts();
  pulseNode(stage, label);
}

function renderCounts() {
  for (const key of NODE_KEYS) {
    els.nodeCounts[key].textContent = state.stageCounts[key];
  }
}

function pulseNode(stage) {
  const node = els.nodes[stage];
  if (!node) {
    return;
  }
  node.classList.add("is-active");
  window.setTimeout(() => node.classList.remove("is-active"), 520);
}

function pulseRoute(route, label) {
  const cleaned = route.filter((key) => els.nodes[key]);
  if (cleaned.length === 0) {
    return;
  }
  for (const key of cleaned) {
    pulseNode(key, label);
  }
  for (let i = 0; i < cleaned.length - 1; i += 1) {
    const edgeName = edgeByPair[`${cleaned[i]}>${cleaned[i + 1]}`];
    const edge = edgeName ? els.edges[edgeName] : null;
    if (!edge) {
      continue;
    }
    edge.classList.add("is-active");
    window.setTimeout(() => edge.classList.remove("is-active"), 620);
  }
  animateRoute(cleaned, shortLabel(label));
}

function shortLabel(label) {
  if (!label) {
    return "event";
  }
  return String(label).replace(/([a-z])([A-Z])/g, "$1 $2").split(/\s+/).slice(0, 2).join(" ");
}

function animateRoute(route, label) {
  if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) {
    return;
  }
  state.animationQueue = state.animationQueue.then(async () => {
    els.tokenLabel.textContent = label;
    els.eventToken.classList.add("is-visible");
    for (const stage of route) {
      moveTokenTo(stage);
      await sleep(180);
    }
    await sleep(110);
    els.eventToken.classList.remove("is-visible");
  });
}

function moveTokenTo(stage) {
  const node = els.nodes[stage];
  if (!node) {
    return;
  }
  const map = els.runtimeMap.getBoundingClientRect();
  const rect = node.getBoundingClientRect();
  const x = rect.left - map.left + rect.width / 2 - els.eventToken.offsetWidth / 2;
  const y = rect.top - map.top + rect.height / 2 - els.eventToken.offsetHeight / 2;
  els.eventToken.style.transform = `translate(${Math.round(x)}px, ${Math.round(y)}px)`;
}

function sleep(ms) {
  return new Promise((resolve) => window.setTimeout(resolve, ms));
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
  state.stageCounts = Object.fromEntries(NODE_KEYS.map((key) => [key, 0]));
  state.animationQueue = Promise.resolve();
  els.runId.textContent = "-";
  els.currentSession.textContent = els.sessionId.value.trim() || "-";
  els.lastAck.textContent = "-";
  els.finalAnswer.textContent = "-";
  els.eventToken.classList.remove("is-visible");
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
for (const preset of els.presets) {
  preset.addEventListener("click", () => {
    els.promptText.value = preset.dataset.prompt || "";
    els.promptText.focus();
  });
}

clearEvents();
