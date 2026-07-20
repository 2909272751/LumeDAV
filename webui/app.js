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
  mode = "grid",
  pageOffset = 0,
  hasMore = false,
  pageLoading = false;
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
const invite = new URLSearchParams(location.search).get("invite");
if (invite) {
  login.classList.add("hidden");
  $("#register").classList.remove("hidden");
  $("#registerForm").onsubmit = async (e) => {
    e.preventDefault();
    const r = await fetch("/api/register", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        code: invite,
        username: $("#regUser").value,
        password: $("#regPass").value,
      }),
    });
    if (!r.ok) return ($("#regError").textContent = await r.text());
    alert("注册成功，请使用新账号登录");
    location.href = "/";
  };
}
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
  await buildTree();
}
if (token) showApp();
async function load(reset = true) {
  if (pageLoading) return;
  pageLoading = true;
  try {
    if (view === "trash") {
      current = (await api("/api/trash")) || [];
      hasMore = false;
      pageOffset = 0;
    } else {
      if (reset) {
        current = [];
        pageOffset = 0;
      }
      const q = `/api/files-page?path=${encodeURIComponent(path)}&offset=${pageOffset}&limit=200&sort=${encodeURIComponent($("#sort").value)}`,
        page = await api(q);
      current = reset ? page.items : [...current, ...page.items];
      hasMore = page.hasMore;
      pageOffset = current.length;
    }
    render();
  } finally {
    pageLoading = false;
  }
}
function render() {
  const sort = $("#sort").value,
    data = [...current];
  data.sort((a, b) =>
    sort === "time"
      ? new Date(b.modified || b.Deleted) - new Date(a.modified || a.Deleted)
      : sort === "size"
        ? (b.size || b.Size || 0) - (a.size || a.Size || 0)
        : (b.isDir || b.IsDir) - (a.isDir || a.IsDir) ||
          name(a).localeCompare(name(b), "zh-CN"),
  );
  files.innerHTML = "";
  empty.classList.toggle("hidden", data.length > 0);
  $("#itemCount").textContent = data.length + " 个项目";
  $("#loadMoreWrap").classList.toggle("hidden", !hasMore || view === "trash");
  $("#loadHint").textContent = hasMore
    ? `已加载 ${data.length} 项，每次加载 200 项`
    : "";
  data.forEach((x) => {
    const n = name(x),
      dir = x.isDir || x.IsDir,
      p = x.path || x.Original || "",
      row = document.createElement("div");
    row.className = "file";
    row.innerHTML = `<div class="fileMain"><div class="fileIcon">${icon(n, dir)}</div><span class="fileName"></span></div><span class="fileMeta">${dir ? "文件夹" : size(x.size || x.Size || 0)}</span><span class="fileType">${dir ? "文件夹" : fileType(n)}</span><span class="fileDate">${new Date(x.modified || x.Deleted).toLocaleString()}</span><button class="more">⋯</button>`;
    row.querySelector(".fileName").textContent = n;
    row.querySelector(".fileMain").onclick = (e) =>
      view === "trash" ? openMenu(e, x) : dir ? go(p) : previewFile(x);
    row.querySelector(".more").onclick = (e) => openMenu(e, x);
    files.append(row);
  });
  crumbs();
}
function name(x) {
  return x.name || x.Name || "";
}
function icon(n, dir) {
  if (dir)
    return `<svg viewBox="0 0 64 64"><path fill="#ffb900" d="M5 15a6 6 0 0 1 6-6h14l6 7h22a6 6 0 0 1 6 6v27a7 7 0 0 1-7 7H12a7 7 0 0 1-7-7z"/><path fill="#ffd25c" d="M5 24h54v25a7 7 0 0 1-7 7H12a7 7 0 0 1-7-7z"/></svg>`;
  const ext = n.split(".").pop().toLowerCase(),
    color =
      {
        pdf: "#f05252",
        jpg: "#22a06b",
        jpeg: "#22a06b",
        png: "#22a06b",
        gif: "#22a06b",
        mp4: "#8b5cf6",
        mp3: "#ec4899",
        zip: "#f59e0b",
        rar: "#f59e0b",
        txt: "#1677ff",
        doc: "#1677ff",
        docx: "#1677ff",
        xls: "#16a36a",
        xlsx: "#16a36a",
      }[ext] || "#5b82d8";
  return `<svg viewBox="0 0 64 64"><path fill="${color}" d="M14 4h24l13 13v39a5 5 0 0 1-5 5H14a5 5 0 0 1-5-5V9a5 5 0 0 1 5-5z"/><path fill="#fff" opacity=".85" d="M38 4v14h13z"/><path stroke="#fff" stroke-width="3" stroke-linecap="round" opacity=".8" d="M19 34h22M19 42h18"/></svg>`;
}
function fileType(n) {
  const e = n.includes(".") ? n.split(".").pop().toUpperCase() : "文件";
  return e + " 文件";
}
function go(p) {
  path = p;
  load();
  markTree();
  if (innerWidth < 760) $("#sidebar").classList.remove("open");
}
function crumbs() {
  if (view === "trash") {
    $("#crumbs").textContent = "已删除项目";
    return;
  }
  const ps = path.split("/").filter(Boolean);
  let h = '<button data-p="">我的文件</button>',
    p = "";
  for (const x of ps) {
    p += (p ? "/" : "") + x;
    h += ` <span> / </span><button data-p="${esc(p)}">${esc(x)}</button>`;
  }
  $("#crumbs").innerHTML = h;
  $("#crumbs")
    .querySelectorAll("button")
    .forEach((b) => (b.onclick = () => go(b.dataset.p)));
}
async function buildTree() {
  const roots = (await api("/api/files?path=")) || [],
    $tree = $("#tree");
  $tree.innerHTML = "";
  roots
    .filter((x) => x.isDir)
    .forEach((x) => {
      const b = document.createElement("button");
      b.dataset.path = x.path;
      b.innerHTML = `<span class="folderMini">▰</span>${esc(x.name)}`;
      b.onclick = () => go(x.path);
      $tree.append(b);
    });
  markTree();
}
function markTree() {
  $("#tree")
    .querySelectorAll("button")
    .forEach((b) =>
      b.classList.toggle(
        "active",
        path === b.dataset.path || path.startsWith(b.dataset.path + "/"),
      ),
    );
}
$("#upload").onchange = async (e) => {
  if (readOnly) return toast("当前账号为只读权限");
  const f = new FormData();
  f.append("path", path);
  [...e.target.files].forEach((x) => f.append("files", x));
  toast("正在上传…");
  try {
    await api("/api/upload", { method: "POST", body: f });
    toast("上传完成");
    load();
  } catch (x) {
    toast(x.message);
  }
  e.target.value = "";
};
$("#newFolder").onclick = async () => {
  if (readOnly) return toast("当前账号为只读权限");
  const n = prompt("新文件夹名称");
  if (n) {
    await json("/api/mkdir", { path, name: n });
    load();
  }
};
$("#refreshBtn").onclick = load;
$("#treeRefresh").onclick = buildTree;
$("#sort").onchange = () => load(true);
$("#loadMore").onclick = () => load(false);
$("#search").oninput = async (e) => {
  const q = e.target.value.trim();
  if (!q) return load();
  current = (await api("/api/search?q=" + encodeURIComponent(q))) || [];
  hasMore = false;
  pageOffset = current.length;
  render();
};
$("#gridView").onclick = () => setMode("grid");
$("#listView").onclick = () => setMode("list");
function setMode(m) {
  mode = m;
  $("#filePanel").className =
    "filePanel " + (m === "grid" ? "gridMode" : "listMode");
  $("#gridView").classList.toggle("active", m === "grid");
  $("#listView").classList.toggle("active", m === "list");
}
document.querySelectorAll("[data-view]").forEach(
  (b) =>
    (b.onclick = () => {
      view = b.dataset.view;
      document
        .querySelectorAll("[data-view]")
        .forEach((x) => x.classList.toggle("active", x === b));
      $("#pageTitle").textContent = view === "trash" ? "回收站" : "我的文件";
      document
        .querySelector(".headActions")
        .classList.toggle("hidden", view === "trash");
      load();
    }),
);
$("#menuToggle").onclick = () => $("#sidebar").classList.toggle("open");
function openMenu(e, x) {
  selected = x;
  menu.querySelector('[data-act="restore"]').style.display =
    view === "trash" ? "block" : "none";
  menu.querySelector('[data-act="delete"]').style.display =
    view === "trash" ? "none" : "block";
  menu.style.left = Math.min(e.clientX - 100, innerWidth - 150) + "px";
  menu.style.top = Math.min(e.clientY, innerHeight - 190) + "px";
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
  }
  if (a === "restore") {
    await json("/api/trash/restore", { ID: selected.ID });
    load();
  }
};
async function previewFile(x) {
  if (x.isDir) return;
  try {
    const ext = name(x).split(".").pop().toLowerCase();
    if (ext === "dwg") toast("正在请求主机生成 CAD 预览…");
    const officeFormats = [
      "doc",
      "docx",
      "xls",
      "xlsx",
      "ppt",
      "pptx",
      "odt",
      "ods",
      "odp",
    ];
    const endpoint =
        ext === "dwg"
          ? "/api/cad-preview"
          : officeFormats.includes(ext)
            ? "/api/office-preview"
            : "/api/preview",
      r = await api(endpoint + "?path=" + encodeURIComponent(x.path)),
      blob = await r.blob(),
      u = URL.createObjectURL(blob),
      body = $("#previewBody");
    $("#previewName").textContent = name(x);
    if (officeFormats.includes(ext))
      body.innerHTML = `<iframe src="${u}"></iframe>`;
    else if (ext === "dwg") {
      body.innerHTML = `<div class="cadTools"><button data-cad="out">−</button><span id="cadZoom">100%</span><button data-cad="in">＋</button><button data-cad="fit">适应窗口</button><button data-cad="full">全屏</button></div><div class="cadViewport"><img src="${u}" draggable="false"></div>`;
      setupCAD(body);
    } else if (["png", "jpg", "jpeg", "gif", "webp", "svg"].includes(ext))
      body.innerHTML = `<img src="${u}">`;
    else if (["mp4", "webm"].includes(ext))
      body.innerHTML = `<video src="${u}" controls autoplay></video>`;
    else if (["mp3", "wav", "ogg"].includes(ext))
      body.innerHTML = `<audio src="${u}" controls autoplay></audio>`;
    else if (ext === "pdf") body.innerHTML = `<iframe src="${u}"></iframe>`;
    else body.innerHTML = `<pre>${esc(await blob.text())}</pre>`;
    $("#preview").classList.remove("hidden");
  } catch (x) {
    toast(x.message);
  }
}
function setupCAD(body) {
  const vp = body.querySelector(".cadViewport"),
    img = vp.querySelector("img"),
    label = body.querySelector("#cadZoom");
  let scale = 1,
    x = 0,
    y = 0,
    drag = false,
    sx = 0,
    sy = 0,
    lastDist = 0;
  const draw = () => {
      img.style.transform = `translate(${x}px,${y}px) scale(${scale})`;
      label.textContent = Math.round(scale * 100) + "%";
    },
    zoom = (factor, cx = vp.clientWidth / 2, cy = vp.clientHeight / 2) => {
      const ns = Math.min(12, Math.max(0.15, scale * factor));
      x = cx - (cx - x) * (ns / scale);
      y = cy - (cy - y) * (ns / scale);
      scale = ns;
      draw();
    };
  vp.onwheel = (e) => {
    e.preventDefault();
    const r = vp.getBoundingClientRect();
    zoom(e.deltaY < 0 ? 1.15 : 0.87, e.clientX - r.left, e.clientY - r.top);
  };
  vp.onpointerdown = (e) => {
    drag = true;
    sx = e.clientX - x;
    sy = e.clientY - y;
    vp.setPointerCapture(e.pointerId);
  };
  vp.onpointermove = (e) => {
    if (drag) {
      x = e.clientX - sx;
      y = e.clientY - sy;
      draw();
    }
  };
  vp.onpointerup = () => (drag = false);
  vp.ontouchmove = (e) => {
    if (e.touches.length === 2) {
      e.preventDefault();
      const d = Math.hypot(
        e.touches[0].clientX - e.touches[1].clientX,
        e.touches[0].clientY - e.touches[1].clientY,
      );
      if (lastDist) zoom(d / lastDist);
      lastDist = d;
    }
  };
  vp.ontouchend = () => (lastDist = 0);
  body.querySelectorAll("[data-cad]").forEach(
    (b) =>
      (b.onclick = () => {
        const a = b.dataset.cad;
        if (a === "in") zoom(1.25);
        if (a === "out") zoom(0.8);
        if (a === "fit") {
          scale = 1;
          x = 0;
          y = 0;
          draw();
        }
        if (a === "full") body.closest(".preview").requestFullscreen?.();
      }),
  );
  draw();
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
function size(n) {
  return n < 1024
    ? n + " B"
    : n < 1048576
      ? (n / 1024).toFixed(1) + " KB"
      : n < 1073741824
        ? (n / 1048576).toFixed(1) + " MB"
        : (n / 1073741824).toFixed(1) + " GB";
}
function esc(s) {
  return String(s).replace(
    /[&<>"']/g,
    (c) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[
        c
      ],
  );
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
