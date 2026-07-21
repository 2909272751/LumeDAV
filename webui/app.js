const $ = (s) => document.querySelector(s),
  login = $("#login"),
  app = $("#app"),
  files = $("#files"),
  empty = $("#empty"),
  menu = $("#menu");
let token = sessionStorage.token || "",
  currentUsername = sessionStorage.lumedavUser || "",
  path = "",
  selected = null,
  readOnly = false,
  current = [],
  view = "files",
  mode = "grid",
  pageOffset = 0,
  hasMore = false,
  pageLoading = false,
  loadRequest = 0,
  archiveTask = null,
  archivePoll = 0,
  archiveDownloadStarted = false,
  folderSizeAbort = null,
  fileDownloadAbort = null,
  previewAbort = null,
  activePreviewURL = "",
  activePreviewCleanup = null;
const api = async (url, opt = {}) => {
  opt.headers = { ...(opt.headers || {}), Authorization: "Bearer " + token };
  const r = await fetch(url, opt);
  if (r.status === 401) {
    sessionStorage.removeItem("token");
    sessionStorage.removeItem("lumedavUser");
    location.reload();
    throw Error("登录已过期");
  }
  if (!r.ok) throw Error(await r.text());
  return r.headers.get("content-type")?.includes("json") ? r.json() : r;
};
fetch("/api/version")
  .then((response) => response.json())
  .then(({ version }) => {
    $("#webVersion").textContent = "v" + version;
    document.title = "LumeDAV v" + version;
  })
  .catch(() => {});
const rememberedCredentialsKey = "lumedav.rememberedCredentials",
  invite = new URLSearchParams(location.search).get("invite") || "";
function showAuthPanel(panel) {
  login.classList.toggle("hidden", panel !== "login");
  $("#register").classList.toggle("hidden", panel !== "register");
  $("#loginError").textContent = "";
  $("#regError").textContent = "";
}
function removeInviteFromURL() {
  const url = new URL(location.href);
  if (!url.searchParams.has("invite")) return;
  url.searchParams.delete("invite");
  history.replaceState(history.state, "", url.pathname + url.search + url.hash);
}
try {
  const remembered = JSON.parse(
    localStorage.getItem(rememberedCredentialsKey) || "null",
  );
  if (remembered?.username && typeof remembered.password === "string") {
    $("#user").value = remembered.username;
    $("#pass").value = remembered.password;
    $("#rememberPassword").checked = true;
  }
} catch {
  try {
    localStorage.removeItem(rememberedCredentialsKey);
  } catch {}
}
$("#rememberPassword").onchange = (event) => {
  if (event.target.checked) return;
  try {
    localStorage.removeItem(rememberedCredentialsKey);
  } catch {}
};
$("#showRegister").onclick = () => showAuthPanel("register");
$("#backToLogin").onclick = () => {
  removeInviteFromURL();
  showAuthPanel("login");
};
$("#registerForm").onsubmit = async (e) => {
  e.preventDefault();
  $("#regError").textContent = "";
  const code = $("#regInvite").value.trim(),
    username = $("#regUser").value.trim(),
    password = $("#regPass").value,
    confirmPassword = $("#regPassConfirm").value,
    button = $("#registerForm .authPrimary");
  if (!code) return ($("#regError").textContent = "请输入管理员邀请码");
  if (!username || !password)
    return ($("#regError").textContent = "账号和密码不能为空");
  if (password !== confirmPassword)
    return ($("#regError").textContent = "两次输入的密码不一致");
  button.disabled = true;
  button.textContent = "正在注册…";
  try {
    const response = await fetch("/api/register", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ code, username, password }),
    });
    if (!response.ok) throw Error(await response.text());
    $("#user").value = username;
    $("#pass").value = "";
    $("#regPass").value = "";
    $("#regPassConfirm").value = "";
    removeInviteFromURL();
    showAuthPanel("login");
    $("#loginMessage").textContent = "注册成功，请使用新账号登录";
  } catch (error) {
    $("#regError").textContent = error.message;
  } finally {
    button.disabled = false;
    button.textContent = "完成注册";
  }
};
if (invite) {
  $("#regInvite").value = invite;
  showAuthPanel("register");
}
$("#loginForm").onsubmit = async (e) => {
  e.preventDefault();
  $("#loginError").textContent = "";
  $("#loginMessage").textContent = "";
  const username = $("#user").value.trim(),
    password = $("#pass").value,
    button = $("#loginForm .authPrimary");
  button.disabled = true;
  button.textContent = "正在登录…";
  try {
    const r = await fetch("/api/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        username,
        password,
      }),
    });
    if (!r.ok) throw Error(await r.text());
    const d = await r.json();
    token = d.token;
    readOnly = d.readOnly;
    sessionStorage.token = token;
    currentUsername = username;
    sessionStorage.lumedavUser = username;
    try {
      if ($("#rememberPassword").checked) {
        localStorage.setItem(
          rememberedCredentialsKey,
          JSON.stringify({ username, password }),
        );
      } else {
        localStorage.removeItem(rememberedCredentialsKey);
      }
    } catch {}
    showApp();
  } catch (x) {
    $("#loginError").textContent = x.message;
  } finally {
    button.disabled = false;
    button.textContent = "登录";
  }
};
async function showApp() {
  login.classList.add("hidden");
  $("#register").classList.add("hidden");
  app.classList.remove("hidden");
  updateAccountUI();
  const saved = history.state;
  if (saved?.lumeNavigation) {
    path = typeof saved.path === "string" ? saved.path : "";
    view = saved.view === "trash" ? "trash" : "files";
  }
  syncNavigationUI();
  await load();
  await buildTree();
  history.replaceState(navigationState(), "");
}
if (token) showApp();
async function load(reset = true) {
  if (pageLoading && !reset) return;
  const request = ++loadRequest,
    requestedPath = path,
    requestedView = view,
    requestedOffset = reset ? 0 : pageOffset;
  pageLoading = true;
  showFileLoading(
    reset ? "正在读取文件" : "正在加载更多",
    requestedView === "trash" ? "正在读取回收站" : "正在扫描当前目录",
  );
  try {
    if (requestedView === "trash") {
      const items = (await api("/api/trash")) || [];
      if (request !== loadRequest) return;
      current = items;
      hasMore = false;
      pageOffset = 0;
    } else {
      const q = `/api/files-page?path=${encodeURIComponent(requestedPath)}&offset=${requestedOffset}&limit=200&sort=${encodeURIComponent($("#sort").value)}`,
        page = await api(q);
      if (request !== loadRequest) return;
      current = reset ? page.items : [...current, ...page.items];
      hasMore = page.hasMore;
      pageOffset = current.length;
    }
    render();
  } finally {
    if (request === loadRequest) {
      pageLoading = false;
      hideFileLoading();
    }
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
  data.forEach((x, index) => {
    const n = name(x),
      dir = x.isDir || x.IsDir,
      p = x.path || x.Original || "",
      row = document.createElement("div");
    row.className = "file entering";
    row.style.animationDelay = Math.min(index, 16) * 18 + "ms";
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
function navigationState(extra = {}) {
  return { lumeNavigation: true, path, view, ...extra };
}
function syncNavigationUI() {
  document
    .querySelectorAll("[data-view]")
    .forEach((button) =>
      button.classList.toggle("active", button.dataset.view === view),
    );
  const parts = path.split("/").filter(Boolean),
    canGoBack = view === "trash" || parts.length > 0,
    back = $("#backBtn");
  $("#pageTitle").textContent =
    view === "trash" ? "回收站" : parts.at(-1) || "我的文件";
  document
    .querySelector(".headActions")
    .classList.toggle("hidden", view === "trash");
  back.disabled = !canGoBack;
  back.title = canGoBack
    ? view === "trash"
      ? "返回我的文件"
      : "返回上一级目录"
    : "已经在最上层";
}
function go(p, { record = true } = {}) {
  const next = typeof p === "string" ? p : "",
    changed = view !== "files" || path !== next;
  view = "files";
  path = next;
  $("#search").value = "";
  syncNavigationUI();
  if (record && changed) history.pushState(navigationState(), "");
  load(true);
  markTree();
  if (innerWidth < 760) $("#sidebar").classList.remove("open");
}
function setView(next, { record = true } = {}) {
  if (next !== "files" && next !== "trash") return;
  const changed = view !== next;
  view = next;
  $("#search").value = "";
  syncNavigationUI();
  if (record && changed) history.pushState(navigationState(), "");
  load(true);
}
function navigateUp() {
  if (view === "trash") return setView("files");
  const parts = path.split("/").filter(Boolean);
  if (!parts.length) return;
  go(parts.slice(0, -1).join("/"));
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
function showFileLoading(title, detail) {
  $("#fileLoadingTitle").textContent = title;
  $("#fileLoadingText").textContent = detail;
  $("#fileLoading").classList.remove("hidden");
}
function hideFileLoading() {
  $("#fileLoading").classList.add("hidden");
}
let operationTimer = 0;
function showOperation(title, detail, state = "busy", autoHide = 0) {
  clearTimeout(operationTimer);
  const panel = $("#operationStatus");
  $("#operationTitle").textContent = title;
  $("#operationText").textContent = detail;
  panel.className = "operationStatus " + state;
  if (autoHide)
    operationTimer = setTimeout(() => panel.classList.add("hidden"), autoHide);
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
  const count = e.target.files.length;
  if (!count) return;
  const f = new FormData();
  f.append("path", path);
  [...e.target.files].forEach((x) => f.append("files", x));
  showOperation("正在上传", `正在传输 ${count} 个文件，请保持页面打开`);
  try {
    await api("/api/upload", { method: "POST", body: f });
    showOperation("上传完成", `${count} 个文件已保存`, "success", 2400);
    toast("上传完成");
    load();
  } catch (x) {
    showOperation("上传失败", x.message, "error", 4500);
    toast(x.message);
  }
  e.target.value = "";
};
$("#newFolder").onclick = async () => {
  if (readOnly) return toast("当前账号为只读权限");
  const n = prompt("新文件夹名称");
  if (n) {
    showOperation("正在创建文件夹", n);
    try {
      await api("/api/mkdir", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ path, name: n }),
      });
      showOperation("创建完成", n, "success", 1800);
      load();
    } catch (error) {
      showOperation("创建失败", error.message, "error", 4000);
      toast(error.message);
    }
  }
};
$("#refreshBtn").onclick = load;
$("#treeRefresh").onclick = buildTree;
$("#sort").onchange = () => load(true);
$("#loadMore").onclick = () => load(false);
$("#search").oninput = async (e) => {
  const q = e.target.value.trim();
  if (!q) return load();
  const request = ++loadRequest;
  pageLoading = false;
  showFileLoading("正在搜索", `正在查找“${q}”`);
  try {
    const results = (await api("/api/search?q=" + encodeURIComponent(q))) || [];
    if (request !== loadRequest || e.target.value.trim() !== q) return;
    current = results;
    hasMore = false;
    pageOffset = current.length;
    render();
  } finally {
    if (request === loadRequest) hideFileLoading();
  }
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
  (button) => (button.onclick = () => setView(button.dataset.view)),
);
$("#backBtn").onclick = navigateUp;
window.addEventListener("popstate", (event) => {
  hidePreview();
  hideArchiveDialog();
  hideFileDownloadDialog();
  hideFolderSizeDialog(true);
  menu.classList.add("hidden");
  const state = event.state;
  if (!state?.lumeNavigation) return;
  const nextPath = typeof state.path === "string" ? state.path : "",
    nextView = state.view === "trash" ? "trash" : "files",
    changed = path !== nextPath || view !== nextView;
  path = nextPath;
  view = nextView;
  $("#search").value = "";
  syncNavigationUI();
  markTree();
  if (changed) load(true);
});
$("#menuToggle").onclick = () => $("#sidebar").classList.toggle("open");
function openMenu(e, x) {
  selected = x;
  const directory = isDirectory(x),
    inTrash = view === "trash";
  menu.querySelector('[data-act="preview"]').style.display =
    !inTrash && !directory ? "block" : "none";
  const downloadButton = menu.querySelector('[data-act="download"]');
  downloadButton.style.display = inTrash ? "none" : "block";
  downloadButton.textContent = directory ? "打包下载" : "下载";
  menu.querySelector('[data-act="size"]').style.display =
    !inTrash && directory ? "block" : "none";
  menu.querySelector('[data-act="rename"]').style.display = inTrash
    ? "none"
    : "block";
  menu.querySelector('[data-act="restore"]').style.display =
    inTrash ? "block" : "none";
  menu.querySelector('[data-act="delete"]').style.display =
    inTrash ? "none" : "block";
  menu.style.left = Math.max(8, Math.min(e.clientX - 100, innerWidth - 158)) + "px";
  menu.classList.remove("hidden");
  menu.style.top =
    Math.max(8, Math.min(e.clientY, innerHeight - menu.offsetHeight - 8)) + "px";
  e.stopPropagation();
}
document.onclick = (e) => {
  if (!e.target.classList.contains("more")) menu.classList.add("hidden");
  $("#accountMenu").classList.add("hidden");
  $("#accountBtn").setAttribute("aria-expanded", "false");
};
menu.onclick = async (e) => {
  const a = e.target.dataset.act;
  if (!a) return;
  if (a === "preview") previewFile(selected);
  if (a === "download") {
    if (isDirectory(selected)) startFolderDownload(selected);
    else download(selected.path);
  }
  if (a === "size") startFolderSize(selected);
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
function isDirectory(item) {
  return Boolean(item?.isDir || item?.IsDir);
}
async function startFolderSize(item) {
  if (!item?.path) return toast("无法识别文件夹路径");
  folderSizeAbort?.abort();
  const controller = new AbortController();
  folderSizeAbort = controller;
  $("#folderSizeTitle").textContent = name(item);
  $("#folderSizeState").textContent = "正在统计…";
  $("#folderSizeMessage").textContent = "正在读取文件夹内容，请稍候";
  $("#folderSizeProgress").classList.remove("hidden");
  $("#folderSizeResult").classList.add("hidden");
  $("#folderSizeCancel").textContent = "取消统计";
  $("#folderSizeHint").textContent = "回收站、缓存和符号链接不会计入大小。";
  $("#folderSizeDialog").classList.remove("hidden");
  if (!history.state?.lumeFolderSize)
    history.pushState(navigationState({ lumeFolderSize: true }), "");
  try {
    const result = await api(
      "/api/folder-size?path=" + encodeURIComponent(item.path),
      { signal: controller.signal },
    );
    if (folderSizeAbort !== controller) return;
    $("#folderSizeState").textContent = "统计完成";
    $("#folderSizeMessage").textContent = "已完成整个文件夹的大小统计";
    $("#folderSizeProgress").classList.add("hidden");
    $("#folderSizeResult").classList.remove("hidden");
    $("#folderSizeBytes").textContent = size(result.bytes || 0);
    $("#folderSizeFiles").textContent = formatCount(result.files || 0);
    $("#folderSizeFolders").textContent = formatCount(result.folders || 0);
    $("#folderSizeTime").textContent = formatDuration(result.durationMs || 0);
    $("#folderSizeCancel").textContent = "关闭";
    $("#folderSizeHint").textContent = result.skipped
      ? `有 ${result.skipped} 个系统、缓存或无法读取的项目未计入。`
      : "已统计全部可访问内容。";
  } catch (error) {
    if (folderSizeAbort !== controller) return;
    $("#folderSizeProgress").classList.add("hidden");
    $("#folderSizeState").textContent =
      error.name === "AbortError" ? "统计已取消" : "统计失败";
    $("#folderSizeMessage").textContent =
      error.name === "AbortError" ? "已停止读取文件夹" : error.message;
    $("#folderSizeCancel").textContent = "关闭";
  } finally {
    if (folderSizeAbort === controller) folderSizeAbort = null;
  }
}
function formatCount(value) {
  return new Intl.NumberFormat("zh-CN").format(value);
}
function formatDuration(milliseconds) {
  if (milliseconds < 1000)
    return milliseconds < 100 ? "<0.1 秒" : `${(milliseconds / 1000).toFixed(1)} 秒`;
  if (milliseconds < 60000) return `${(milliseconds / 1000).toFixed(1)} 秒`;
  return `${Math.floor(milliseconds / 60000)}分${Math.round((milliseconds % 60000) / 1000)}秒`;
}
function hideFolderSizeDialog(cancel = false) {
  if (cancel) folderSizeAbort?.abort();
  $("#folderSizeDialog").classList.add("hidden");
}
function closeFolderSizeDialog() {
  const hasHistoryEntry = history.state?.lumeFolderSize;
  hideFolderSizeDialog(true);
  if (hasHistoryEntry) history.back();
}
$("#folderSizeClose").onclick = closeFolderSizeDialog;
$("#folderSizeCancel").onclick = () => {
  if (folderSizeAbort) folderSizeAbort.abort();
  else closeFolderSizeDialog();
};
async function startFolderDownload(item) {
  if (!item?.path) return toast("无法识别文件夹路径");
  clearTimeout(archivePoll);
  archiveTask = null;
  archiveDownloadStarted = false;
  $("#archiveTitle").textContent = name(item) + ".zip";
  $("#archiveState").textContent = "正在创建任务";
  $("#archiveMessage").textContent = "准备扫描文件夹内容…";
  $("#archiveFiles").textContent = "0 个文件";
  $("#archiveSize").textContent = "0 B";
  $("#archivePercent").textContent = "0%";
  $("#archiveBar").style.width = "0%";
  $("#archiveBar").classList.add("indeterminate");
  $("#archiveCancel").classList.remove("hidden");
  $("#archiveDownload").classList.add("hidden");
  $("#archiveDialog").classList.remove("hidden");
  history.pushState(navigationState({ lumeArchive: true }), "");
  try {
    archiveTask = await api("/api/folder-download/start", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ path: item.path }),
    });
    updateArchiveDialog(archiveTask);
    scheduleArchivePoll(300);
  } catch (error) {
    showArchiveError(error.message);
  }
}
function scheduleArchivePoll(delay = 800) {
  clearTimeout(archivePoll);
  archivePoll = setTimeout(pollArchiveTask, delay);
}
async function pollArchiveTask() {
  if (!archiveTask?.id) return;
  try {
    archiveTask = await api(
      "/api/folder-download/status?id=" + encodeURIComponent(archiveTask.id),
    );
    updateArchiveDialog(archiveTask);
    if (["queued", "scanning", "packing", "ready", "downloading"].includes(archiveTask.status))
      scheduleArchivePoll();
  } catch (error) {
    showArchiveError(error.message);
  }
}
function updateArchiveDialog(task) {
  const labels = {
      queued: "等待打包",
      scanning: "正在扫描文件夹",
      packing: "正在生成压缩包",
      ready: "压缩包已准备完成",
      downloading: "正在传输下载",
      complete: "文件夹下载完成",
      cancelled: "任务已取消",
      error: "打包失败",
    },
    messages = {
      queued: "前面有任务时会自动排队，请稍候。",
      scanning: "正在统计文件数量和大小…",
      packing: "大文件夹正在本机临时打包，可安全离开此目录。",
      ready:
        task.mode === "stream"
          ? "此文件夹将边压缩边传输，不占用临时磁盘空间。"
          : "支持断点续传，临时压缩包将在传输后自动删除。",
      downloading: "浏览器正在接收文件，可以查看浏览器下载进度。",
      complete: "传输已完成，临时压缩包会在 10 分钟后清理。",
      cancelled: "临时文件已清理。",
      error: task.error || "请稍后重试。",
    },
    indeterminate = task.status === "queued" || task.status === "scanning",
    progress = Math.max(0, Math.min(100, task.progress || 0));
  $("#archiveState").textContent = labels[task.status] || "处理中";
  $("#archiveMessage").textContent = messages[task.status] || "请稍候…";
  $("#archiveFiles").textContent = `${task.processedFiles || task.totalFiles || 0} / ${task.totalFiles || 0} 个文件`;
  $("#archiveSize").textContent = `${size(task.processedBytes || 0)} / ${size(task.totalBytes || 0)}`;
  $("#archivePercent").textContent = progress + "%";
  $("#archiveBar").classList.toggle("indeterminate", indeterminate);
  $("#archiveBar").style.width = progress + "%";
  const cancellable = ["queued", "scanning", "packing", "ready"].includes(
    task.status,
  );
  $("#archiveCancel").classList.toggle("hidden", !cancellable);
  const canDownload = Boolean(task.downloadUrl);
  $("#archiveDownload").classList.toggle("hidden", !canDownload);
  $("#archiveDownload").textContent =
    task.status === "complete" ? "再次下载" : "开始下载";
  if (task.status === "ready" && canDownload && !archiveDownloadStarted) {
    triggerArchiveDownload();
  }
  if (task.status === "complete") toast("文件夹下载完成");
}
function triggerArchiveDownload() {
  if (!archiveTask?.downloadUrl) return;
  archiveDownloadStarted = true;
  const anchor = document.createElement("a");
  anchor.href = archiveTask.downloadUrl;
  anchor.download = archiveTask.name + ".zip";
  document.body.append(anchor);
  anchor.click();
  anchor.remove();
  $("#archiveDownload").textContent = "重新开始下载";
  toast("已交给浏览器下载");
}
function showArchiveError(message) {
  clearTimeout(archivePoll);
  $("#archiveBar").classList.remove("indeterminate");
  $("#archiveState").textContent = "打包失败";
  $("#archiveMessage").textContent = message || "请稍后重试";
  $("#archiveCancel").classList.add("hidden");
}
function hideArchiveDialog() {
  $("#archiveDialog").classList.add("hidden");
}
$("#archiveClose").onclick = () => {
  const hasHistoryEntry = history.state?.lumeArchive;
  hideArchiveDialog();
  if (hasHistoryEntry) history.back();
};
$("#archiveDownload").onclick = triggerArchiveDownload;
$("#archiveCancel").onclick = async () => {
  if (!archiveTask?.id) return;
  try {
    archiveTask = await api(
      "/api/folder-download/cancel?id=" + encodeURIComponent(archiveTask.id),
      { method: "POST" },
    );
    updateArchiveDialog(archiveTask);
    clearTimeout(archivePoll);
  } catch (error) {
    showArchiveError(error.message);
  }
};
async function previewFile(x) {
  if (x.isDir) return;
  const ext = name(x).split(".").pop().toLowerCase();
  if (ext === "dwg")
    return toast("DWG 在线预览已移除，请下载后使用 CAD 软件打开");
  clearPreviewResources();
  const body = $("#previewBody"),
    imageFormats = ["png", "jpg", "jpeg", "gif", "webp", "svg"],
    videoFormats = ["mp4", "m4v", "mov", "webm", "ogv"],
    officeFormats = [
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
  $("#previewName").textContent = name(x);
  body.innerHTML = `<div class="previewLoading"><i></i><b>正在准备预览</b><span>${officeFormats.includes(ext) ? "正在转换 Office 文档，首次打开可能稍慢" : "正在读取文件内容"}</span></div>`;
  history.pushState(navigationState({ lumePreview: true }), "");
  $("#preview").classList.remove("hidden");
  if (videoFormats.includes(ext)) {
    showVideoPreview(body, x);
    return;
  }
  const controller = new AbortController();
  previewAbort = controller;
  try {
    const endpoint = officeFormats.includes(ext)
          ? "/api/office-preview"
          : "/api/preview",
      r = await api(endpoint + "?path=" + encodeURIComponent(x.path), {
        signal: controller.signal,
      }),
      blob = await r.blob(),
      u = URL.createObjectURL(blob);
    if (previewAbort !== controller) {
      URL.revokeObjectURL(u);
      return;
    }
    setPreviewURL(u);
    if (officeFormats.includes(ext))
      body.innerHTML = `<iframe src="${u}"></iframe>`;
    else if (imageFormats.includes(ext)) setupImagePreview(body, u);
    else if (["mp3", "wav", "ogg"].includes(ext))
      body.innerHTML = `<audio src="${u}" controls autoplay></audio>`;
    else if (ext === "pdf") body.innerHTML = `<iframe src="${u}"></iframe>`;
    else body.innerHTML = `<pre>${esc(await blob.text())}</pre>`;
  } catch (x) {
    if (x.name === "AbortError") return;
    if (!$("#preview").classList.contains("hidden"))
      body.innerHTML = `<div class="previewLoading failed"><b>预览失败</b><span>${esc(x.message)}</span></div>`;
    toast(x.message);
  } finally {
    if (previewAbort === controller) previewAbort = null;
  }
}

function showVideoPreview(body, item) {
  const source = "/api/preview?path=" + encodeURIComponent(item.path);
  body.innerHTML = `<div class="videoPreview"><video controls playsinline preload="metadata" src="${source}"></video><div class="videoHint"><span>流式播放</span><span>支持拖动进度、音量和全屏</span></div></div>`;
  const video = body.querySelector("video");
  video.onerror = () => {
    const detail = video.error?.message || "浏览器不支持该视频编码，请下载后播放";
    body.innerHTML = `<div class="previewLoading failed"><b>视频无法播放</b><span>${esc(detail)}</span></div>`;
  };
  activePreviewCleanup = () => {
    video.pause();
    video.removeAttribute("src");
    video.load();
  };
}

function setupImagePreview(body, url) {
  body.innerHTML = `<div class="imagePreview"><div class="previewTools"><button data-zoom="out" aria-label="缩小">−</button><span id="imageZoomValue">100%</span><button data-zoom="in" aria-label="放大">＋</button><button data-zoom="actual">1:1</button><button data-zoom="fit">适应窗口</button></div><div class="imageCanvas"><div class="imageSurface"><img src="${url}" alt="图片预览" draggable="false"></div></div></div>`;
  const canvas = body.querySelector(".imageCanvas"),
    surface = body.querySelector(".imageSurface"),
    image = body.querySelector("img"),
    value = body.querySelector("#imageZoomValue");
  let scale = 1,
    pinchDistance = 0,
    pinchScale = 1;
  const clamp = (next) => Math.max(0.1, Math.min(8, next));
  const apply = (next) => {
    scale = clamp(next);
    const width = Math.max(1, image.naturalWidth * scale),
      height = Math.max(1, image.naturalHeight * scale);
    image.style.width = width + "px";
    image.style.height = height + "px";
    surface.style.width = Math.max(canvas.clientWidth, width) + "px";
    surface.style.height = Math.max(canvas.clientHeight, height) + "px";
    value.textContent = Math.round(scale * 100) + "%";
  };
  const fit = () => {
    if (!image.naturalWidth || !image.naturalHeight) return;
    apply(
      Math.min(
        1,
        Math.max(0.1, (canvas.clientWidth - 32) / image.naturalWidth),
        Math.max(0.1, (canvas.clientHeight - 32) / image.naturalHeight),
      ),
    );
    canvas.scrollTo(0, 0);
  };
  image.onload = fit;
  if (image.complete) fit();
  body.querySelector('[data-zoom="out"]').onclick = () => apply(scale / 1.2);
  body.querySelector('[data-zoom="in"]').onclick = () => apply(scale * 1.2);
  body.querySelector('[data-zoom="actual"]').onclick = () => apply(1);
  body.querySelector('[data-zoom="fit"]').onclick = fit;
  const onWheel = (event) => {
    event.preventDefault();
    apply(scale * (event.deltaY < 0 ? 1.12 : 1 / 1.12));
  };
  const touchDistance = (touches) =>
    Math.hypot(
      touches[0].clientX - touches[1].clientX,
      touches[0].clientY - touches[1].clientY,
    );
  const onTouchStart = (event) => {
    if (event.touches.length !== 2) return;
    pinchDistance = touchDistance(event.touches);
    pinchScale = scale;
  };
  const onTouchMove = (event) => {
    if (event.touches.length !== 2 || !pinchDistance) return;
    event.preventDefault();
    apply(pinchScale * (touchDistance(event.touches) / pinchDistance));
  };
  const onResize = () => {
    if (scale < 1) fit();
  };
  canvas.addEventListener("wheel", onWheel, { passive: false });
  canvas.addEventListener("touchstart", onTouchStart, { passive: true });
  canvas.addEventListener("touchmove", onTouchMove, { passive: false });
  window.addEventListener("resize", onResize);
  activePreviewCleanup = () => {
    canvas.removeEventListener("wheel", onWheel);
    canvas.removeEventListener("touchstart", onTouchStart);
    canvas.removeEventListener("touchmove", onTouchMove);
    window.removeEventListener("resize", onResize);
  };
}

function setPreviewURL(url) {
  if (activePreviewURL) URL.revokeObjectURL(activePreviewURL);
  activePreviewURL = url;
}

function clearPreviewResources() {
  previewAbort?.abort();
  previewAbort = null;
  if (activePreviewCleanup) activePreviewCleanup();
  activePreviewCleanup = null;
  setPreviewURL("");
}

function hidePreview() {
  clearPreviewResources();
  $("#previewBody").innerHTML = "";
  $("#preview").classList.add("hidden");
}
$("#closePreview").onclick = () => {
  const hasHistoryEntry = history.state?.lumePreview;
  hidePreview();
  if (hasHistoryEntry) history.back();
};
async function download(p) {
  fileDownloadAbort?.abort();
  const controller = new AbortController(),
    filename = p.split(/[\\/]/).pop() || "下载文件";
  fileDownloadAbort = controller;
  showFileDownloadDialog(filename);
  const startedAt = performance.now();
  try {
    const response = await fetch("/api/download?path=" + encodeURIComponent(p), {
      headers: { Authorization: "Bearer " + token },
      signal: controller.signal,
    });
    if (!response.ok) throw Error((await response.text()) || "下载失败");
    const total = Number(response.headers.get("content-length")) || 0,
      contentType =
        response.headers.get("content-type") || "application/octet-stream",
      chunks = [];
    let transferred = 0,
      lastPaint = 0;
    if (response.body?.getReader) {
      const reader = response.body.getReader();
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        chunks.push(value);
        transferred += value.byteLength;
        const now = performance.now();
        if (now - lastPaint > 120) {
          updateFileDownloadProgress(transferred, total, now - startedAt);
          lastPaint = now;
        }
      }
    } else {
      const blob = await response.blob();
      chunks.push(blob);
      transferred = blob.size;
    }
    updateFileDownloadProgress(transferred, total || transferred, performance.now() - startedAt);
    const objectURL = URL.createObjectURL(new Blob(chunks, { type: contentType })),
      anchor = document.createElement("a");
    anchor.href = objectURL;
    anchor.download = filename;
    document.body.append(anchor);
    anchor.click();
    anchor.remove();
    setTimeout(() => URL.revokeObjectURL(objectURL), 60000);
    showFileDownloadResult("下载完成", "文件已交给浏览器保存", "complete");
    toast("文件下载完成");
  } catch (error) {
    if (fileDownloadAbort !== controller) return;
    if (error.name === "AbortError") {
      showFileDownloadResult("下载已取消", "已停止接收文件", "cancelled");
    } else {
      showFileDownloadResult("下载失败", error.message || "请稍后重试", "error");
      toast("下载失败");
    }
  } finally {
    if (fileDownloadAbort === controller) fileDownloadAbort = null;
  }
}
function showFileDownloadDialog(filename) {
  $("#downloadTitle").textContent = filename;
  $("#downloadState").textContent = "准备下载…";
  $("#downloadMessage").textContent = "正在连接文件";
  $("#downloadTransferred").textContent = "0 B";
  $("#downloadTotal").textContent = "大小未知";
  $("#downloadSpeed").textContent = "0 B/s";
  $("#downloadBar").style.width = "0%";
  $("#downloadBar").classList.remove("failed");
  $("#downloadBar").classList.add("indeterminate");
  $("#downloadCancel").classList.remove("hidden");
  $("#downloadDone").classList.add("hidden");
  $("#downloadDialog").classList.remove("hidden");
  if (!history.state?.lumeDownload)
    history.pushState(navigationState({ lumeDownload: true }), "");
}
function updateFileDownloadProgress(transferred, total, elapsedMilliseconds) {
  const percent = total
    ? Math.max(0, Math.min(100, Math.round((transferred / total) * 100)))
    : 0;
  $("#downloadState").textContent = total ? `正在下载 ${percent}%` : "正在下载";
  $("#downloadMessage").textContent = "正在安全传输文件，请保持页面打开";
  $("#downloadTransferred").textContent = `已下载 ${size(transferred)}`;
  $("#downloadTotal").textContent = total ? `共 ${size(total)}` : "大小未知";
  $("#downloadSpeed").textContent = `${size(
    Math.round(
      elapsedMilliseconds > 0 ? (transferred * 1000) / elapsedMilliseconds : 0,
    ),
  )}/s`;
  $("#downloadBar").classList.toggle("indeterminate", !total);
  $("#downloadBar").style.width = percent + "%";
}
function showFileDownloadResult(title, message, result) {
  $("#downloadState").textContent = title;
  $("#downloadMessage").textContent = message;
  $("#downloadBar").classList.remove("indeterminate");
  $("#downloadBar").classList.toggle("failed", result === "error");
  if (result === "complete") $("#downloadBar").style.width = "100%";
  $("#downloadCancel").classList.add("hidden");
  $("#downloadDone").classList.remove("hidden");
}
function hideFileDownloadDialog() {
  $("#downloadDialog").classList.add("hidden");
}
function closeFileDownloadDialog() {
  const hasHistoryEntry = history.state?.lumeDownload;
  hideFileDownloadDialog();
  if (hasHistoryEntry) history.back();
}
$("#downloadClose").onclick = closeFileDownloadDialog;
$("#downloadDone").onclick = closeFileDownloadDialog;
$("#downloadCancel").onclick = () => fileDownloadAbort?.abort();
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
function updateAccountUI() {
  const name = currentUsername || "已登录用户";
  $("#accountName").textContent = name;
  $("#accountInitial").textContent =
    name === "已登录用户" ? "我" : name.slice(0, 1).toUpperCase();
}
$("#accountBtn").onclick = (event) => {
  event.stopPropagation();
  const accountMenu = $("#accountMenu"),
    opening = accountMenu.classList.contains("hidden");
  accountMenu.classList.toggle("hidden", !opening);
  $("#accountBtn").setAttribute("aria-expanded", String(opening));
};
$("#accountMenu").onclick = (event) => event.stopPropagation();
$("#logoutBtn").onclick = async () => {
  const button = $("#logoutBtn");
  button.disabled = true;
  button.textContent = "正在退出…";
  try {
    await api("/api/logout", { method: "POST" });
  } catch {}
  sessionStorage.removeItem("token");
  sessionStorage.removeItem("lumedavUser");
  token = "";
  currentUsername = "";
  location.href = "/";
};
$("#davBtn").onclick = () => {
  navigator.clipboard.writeText(location.origin + "/dav/");
  toast("WebDAV 地址已复制");
};
