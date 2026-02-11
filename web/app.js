const rows = document.getElementById("rows");
const refreshBtn = document.getElementById("refresh");
const statCount = document.getElementById("stat-count");
const statP95 = document.getElementById("stat-p95");
const statError = document.getElementById("stat-error");
const chart = document.getElementById("chart");

const inputs = {
  from: document.getElementById("from"),
  to: document.getElementById("to"),
  method: document.getElementById("method"),
  status: document.getElementById("status"),
  namespace: document.getElementById("namespace"),
  pod: document.getElementById("pod"),
  path: document.getElementById("path"),
};

function toLocalInputValue(date) {
  const pad = (num) => String(num).padStart(2, "0");
  return (
    `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}` +
    `T${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`
  );
}

function buildParams() {
  const params = new URLSearchParams();
  if (inputs.from.value) {
    params.set("from", new Date(inputs.from.value).toISOString());
  }
  if (inputs.to.value) {
    params.set("to", new Date(inputs.to.value).toISOString());
  }
  if (inputs.method.value) params.set("method", inputs.method.value);
  if (inputs.status.value) params.set("status", inputs.status.value);
  if (inputs.namespace.value) params.set("namespace", inputs.namespace.value);
  if (inputs.pod.value) params.set("pod", inputs.pod.value);
  if (inputs.path.value) params.set("path", inputs.path.value);
  params.set("limit", "200");
  return params;
}

let lastEntries = [];
let autoRange = true;

function setAutoRange() {
  const now = new Date();
  const from = new Date(now.getTime() - 15 * 60 * 1000);
  inputs.from.value = toLocalInputValue(from);
  inputs.to.value = toLocalInputValue(now);
}

async function fetchLogs() {
  const params = buildParams();
  const response = await fetch(`/api/logs?${params.toString()}`);
  if (!response.ok) {
    throw new Error("Query failed");
  }
  const data = await response.json();
  return data.entries || [];
}

function renderStats(entries) {
  statCount.textContent = entries.length.toString();

  if (entries.length === 0) {
    statP95.textContent = "0 ms";
    statError.textContent = "0%";
    return;
  }

  const durations = entries
    .map((entry) => entry.duration_ns / 1e6)
    .sort((a, b) => a - b);
  const p95Index = Math.floor(durations.length * 0.95);
  const p95 = durations[p95Index] || 0;
  statP95.textContent = `${p95.toFixed(1)} ms`;

  const errors = entries.filter((entry) => entry.status >= 400).length;
  const errorRate = (errors / entries.length) * 100;
  statError.textContent = `${errorRate.toFixed(1)}%`;
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
    tr.innerHTML = `
      <td>${formatter.format(new Date(entry.timestamp))}</td>
      <td>${entry.method || "-"}</td>
      <td>${entry.path || "-"}</td>
      <td>${entry.status || "-"}</td>
      <td>${(entry.duration_ns / 1e6).toFixed(1)} ms</td>
      <td>${entry.namespace || "-"}</td>
      <td>${entry.pod || "-"}</td>
      <td>${entry.node || "-"}</td>
    `;
    rows.appendChild(tr);
  });
}

function renderChart(entries) {
  const ctx = chart.getContext("2d");
  const width = chart.width = chart.clientWidth * window.devicePixelRatio;
  const height = chart.height = 140 * window.devicePixelRatio;
  ctx.clearRect(0, 0, width, height);

  const buckets = new Map();
  entries.forEach((entry) => {
    const ts = new Date(entry.timestamp);
    ts.setSeconds(0, 0);
    const key = ts.toISOString();
    buckets.set(key, (buckets.get(key) || 0) + 1);
  });

  const keys = Array.from(buckets.keys()).sort();
  const values = keys.map((k) => buckets.get(k));
  const max = Math.max(1, ...values);

  const barWidth = width / Math.max(values.length, 1);
  ctx.fillStyle = "#f07a2a";
  values.forEach((val, index) => {
    const barHeight = (val / max) * (height - 20);
    const x = index * barWidth + 4;
    const y = height - barHeight - 10;
    ctx.fillRect(x, y, barWidth - 8, barHeight);
  });
}

async function refresh() {
  refreshBtn.disabled = true;
  try {
    if (autoRange) {
      setAutoRange();
    }
    lastEntries = await fetchLogs();
    renderStats(lastEntries);
    renderTable(lastEntries);
    renderChart(lastEntries);
  } catch (err) {
    console.error(err);
  } finally {
    refreshBtn.disabled = false;
  }
}

function init() {
  setAutoRange();

  inputs.from.addEventListener("input", () => {
    autoRange = false;
  });
  inputs.to.addEventListener("input", () => {
    autoRange = false;
  });

  refreshBtn.addEventListener("click", refresh);
  refresh();
}

window.addEventListener("load", init);
window.addEventListener("resize", () => {
  if (lastEntries.length) {
    renderChart(lastEntries);
  }
});
