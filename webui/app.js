const $ = (s) => document.querySelector(s),
  login = $("#login"),
  app = $("#app"),
  files = $("#files"),
  empty = $("#empty"),
  menu = $("#menu");
let token = sessionStorage.token || "",
  path = "",
  selected = null,
  readOnly = false,
  current = [],
  view = "files",
  timer;
const inviteCode = new URLSearchParams(location.search).get("invite");
if (inviteCode) {
  login.classList.add("hidden");
  $("#register").classList.remove("hidden");
  $("#registerForm").onsubmit = async (e) => {
    e.preventDefault();
    const r = await fetch("/api/register", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        code: inviteCode,
        username: $("#regUser").value,
        password: $("#regPass").value,
      }),
    });
    if (!r.ok) {
      $("#regError").textContent = await r.text();
      return;
    }
    alert("注册成功，请使用新账号登录");
    location.href = "/";
  };
}
const api = async (url, opt = {}) => {
  opt.headers = { ...(opt.headers || {}), Authorization: "Bearer " + token };
  const r = await fetch(url, opt);
  if (r.status === 401) {
    sessionStorage.removeItem("token");
    location.reload();
    throw Error("登录已过期");
  }
  if (!r.ok) throw Error(await r.text());
  return r.headers.get("content-type")?.includes("json") ? r.json() : r;
};
$("#loginForm").onsubmit = async (e) => {
  e.preventDefault();
  $("#loginError").textContent = "";
  try {
    const r = await fetch("/api/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        username: $("#user").value,
        password: $("#pass").value,
      }),
    });
    if (!r.ok) throw Error(await r.text());
    const d = await r.json();
    token = d.token;
    readOnly = d.readOnly;
    sessionStorage.token = token;
    showApp();
  } catch (x) {
    $("#loginError").textContent = x.message;
  }
};
async function showApp() {
  login.classList.add("hidden");
  app.classList.remove("hidden");
  await load();
  stats();
  timer = setInterval(stats, 5000);
}
if (token) showApp();
async function load() {
  current =
    view === "trash"
      ? await api("/api/trash")
      : await api("/api/files?path=" + encodeURIComponent(path));
  render();
}
function render() {
  let data = [...current],
    sort = $("#sort").value;
  data.sort((a, b) =>
    sort === "time"
      ? new Date(b.modified || b.Deleted) - new Date(a.modified || a.Deleted)
      : sort === "size"
        ? (b.size || b.Size) - (a.size || a.Size)
        : b.isDir - a.isDir ||
          (a.name || a.Name).localeCompare(b.name || b.Name, "zh-CN"),
  );
  files.innerHTML = "";
  empty.classList.toggle("hidden", data.length > 0);
  data.forEach((x) => {
    const trash = view === "trash",
      name = x.name || x.Name,
      p = x.path || x.Original,
      row = document.createElement("div");
    row.className = "file";
    row.innerHTML = `<div class="filename"><i>${x.isDir || x.IsDir ? "▱" : "◇"}</i><b></b></div><span>${x.isDir || x.IsDir ? "—" : size(x.size || x.Size || 0)}</span><span>${new Date(x.modified || x.Deleted).toLocaleString()}</span><button class="more">⋯</button>`;
    row.querySelector("b").textContent = name;
    row.querySelector(".filename").onclick = () =>
      trash ? openMenu(event, x) : x.isDir ? go(p) : previewFile(x);
    row.querySelector(".more").onclick = (e) => openMenu(e, x);
    files.append(row);
  });
  crumbs();
}
function crumbs() {
  if (view === "trash") {
    $("#crumbs").textContent = "删除的文件可在这里恢复";
    return;
  }
  let parts = path.split("/").filter(Boolean),
    h = '<button data-p="">我的空间</button>',
    p = "";
  for (const x of parts) {
    p += (p ? "/" : "") + x;
    h += ` <span>›</span> <button data-p="${esc(p)}">${esc(x)}</button>`;
  }
  $("#crumbs").innerHTML = h;
  $("#crumbs")
    .querySelectorAll("button")
    .forEach((b) => (b.onclick = () => go(b.dataset.p)));
}
const go = (p) => {
    path = p;
    load();
  },
  size = (n) =>
    n < 1024
      ? n + " B"
      : n < 1048576
        ? (n / 1024).toFixed(1) + " KB"
        : n < 1073741824
          ? (n / 1048576).toFixed(1) + " MB"
          : (n / 1073741824).toFixed(1) + " GB",
  esc = (s) =>
    s.replace(
      /[&<>"']/g,
      (c) =>
        ({
          "&": "&amp;",
          "<": "&lt;",
          ">": "&gt;",
          '"': "&quot;",
          "'": "&#39;",
        })[c],
    );
$("#upload").onchange = async (e) => {
  if (readOnly) return toast("当前为只读模式");
  const f = new FormData();
  f.append("path", path);
  [...e.target.files].forEach((x) => f.append("files", x));
  toast("正在上传…");
  try {
    await api("/api/upload", { method: "POST", body: f });
    toast("上传完成");
    load();
    stats();
  } catch (x) {
    toast(x.message);
  }
  e.target.value = "";
};
$("#newFolder").onclick = async () => {
  if (readOnly) return toast("当前为只读模式");
  const n = prompt("新文件夹名称");
  if (n) {
    await json("/api/mkdir", { path, name: n });
    load();
  }
};
$("#search").oninput = async (e) => {
  const q = e.target.value.trim();
  if (!q) return load();
  current = await api("/api/search?q=" + encodeURIComponent(q));
  render();
};
$("#sort").onchange = render;
document.querySelectorAll("[data-view]").forEach(
  (b) =>
    (b.onclick = () => {
      view = b.dataset.view;
      document
        .querySelectorAll("[data-view]")
        .forEach((x) => x.classList.toggle("active", x === b));
      $("#pageTitle").textContent = view === "trash" ? "回收站" : "所有文件";
      document
        .querySelector(".actions")
        .classList.toggle("hidden", view === "trash");
      load();
    }),
);
function openMenu(e, x) {
  selected = x;
  menu.querySelector('[data-act="restore"]').style.display =
    view === "trash" ? "block" : "none";
  menu.querySelector('[data-act="delete"]').style.display =
    view === "trash" ? "none" : "block";
  menu.style.left = Math.min(e.clientX - 100, innerWidth - 145) + "px";
  menu.style.top = Math.min(e.clientY, innerHeight - 180) + "px";
  menu.classList.remove("hidden");
  e.stopPropagation();
}
document.onclick = (e) => {
  if (!e.target.classList.contains("more")) menu.classList.add("hidden");
};
menu.onclick = async (e) => {
  const a = e.target.dataset.act;
  if (!a) return;
  if (a === "preview") previewFile(selected);
  if (a === "download") download(selected.path);
  if (a === "rename") {
    const n = prompt("新名称", selected.name);
    if (n) {
      await json("/api/rename", { path: selected.path, name: n });
      load();
    }
  }
  if (a === "delete" && confirm(`将“${selected.name}”移入回收站？`)) {
    await json("/api/delete", { path: selected.path });
    load();
    stats();
  }
  if (a === "restore") {
    await json("/api/trash/restore", { ID: selected.ID });
    load();
    stats();
  }
};
async function previewFile(x) {
  if (x.isDir) return;
  const ext = (x.name.split(".").pop() || "").toLowerCase(),
    r = await api("/api/preview?path=" + encodeURIComponent(x.path)),
    blob = await r.blob(),
    u = URL.createObjectURL(blob),
    body = $("#previewBody");
  $("#previewName").textContent = x.name;
  if (["png", "jpg", "jpeg", "gif", "webp", "svg"].includes(ext))
    body.innerHTML = `<img src="${u}">`;
  else if (["mp4", "webm"].includes(ext))
    body.innerHTML = `<video src="${u}" controls autoplay></video>`;
  else if (["mp3", "wav", "ogg"].includes(ext))
    body.innerHTML = `<audio src="${u}" controls autoplay></audio>`;
  else if (ext === "pdf") body.innerHTML = `<iframe src="${u}"></iframe>`;
  else body.innerHTML = `<pre>${esc(await blob.text())}</pre>`;
  $("#preview").classList.remove("hidden");
}
$("#closePreview").onclick = () => $("#preview").classList.add("hidden");
function download(p) {
  fetch("/api/download?path=" + encodeURIComponent(p), {
    headers: { Authorization: "Bearer " + token },
  }).then(async (r) => {
    if (!r.ok) return toast("下载失败");
    const b = await r.blob(),
      a = document.createElement("a");
    a.href = URL.createObjectURL(b);
    a.download = p.split("/").pop();
    a.click();
    URL.revokeObjectURL(a.href);
  });
}
async function stats() {
  try {
    const s = await api("/api/stats");
    $("#sOnline").textContent = s.online;
    $("#sRequests").textContent = s.requests;
    $("#sTraffic").textContent = size(s.uploaded + s.downloaded);
    $("#sUptime").textContent =
      s.uptime < 3600
        ? Math.floor(s.uptime / 60) + " 分钟"
        : Math.floor(s.uptime / 3600) + " 小时";
  } catch {}
}
async function json(url, data) {
  try {
    return await api(url, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(data),
    });
  } catch (x) {
    toast(x.message);
  }
}
function toast(s) {
  const t = $("#toast");
  t.textContent = s;
  t.classList.add("show");
  setTimeout(() => t.classList.remove("show"), 2200);
}
$("#davBtn").onclick = () => {
  navigator.clipboard.writeText(location.origin + "/dav/");
  toast("WebDAV 地址已复制");
};
