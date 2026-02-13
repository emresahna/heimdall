const rows = document.getElementById("rows");
const refreshBtn = document.getElementById("refresh");
const autoRefreshInput = document.getElementById("auto-refresh");
const lastUpdated = document.getElementById("last-updated");
const stateBanner = document.getElementById("state-banner");
const resultMeta = document.getElementById("result-meta");

const statCount = document.getElementById("stat-count");
const statP95 = document.getElementById("stat-p95");
const statError = document.getElementById("stat-error");
const chart = document.getElementById("chart");

const loadingState = document.getElementById("loading-state");
const emptyState = document.getElementById("empty-state");
const errorState = document.getElementById("error-state");
const tableWrap = document.getElementById("table-wrap");

const inputs = {
  from: document.getElementById("from"),
  to: document.getElementById("to"),
  method: document.getElementById("method"),
  status: document.getElementById("status"),
  namespace: document.getElementById("namespace"),
  pod: document.getElementById("pod"),
  path: document.getElementById("path"),
};

const AUTO_REFRESH_MS = 10000;
let lastEntries = [];
let autoRange = true;
let isRefreshing = false;
let refreshTimer = null;

function toLocalInputValue(date) {
  const pad = (num) => String(num).padStart(2, "0");
  return (
    `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}` +
    `T${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`
  );
}

function setAutoRangeWindow() {
  const now = new Date();
  const from = new Date(now.getTime() - 15 * 60 * 1000);
  inputs.from.value = toLocalInputValue(from);
  inputs.to.value = toLocalInputValue(now);
}

function buildParams() {
  const params = new URLSearchParams();

  if (inputs.from.value) {
    params.set("from", new Date(inputs.from.value).toISOString());
  }
  if (inputs.to.value) {
    params.set("to", new Date(inputs.to.value).toISOString());
  }
  if (inputs.method.value.trim()) {
    params.set("method", inputs.method.value.trim().toUpperCase());
  }
  if (inputs.status.value.trim()) {
    params.set("status", inputs.status.value.trim());
  }
  if (inputs.namespace.value.trim()) {
    params.set("namespace", inputs.namespace.value.trim());
  }
  if (inputs.pod.value.trim()) {
    params.set("pod", inputs.pod.value.trim());
  }
  if (inputs.path.value.trim()) {
    params.set("path", inputs.path.value.trim());
  }

  params.set("limit", "200");
  return params;
}

function setBanner(mode, message) {
  stateBanner.className = "state-banner";
  stateBanner.classList.add(mode);
  stateBanner.textContent = message;
  stateBanner.classList.remove("hidden");
}

function hideBanner() {
  stateBanner.className = "state-banner hidden";
  stateBanner.textContent = "";
}

function setDataState(state, errorMessage = "") {
  loadingState.classList.add("hidden");
  emptyState.classList.add("hidden");
  errorState.classList.add("hidden");
  errorState.classList.remove("error");

  if (state === "loading") {
    loadingState.classList.remove("hidden");
    tableWrap.classList.add("hidden");
    setBanner("loading", "Loading telemetry data...");
    return;
  }

  if (state === "error") {
    errorState.textContent = errorMessage || "Unable to load telemetry data.";
    errorState.classList.remove("hidden");
    errorState.classList.add("error");
    tableWrap.classList.add("hidden");
    setBanner("error", "Last query failed. Check logs and retry.");
    return;
  }

  if (state === "empty") {
    emptyState.classList.remove("hidden");
    tableWrap.classList.add("hidden");
    setBanner("empty", "No data found for the selected filters and time range.");
    return;
  }

  tableWrap.classList.remove("hidden");
  hideBanner();
}

function formatDuration(durationNs) {
  const ms = durationNs / 1e6;
  if (!Number.isFinite(ms)) {
    return "-";
  }
  if (ms >= 1000) {
    return `${(ms / 1000).toFixed(2)} s`;
  }
  return `${ms.toFixed(1)} ms`;
}

function renderStats(entries) {
  statCount.textContent = String(entries.length);

  if (entries.length === 0) {
    statP95.textContent = "0.0 ms";
    statError.textContent = "0.0%";
    return;
  }

  const latenciesMs = entries
    .map((entry) => entry.duration_ns / 1e6)
    .filter((value) => Number.isFinite(value))
    .sort((a, b) => a - b);

  const p95Index = Math.min(
    latenciesMs.length - 1,
    Math.floor(latenciesMs.length * 0.95)
  );
  const p95 = latenciesMs[p95Index] || 0;
  statP95.textContent = `${p95.toFixed(1)} ms`;

  const errors = entries.filter((entry) => Number(entry.status) >= 400).length;
  const errorRate = (errors / entries.length) * 100;
  statError.textContent = `${errorRate.toFixed(1)}%`;
}

function statusBadgeClass(status) {
  const numeric = Number(status || 0);
  if (numeric >= 500) {
    return "err";
  }
  if (numeric >= 400) {
    return "warn";
  }
  return "ok";
}

function renderTable(entries) {
  rows.innerHTML = "";

  const formatter = new Intl.DateTimeFormat(undefined, {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });

  entries.slice(0, 200).forEach((entry) => {
    const tr = document.createElement("tr");

    const timeCell = document.createElement("td");
    timeCell.textContent = formatter.format(new Date(entry.timestamp));

    const methodCell = document.createElement("td");
    methodCell.textContent = entry.method || "-";

    const pathCell = document.createElement("td");
    pathCell.className = "path-cell";
    pathCell.textContent = entry.path || "-";

    const statusCell = document.createElement("td");
    const badge = document.createElement("span");
    badge.className = `badge ${statusBadgeClass(entry.status)}`;
    badge.textContent = entry.status ? String(entry.status) : "-";
    statusCell.appendChild(badge);

    const durationCell = document.createElement("td");
    durationCell.textContent = formatDuration(Number(entry.duration_ns || 0));

    const namespaceCell = document.createElement("td");
    namespaceCell.textContent = entry.namespace || "-";

    const podCell = document.createElement("td");
    podCell.textContent = entry.pod || "-";

    const nodeCell = document.createElement("td");
    nodeCell.textContent = entry.node || "-";

    tr.append(
      timeCell,
      methodCell,
      pathCell,
      statusCell,
      durationCell,
      namespaceCell,
      podCell,
      nodeCell
    );
    rows.appendChild(tr);
  });
}

function renderChart(entries) {
  const ctx = chart.getContext("2d");
  const dpr = window.devicePixelRatio || 1;
  const width = Math.max(1, Math.floor(chart.clientWidth * dpr));
  const height = Math.max(1, Math.floor(180 * dpr));

  if (chart.width !== width || chart.height !== height) {
    chart.width = width;
    chart.height = height;
  }

  ctx.clearRect(0, 0, width, height);

  const padding = { top: 14 * dpr, right: 16 * dpr, bottom: 18 * dpr, left: 16 * dpr };
  const innerWidth = width - padding.left - padding.right;
  const innerHeight = height - padding.top - padding.bottom;

  if (entries.length === 0) {
    ctx.fillStyle = "#64748b";
    ctx.font = `${12 * dpr}px KaTeXMain`;
    ctx.fillText("No telemetry data", padding.left, padding.top + 14 * dpr);
    return;
  }

  const buckets = new Map();
  entries.forEach((entry) => {
    const ts = new Date(entry.timestamp);
    ts.setSeconds(0, 0);
    const key = ts.toISOString();
    buckets.set(key, (buckets.get(key) || 0) + 1);
  });

  const points = Array.from(buckets.entries())
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([label, value]) => ({ label, value }));

  const maxValue = Math.max(1, ...points.map((point) => point.value));

  ctx.strokeStyle = "#e1e8f2";
  ctx.lineWidth = 1;
  for (let line = 0; line <= 4; line += 1) {
    const y = padding.top + (innerHeight / 4) * line;
    ctx.beginPath();
    ctx.moveTo(padding.left, y);
    ctx.lineTo(width - padding.right, y);
    ctx.stroke();
  }

  const xStep = points.length > 1 ? innerWidth / (points.length - 1) : innerWidth;

  const coords = points.map((point, index) => {
    const x = padding.left + xStep * index;
    const y = padding.top + innerHeight - (point.value / maxValue) * innerHeight;
    return { x, y };
  });

  const gradient = ctx.createLinearGradient(0, padding.top, 0, height - padding.bottom);
  gradient.addColorStop(0, "rgba(13, 99, 214, 0.26)");
  gradient.addColorStop(1, "rgba(13, 99, 214, 0.02)");

  ctx.beginPath();
  coords.forEach((point, index) => {
    if (index === 0) {
      ctx.moveTo(point.x, point.y);
    } else {
      ctx.lineTo(point.x, point.y);
    }
  });
  ctx.lineTo(coords[coords.length - 1].x, height - padding.bottom);
  ctx.lineTo(coords[0].x, height - padding.bottom);
  ctx.closePath();
  ctx.fillStyle = gradient;
  ctx.fill();

  ctx.beginPath();
  coords.forEach((point, index) => {
    if (index === 0) {
      ctx.moveTo(point.x, point.y);
    } else {
      ctx.lineTo(point.x, point.y);
    }
  });
  ctx.strokeStyle = "#0d63d6";
  ctx.lineWidth = 2 * dpr;
  ctx.stroke();

  ctx.fillStyle = "#0d63d6";
  coords.forEach((point) => {
    ctx.beginPath();
    ctx.arc(point.x, point.y, 2.5 * dpr, 0, Math.PI * 2);
    ctx.fill();
  });
}

async function fetchLogs() {
  const params = buildParams();
  const response = await fetch(`/api/logs?${params.toString()}`);
  if (!response.ok) {
    throw new Error(`HTTP ${response.status}`);
  }
  const data = await response.json();
  return data.entries || [];
}

function updateMeta(entries) {
  resultMeta.textContent = `${entries.length} entries`;
  lastUpdated.textContent = new Date().toLocaleTimeString();
}

async function refresh({ fromAuto = false } = {}) {
  if (isRefreshing) {
    return;
  }

  isRefreshing = true;
  refreshBtn.disabled = true;

  if (autoRange && (fromAuto || !inputs.from.value || !inputs.to.value)) {
    setAutoRangeWindow();
  }

  setDataState("loading");

  try {
    const entries = await fetchLogs();
    lastEntries = entries;

    renderStats(entries);
    renderChart(entries);
    updateMeta(entries);

    if (entries.length === 0) {
      rows.innerHTML = "";
      setDataState("empty");
    } else {
      renderTable(entries);
      setDataState("ready");
    }
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    setDataState("error", `Query failed (${message}).`);
  } finally {
    isRefreshing = false;
    refreshBtn.disabled = false;
  }
}

function setAutoRefreshTimer() {
  if (refreshTimer) {
    clearInterval(refreshTimer);
    refreshTimer = null;
  }

  if (autoRefreshInput.checked) {
    refreshTimer = setInterval(() => {
      refresh({ fromAuto: true });
    }, AUTO_REFRESH_MS);
  }
}

function initInputHandlers() {
  [inputs.from, inputs.to].forEach((input) => {
    input.addEventListener("input", () => {
      autoRange = false;
    });
  });

  [inputs.method, inputs.status, inputs.namespace, inputs.pod, inputs.path].forEach((input) => {
    input.addEventListener("keydown", (event) => {
      if (event.key === "Enter") {
        refresh({ fromAuto: false });
      }
    });
  });
}

function init() {
  setAutoRangeWindow();
  initInputHandlers();

  refreshBtn.addEventListener("click", () => {
    refresh({ fromAuto: false });
  });

  autoRefreshInput.addEventListener("change", () => {
    setAutoRefreshTimer();
  });

  setAutoRefreshTimer();
  refresh({ fromAuto: false });
}

window.addEventListener("load", init);
window.addEventListener("resize", () => {
  renderChart(lastEntries);
});
