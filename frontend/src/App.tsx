import { useEffect, useRef, useState } from "react";
import {
  CheckPort,
  ClearArchiveCache,
  ClearOfficePreviewCache,
  CreateInvite,
  CreateTemporaryAccess,
  CreateUser,
  DeleteUser,
  EmptyTrash,
  GetDashboard,
  GetArchiveDrives,
  GetSettings,
  GetStatus,
  GetVersion,
  InspectArchiveCache,
  OfficePreviewStatus,
  OpenArchiveCacheFolder,
  OpenLibreOfficeDownload,
  ListTemporaryAccess,
  ListTrash,
  ListInvites,
  ListUsers,
  RestoreTrash,
  RevokeTemporaryAccess,
  RevokeInvite,
  SaveSettings,
  SelectArchiveCacheFolder,
  SelectFolder,
  SelectTemporaryFolder,
  Start,
  SetUserEnabled,
  Stop,
} from "../wailsjs/go/main/App";
import "./App.css";
import "./admin.css";
import "./engine.css";
type Settings = {
  folder: string;
  folders: string[];
  port: number;
  listen: string;
  username: string;
  password: string;
  passwordSet: boolean;
  readOnly: boolean;
  autoStart: boolean;
  archiveCacheDir: string;
};
type ArchiveDriveInfo = { root: string; total: number; free: number };
type ArchiveCacheInfo = {
  path: string;
  total: number;
  free: number;
  cacheBytes: number;
  cacheFiles: number;
  available: boolean;
  error?: string;
};
type OperationNotice = {
  state: "busy" | "success" | "error";
  title: string;
  detail: string;
};
type Status = {
  running: boolean;
  urls: string[];
  davUrl: string;
  error: string;
};
const empty: Settings = {
  folder: "",
  folders: [],
  port: 8088,
  listen: "127.0.0.1",
  username: "admin",
  password: "",
  passwordSet: false,
  readOnly: false,
  autoStart: false,
  archiveCacheDir: "",
};
export default function App() {
  const [cfg, setCfg] = useState(empty),
    [status, setStatus] = useState<Status>({
      running: false,
      urls: [],
      davUrl: "",
      error: "",
    }),
    [page, setPage] = useState("dashboard"),
    [busy, setBusy] = useState(false),
    [serviceBusy, setServiceBusy] = useState(false),
    [msg, setMsg] = useState(""),
    [operation, setOperation] = useState<OperationNotice | null>(null),
    [portText, setPortText] = useState("8088"),
    [dash, setDash] = useState<any>({}),
    [trash, setTrash] = useState<any[]>([]),
    [temps, setTemps] = useState<any[]>([]),
    [users, setUsers] = useState<any[]>([]),
    [invites, setInvites] = useState<any[]>([]),
    [newUser, setNewUser] = useState({
      username: "",
      password: "",
      readOnly: true,
    }),
    [inviteHours, setInviteHours] = useState(24),
    [version, setVersion] = useState(""),
    [officeStatus, setOfficeStatus] = useState("missing"),
    [archiveDrives, setArchiveDrives] = useState<ArchiveDriveInfo[]>([]),
    [archiveInfo, setArchiveInfo] = useState<ArchiveCacheInfo | null>(null),
    [savedArchivePath, setSavedArchivePath] = useState(""),
    [cacheBusy, setCacheBusy] = useState(false),
    [temp, setTemp] = useState({
      folder: "",
      username: "",
      password: "",
      hours: 24,
      readOnly: true,
    }),
    operationTimer = useRef<number | undefined>(undefined);
  function announce(
    state: OperationNotice["state"],
    title: string,
    detail: string,
    autoHide = state === "busy" ? 0 : 3200,
  ) {
    if (operationTimer.current) window.clearTimeout(operationTimer.current);
    setOperation({ state, title, detail });
    if (autoHide)
      operationTimer.current = window.setTimeout(
        () => setOperation(null),
        autoHide,
      );
  }
  const refresh = async () => {
    setStatus((await GetStatus()) as Status);
    setDash(await GetDashboard());
    setTrash(await ListTrash());
    setTemps(await ListTemporaryAccess());
    setUsers((await ListUsers()) || []);
    setInvites((await ListInvites()) || []);
    setOfficeStatus(await OfficePreviewStatus());
  };
  useEffect(() => {
    GetVersion().then(setVersion);
    GetArchiveDrives().then((items) => setArchiveDrives(items || []));
    GetSettings().then((x) => {
      const v = x as Settings;
      v.folders = v.folders || (v.folder && [v.folder]) || [];
      setCfg(v);
      setPortText(String(v.port));
      setSavedArchivePath(v.archiveCacheDir);
      InspectArchiveCache(v.archiveCacheDir).then((info) =>
        setArchiveInfo(info as ArchiveCacheInfo),
      );
    });
    refresh();
    const t = setInterval(() => GetDashboard().then(setDash), 4000);
    return () => {
      clearInterval(t);
      if (operationTimer.current) window.clearTimeout(operationTimer.current);
    };
  }, []);
  const set = (k: keyof Settings, v: any) => setCfg({ ...cfg, [k]: v });
  async function save() {
    setBusy(true);
    announce("busy", "正在保存设置", "正在验证共享目录、端口和压缩磁盘");
    try {
      const port = Number(portText);
      if (!Number.isInteger(port) || port < 1 || port > 65535)
        throw Error("端口必须是 1–65535 的整数");
      const x = await SaveSettings({ ...cfg, port } as any);
      const next = x as Settings;
      setCfg(next);
      setSavedArchivePath(next.archiveCacheDir);
      setArchiveInfo(
        (await InspectArchiveCache(next.archiveCacheDir)) as ArchiveCacheInfo,
      );
      setMsg("设置已保存");
      announce("success", "设置已保存", "新的压缩与服务设置已经生效");
      return true;
    } catch (e: any) {
      setMsg(String(e));
      announce("error", "保存失败", String(e), 5200);
      return false;
    } finally {
      setBusy(false);
    }
  }
  async function toggle() {
    if (serviceBusy) return;
    setServiceBusy(true);
    try {
      if (status.running) {
        announce("busy", "正在停止服务", "正在结束连接并释放监听端口");
        const next = (await Stop()) as Status;
        setStatus(next);
        if (next.error) throw Error(next.error);
        announce("success", "服务已停止", "端口已经释放，可安全修改配置");
      } else if (await save()) {
        announce("busy", "正在启动服务", `正在监听 localhost:${Number(portText)}`);
        const next = (await Start()) as Status;
        setStatus(next);
        if (next.error) throw Error(next.error);
        announce("success", "服务已启动", `现在可以访问 localhost:${Number(portText)}`);
      }
    } catch (e: any) {
      setMsg(String(e));
      announce("error", "服务操作失败", String(e), 5200);
    } finally {
      setServiceBusy(false);
      await refresh();
    }
  }
  async function inspectCache(path: string, notify = false) {
    setCacheBusy(true);
    if (notify)
      announce("busy", "正在检查压缩磁盘", "正在读取磁盘容量与临时文件占用");
    try {
      const info = (await InspectArchiveCache(path)) as ArchiveCacheInfo;
      setArchiveInfo(info);
      if (notify)
        announce(
          info.available ? "success" : "error",
          info.available ? "磁盘检查完成" : "压缩磁盘不可用",
          info.available
            ? `${storageSize(info.free)} 可用，当前缓存 ${storageSize(info.cacheBytes)}`
            : info.error || "请选择其他本地磁盘",
          info.available ? 2600 : 5200,
        );
    } catch (e: any) {
      if (notify) announce("error", "磁盘检查失败", String(e), 5200);
    } finally {
      setCacheBusy(false);
    }
  }
  async function chooseArchiveCache() {
    const selected = await SelectArchiveCacheFolder();
    if (!selected) return;
    set("archiveCacheDir", selected);
    await inspectCache(selected, true);
    setMsg("已选择新位置，保存设置后生效");
  }
  async function useArchiveDrive(root: string) {
    const selected = `${root}LumeDAVCache\\downloads`;
    set("archiveCacheDir", selected);
    await inspectCache(selected, true);
    setMsg("已选择新磁盘，保存设置后生效");
  }
  const cacheChanged =
    cfg.archiveCacheDir.toLowerCase() !== savedArchivePath.toLowerCase();
  const nav = [
    ["dashboard", "◈", "仪表盘"],
    ["settings", "⚙", "服务设置"],
    ["cache", "▣", "压缩与缓存"],
    ["security", "◇", "安全与临时访问"],
    ["admin", "♙", "用户与邀请"],
    ["office", "▤", "Office 预览"],
    ["trash", "♲", "回收站"],
  ];
  return (
    <main>
      <aside>
        <div className="brand">
          <img className="mark" src="/lumedav-logo.png" />
          <div>
            <div className="brandName">
              <b>LumeDAV</b>
              {version && <em>v{version}</em>}
            </div>
            <span>WebDAV 管理中心</span>
          </div>
        </div>
        <nav>
          {nav.map((n) => (
            <button
              key={n[0]}
              className={page === n[0] ? "active" : ""}
              onClick={() => setPage(n[0])}
            >
              {n[1]}　{n[2]}
            </button>
          ))}
        </nav>
        <div className="aside-note">
          <i />
          <div>
            <b>{status.running ? "服务正在运行" : "服务已停止"}</b>
            <span>关闭窗口后继续在托盘运行</span>
          </div>
        </div>
      </aside>
      <section className="content">
        <header>
          <div>
            <p>LUMEDAV ADMIN</p>
            <h1>
              {
                {
                  dashboard: "运行仪表盘",
                  settings: "服务设置",
                  cache: "压缩与缓存",
                  security: "安全与临时访问",
                  admin: "用户与邀请",
                  office: "Office 预览",
                  trash: "回收站",
                }[page]
              }
            </h1>
            <span>所有服务管理都集中在这里，Web 页面只负责访问文件。</span>
          </div>
          <div className={"state " + (status.running ? "on" : "")}>
            <i />
            {status.running ? "正在运行" : "已停止"}
          </div>
        </header>
        {operation && (
          <div
            className={`adminOperation ${operation.state}`}
            role="status"
            aria-live="polite"
          >
            <i />
            <div>
              <b>{operation.title}</b>
              <span>{operation.detail}</span>
            </div>
            {operation.state !== "busy" && (
              <button
                aria-label="关闭提示"
                onClick={() => setOperation(null)}
              >
                ×
              </button>
            )}
          </div>
        )}
        {page === "dashboard" && (
          <>
            <div className="hero">
              <div className="orb" />
              <div>
                <small>服务状态</small>
                <h2>
                  {status.running
                    ? `localhost:${cfg.port}`
                    : "WebDAV 服务尚未启动"}
                </h2>
                <p>
                  {status.running
                    ? "关闭窗口不会停止服务"
                    : "完成设置后即可启动"}
                </p>
              </div>
              <button
                className={status.running ? "danger" : "primary"}
                disabled={serviceBusy}
                onClick={toggle}
              >
                {serviceBusy && <i className="buttonSpinner" />}
                {serviceBusy
                  ? status.running
                    ? "正在停止…"
                    : "正在启动…"
                  : status.running
                    ? "停止服务"
                    : "启动服务"}
                {!serviceBusy && "　→"}
              </button>
            </div>
            <div className="adminStats">
              {[
                ["在线会话", dash.online || 0],
                ["运行时间", duration(dash.uptime)],
                ["请求次数", dash.requests || 0],
                [
                  "传输流量",
                  size((dash.uploaded || 0) + (dash.downloaded || 0)),
                ],
                ["共享目录", dash.folders || cfg.folders.length],
                ["回收站", dash.trash || 0],
                ["封禁来源", dash.blocked || 0],
              ].map((x) => (
                <article>
                  <small>{x[0]}</small>
                  <b>{x[1]}</b>
                </article>
              ))}
            </div>
            {status.urls?.length > 0 && (
              <div className="addressCard">
                <b>访问地址</b>
                {status.urls.map((u) => (
                  <button onClick={() => navigator.clipboard.writeText(u)}>
                    {u}　复制
                  </button>
                ))}
              </div>
            )}
          </>
        )}
        {page === "settings" && (
          <div className="grid">
            <article>
              <Title n="01" h="共享文件夹" p="可连续添加多个独立目录" />
              <button
                className="folderAdd"
                onClick={async () => {
                  const p = await SelectFolder();
                  if (p && !cfg.folders.includes(p))
                    set("folders", [...cfg.folders, p]);
                }}
              >
                ＋ 添加文件夹
              </button>
              <div className="folderList">
                {cfg.folders.map((f, i) => (
                  <div>
                    <span>▱</span>
                    <b>{f}</b>
                    <button
                      onClick={() =>
                        set(
                          "folders",
                          cfg.folders.filter((_, n) => n !== i),
                        )
                      }
                    >
                      移除
                    </button>
                  </div>
                ))}
              </div>
            </article>
            <article>
              <Title n="02" h="网络与端口" p="输入后自动检测是否可用" />
              <div className="row">
                <label>
                  端口
                  <input
                    value={portText}
                    onChange={(e) =>
                      setPortText(e.target.value.replace(/\D/g, ""))
                    }
                  />
                </label>
                <label>
                  访问范围
                  <select
                    value={cfg.listen}
                    onChange={(e) => set("listen", e.target.value)}
                  >
                    <option value="127.0.0.1">仅本机 / FRP</option>
                    <option value="0.0.0.0">局域网与本机</option>
                  </select>
                </label>
              </div>
              <button
                className="checkPort"
                onClick={async () =>
                  setMsg(await CheckPort(Number(portText), cfg.listen))
                }
              >
                检测端口占用
              </button>
            </article>
            <article>
              <Title n="03" h="账号凭据" p="Web 与 WebDAV 共用" />
              <div className="row">
                <label>
                  用户名
                  <input
                    value={cfg.username}
                    onChange={(e) => set("username", e.target.value)}
                  />
                </label>
                <label>
                  密码
                  <input
                    type="password"
                    value={cfg.password}
                    placeholder={
                      cfg.passwordSet ? "留空保持原密码" : "设置密码"
                    }
                    onChange={(e) => set("password", e.target.value)}
                  />
                </label>
              </div>
            </article>
            <article>
              <Title n="04" h="运行偏好" p="后台运行与访问权限" />
              <Switch
                t="开机自动启动"
                d="登录 Windows 后自动启动"
                v={cfg.autoStart}
                f={(v) => set("autoStart", v)}
              />
              <Switch
                t="主账号只读"
                d="禁止修改和删除文件"
                v={cfg.readOnly}
                f={(v) => set("readOnly", v)}
              />
            </article>
          </div>
        )}
        {page === "cache" && (
          <div className="cacheAdmin">
            <article className="cacheOverview">
              <Title
                n="ZIP"
                h="文件夹压缩缓存"
                p="大文件夹会先在所选磁盘生成临时 ZIP；小于 2 GB 的目录仍然直接流式传输"
              />
              <div
                className={
                  "cacheHealth " +
                  (archiveInfo?.available ? "ready" : "unavailable")
                }
              >
                <i />
                <div>
                  <b>
                    {archiveInfo?.available
                      ? "压缩磁盘可用"
                      : "压缩磁盘不可用"}
                  </b>
                  <span>
                    {archiveInfo?.available
                      ? `${storageSize(archiveInfo.free)} 可用，共 ${storageSize(archiveInfo.total)}`
                      : archiveInfo?.error || "正在检查磁盘状态…"}
                  </span>
                </div>
                {cacheBusy && <span className="miniSpinner">检查中</span>}
              </div>
              <label className="cachePathLabel">
                当前压缩目录
                <div className="cachePathRow">
                  <input
                    value={cfg.archiveCacheDir}
                    onChange={(e) => set("archiveCacheDir", e.target.value)}
                    onBlur={() => inspectCache(cfg.archiveCacheDir)}
                  />
                  <button onClick={chooseArchiveCache}>选择磁盘或目录</button>
                </div>
              </label>
              {cacheChanged && (
                <div className="cachePending">
                  新位置尚未应用；点击页面底部“保存所有设置”后，新任务才会使用它。
                </div>
              )}
              <div className="driveGrid">
                {archiveDrives.map((drive) => {
                  const selected = cfg.archiveCacheDir
                    .toLowerCase()
                    .startsWith(drive.root.toLowerCase());
                  return (
                    <button
                      key={drive.root}
                      className={selected ? "selected" : ""}
                      onClick={() => useArchiveDrive(drive.root)}
                    >
                      <span>{drive.root.slice(0, 1)}</span>
                      <div>
                        <b>{drive.root} 本地磁盘</b>
                        <small>
                          可用 {storageSize(drive.free)} / {storageSize(drive.total)}
                        </small>
                        <i>
                          <em
                            style={{
                              width: `${drive.total ? Math.max(4, (drive.free / drive.total) * 100) : 0}%`,
                            }}
                          />
                        </i>
                      </div>
                    </button>
                  );
                })}
              </div>
            </article>
            <div className="cacheSide">
              <article>
                <Title n="DATA" h="缓存占用" p="只统计临时压缩目录" />
                <div className="cacheNumbers">
                  <div>
                    <b>{storageSize(archiveInfo?.cacheBytes || 0)}</b>
                    <span>当前占用</span>
                  </div>
                  <div>
                    <b>{archiveInfo?.cacheFiles || 0}</b>
                    <span>临时文件</span>
                  </div>
                </div>
                <div className="cacheActions">
                  <button
                    disabled={cacheChanged || cacheBusy}
                    onClick={async () => {
                      try {
                        await OpenArchiveCacheFolder();
                        announce(
                          "success",
                          "已打开压缩目录",
                          archiveInfo?.path || cfg.archiveCacheDir,
                          2200,
                        );
                      } catch (e: any) {
                        announce("error", "无法打开目录", String(e), 5200);
                      }
                    }}
                  >
                    打开目录
                  </button>
                  <button
                    disabled={cacheChanged || cacheBusy}
                    onClick={async () => {
                      if (!confirm("确定清理已完成的临时压缩包？")) return;
                      setCacheBusy(true);
                      try {
                        announce(
                          "busy",
                          "正在清理压缩缓存",
                          "只会删除已经完成的 ZIP 临时文件",
                        );
                        setArchiveInfo(
                          (await ClearArchiveCache()) as ArchiveCacheInfo,
                        );
                        setMsg("压缩缓存已清理");
                        announce(
                          "success",
                          "压缩缓存已清理",
                          "未触碰共享文件夹和其他类型文件",
                        );
                      } catch (e: any) {
                        setMsg(String(e));
                        announce("error", "缓存清理失败", String(e), 5200);
                      } finally {
                        setCacheBusy(false);
                      }
                    }}
                  >
                    清理缓存
                  </button>
                  <button
                    disabled={cacheBusy}
                    onClick={() => inspectCache(cfg.archiveCacheDir, true)}
                  >
                    重新检查
                  </button>
                </div>
              </article>
              <article className="cacheRules">
                <Title n="SAFE" h="空间保护" p="打包前自动检查" />
                <p>大文件夹开始压缩前，会预留原始容量加至少 512 MB 安全空间。</p>
                <p>缓存目录不能与任何 WebDAV 共享文件夹重叠。</p>
                <p>磁盘拔出或空间不足时会停止任务，不会自动改用 C 盘。</p>
              </article>
            </div>
          </div>
        )}
        {page === "security" && (
          <>
            <div className="securityCards">
              <article>
                <b>5 次</b>
                <span>连续失败后触发封禁</span>
              </article>
              <article>
                <b>15 分钟</b>
                <span>来源 IP 封禁时间</span>
              </article>
              <article>
                <b>12 小时</b>
                <span>登录会话有效期</span>
              </article>
              <article>
                <b>{dash.blocked || 0}</b>
                <span>当前封禁来源</span>
              </article>
            </div>
            <article className="temporary">
              <Title
                n="＋"
                h="创建临时访问"
                p="为指定目录创建自动过期的独立账号"
              />
              <div className="tempForm">
                <button
                  onClick={async () => {
                    const p = await SelectTemporaryFolder();
                    if (p) setTemp({ ...temp, folder: p });
                  }}
                >
                  选择目录
                </button>
                <input value={temp.folder} readOnly placeholder="临时目录" />
                <input
                  value={temp.username}
                  onChange={(e) =>
                    setTemp({ ...temp, username: e.target.value })
                  }
                  placeholder="账号"
                />
                <input
                  type="password"
                  value={temp.password}
                  onChange={(e) =>
                    setTemp({ ...temp, password: e.target.value })
                  }
                  placeholder="密码"
                />
                <input
                  type="number"
                  value={temp.hours}
                  onChange={(e) => setTemp({ ...temp, hours: +e.target.value })}
                />
                <label className="mini">
                  <input
                    type="checkbox"
                    checked={temp.readOnly}
                    onChange={(e) =>
                      setTemp({ ...temp, readOnly: e.target.checked })
                    }
                  />
                  只读
                </label>
                <button
                  className="add"
                  onClick={async () => {
                    try {
                      await CreateTemporaryAccess(
                        temp.folder,
                        temp.username,
                        temp.password,
                        temp.hours,
                        temp.readOnly,
                      );
                      setTemps(await ListTemporaryAccess());
                      setMsg("临时访问已创建");
                    } catch (e: any) {
                      setMsg(String(e));
                    }
                  }}
                >
                  创建
                </button>
              </div>
              <div className="tempList">
                {temps.map((t) => (
                  <div>
                    <span>
                      <b>{t.username}</b>
                      <small>
                        {t.folder} ·{" "}
                        {new Date(t.expiresAt * 1000).toLocaleString()} 到期
                      </small>
                    </span>
                    <button
                      onClick={async () => {
                        await RevokeTemporaryAccess(t.id);
                        setTemps(await ListTemporaryAccess());
                      }}
                    >
                      撤销
                    </button>
                  </div>
                ))}
              </div>
            </article>
          </>
        )}
        {page === "admin" && (
          <>
            <div className="securityCards">
              <article>
                <b>{users.length}</b>
                <span>已注册普通用户</span>
              </article>
              <article>
                <b>
                  {
                    invites.filter(
                      (i) => !i.used && i.expiresAt > Date.now() / 1000,
                    ).length
                  }
                </b>
                <span>有效注册邀请</span>
              </article>
              <article>
                <b>{temps.length}</b>
                <span>临时目录访问</span>
              </article>
              <article>
                <b>1</b>
                <span>主管理员</span>
              </article>
            </div>
            <div className="adminColumns">
              <article>
                <Title n="U" h="普通用户" p="创建、停用和删除多个访问账号" />
                <div className="userCreate">
                  <input
                    placeholder="用户名"
                    value={newUser.username}
                    onChange={(e) =>
                      setNewUser({ ...newUser, username: e.target.value })
                    }
                  />
                  <input
                    type="password"
                    placeholder="初始密码"
                    value={newUser.password}
                    onChange={(e) =>
                      setNewUser({ ...newUser, password: e.target.value })
                    }
                  />
                  <label className="mini">
                    <input
                      type="checkbox"
                      checked={newUser.readOnly}
                      onChange={(e) =>
                        setNewUser({ ...newUser, readOnly: e.target.checked })
                      }
                    />
                    只读
                  </label>
                  <button
                    onClick={async () => {
                      try {
                        await CreateUser(
                          newUser.username,
                          newUser.password,
                          newUser.readOnly,
                        );
                        setUsers(await ListUsers());
                        setNewUser({ ...newUser, password: "" });
                        setMsg("用户已创建");
                      } catch (e: any) {
                        setMsg(String(e));
                      }
                    }}
                  >
                    添加用户
                  </button>
                </div>
                <div className="userList">
                  {users.map((u) => (
                    <div>
                      <span>
                        <b>{u.username}</b>
                        <small>
                          {u.readOnly ? "只读" : "可读写"} ·{" "}
                          {u.enabled ? "已启用" : "已停用"}
                        </small>
                      </span>
                      <button
                        onClick={async () => {
                          await SetUserEnabled(u.id, !u.enabled);
                          setUsers(await ListUsers());
                        }}
                      >
                        {u.enabled ? "停用" : "启用"}
                      </button>
                      <button
                        className="remove"
                        onClick={async () => {
                          await DeleteUser(u.id);
                          setUsers(await ListUsers());
                        }}
                      >
                        删除
                      </button>
                    </div>
                  ))}
                </div>
              </article>
              <article>
                <Title
                  n="I"
                  h="邀请注册"
                  p="生成一次性邀请链接，由访客自行设置账号密码"
                />
                <div className="inviteCreate">
                  <label>
                    有效小时
                    <input
                      type="number"
                      value={inviteHours}
                      onChange={(e) => setInviteHours(+e.target.value)}
                    />
                  </label>
                  <button
                    onClick={async () => {
                      try {
                        await CreateInvite(inviteHours, true);
                        setInvites((await ListInvites()) || []);
                        setMsg("邀请已生成");
                      } catch (e: any) {
                        setMsg(String(e));
                      }
                    }}
                  >
                    生成只读邀请
                  </button>
                </div>
                <div className="inviteList">
                  {invites.map((i) => {
                    const url =
                      (status.urls?.[0] || `http://127.0.0.1:${cfg.port}`) +
                      `/?invite=${i.code}`;
                    return (
                      <div>
                        <span>
                          <b>
                            {i.used
                              ? "已使用"
                              : i.expiresAt < Date.now() / 1000
                                ? "已过期"
                                : "等待注册"}
                          </b>
                          <small>
                            {new Date(i.expiresAt * 1000).toLocaleString()} 到期
                          </small>
                        </span>
                        <button
                          onClick={() => {
                            navigator.clipboard.writeText(url);
                            setMsg("邀请链接已复制");
                          }}
                        >
                          复制链接
                        </button>
                        <button
                          className="remove"
                          onClick={async () => {
                            await RevokeInvite(i.code);
                            setInvites((await ListInvites()) || []);
                          }}
                        >
                          撤销
                        </button>
                      </div>
                    );
                  })}
                </div>
              </article>
            </div>
          </>
        )}
        {page === "office" && (
          <div className="cadAdmin">
            <article>
              <Title
                n="DOC"
                h="Office 文档预览引擎"
                p="Word、Excel、PowerPoint 在主机本地转换为 PDF，访问设备无需安装 Office"
              />
              <div className={"engineState " + officeStatus}>
                <i />
                <div>
                  <b>
                    {officeStatus === "ready"
                      ? "LibreOffice 已就绪"
                      : "尚未检测到 LibreOffice"}
                  </b>
                  <span>
                    {officeStatus === "ready"
                      ? "支持 DOC/DOCX、XLS/XLSX、PPT/PPTX、ODT/ODS/ODP"
                      : "LibreOffice 体积较大，需要管理员在 Windows 主机安装一次"}
                  </span>
                </div>
              </div>
              <div className="cadActions">
                <button
                  className="primary"
                  disabled={officeStatus === "ready"}
                  onClick={() => OpenLibreOfficeDownload()}
                >
                  {officeStatus === "ready" ? "已经安装" : "打开官方下载页"}
                </button>
                <button
                  onClick={async () => {
                    await ClearOfficePreviewCache();
                    setMsg("Office 预览缓存已清空");
                  }}
                >
                  清空预览缓存
                </button>
                <button
                  onClick={async () =>
                    setOfficeStatus(await OfficePreviewStatus())
                  }
                >
                  重新检测
                </button>
              </div>
              <div className="cadInfo">
                <div>
                  <b>Word</b>
                  <span>DOC、DOCX、ODT 转换为 PDF</span>
                </div>
                <div>
                  <b>Excel</b>
                  <span>XLS、XLSX、ODS，多工作表按 PDF 页面查看</span>
                </div>
                <div>
                  <b>PowerPoint</b>
                  <span>PPT、PPTX、ODP，按幻灯片页面查看</span>
                </div>
                <div>
                  <b>其他格式</b>
                  <span>图片、视频、音频、PDF、文本无需额外引擎</span>
                </div>
              </div>
            </article>
          </div>
        )}
        {page === "trash" && (
          <article className="trashAdmin">
            <div className="trashHead">
              <div>
                <h3>已删除项目</h3>
                <p>远程删除的文件会先保留在这里</p>
              </div>
              <button
                onClick={async () => {
                  if (confirm("确定清空回收站？")) {
                    await EmptyTrash();
                    setTrash(await ListTrash());
                  }
                }}
              >
                清空回收站
              </button>
            </div>
            {trash.length === 0 ? (
              <div className="emptyAdmin">回收站为空</div>
            ) : (
              trash.map((t) => (
                <div className="trashRow">
                  <span>◇</span>
                  <div>
                    <b>{t.Name}</b>
                    <small>
                      {t.Original} · {new Date(t.Deleted).toLocaleString()}
                    </small>
                  </div>
                  <button
                    onClick={async () => {
                      await RestoreTrash(t.ID);
                      setTrash(await ListTrash());
                      setMsg("文件已恢复");
                    }}
                  >
                    恢复
                  </button>
                </div>
              ))
            )}
          </article>
        )}
        <footer>
          <span className={msg ? "show" : ""}>{msg}</span>
          {(page === "settings" || page === "cache") && (
            <button
              className={busy ? "saving" : ""}
              disabled={busy}
              onClick={save}
            >
              {busy && <i className="buttonSpinner dark" />}
              {busy ? "正在保存…" : "保存所有设置"}
            </button>
          )}
        </footer>
      </section>
    </main>
  );
}
function Title(p: { n: string; h: string; p: string }) {
  return (
    <div className="title">
      <span>{p.n}</span>
      <div>
        <h3>{p.h}</h3>
        <p>{p.p}</p>
      </div>
    </div>
  );
}
function Switch(p: {
  t: string;
  d: string;
  v: boolean;
  f: (v: boolean) => void;
}) {
  return (
    <label className="switch">
      <div>
        <b>{p.t}</b>
        <span>{p.d}</span>
      </div>
      <input
        type="checkbox"
        checked={p.v}
        onChange={(e) => p.f(e.target.checked)}
      />
      <i />
    </label>
  );
}
function size(n: number) {
  return n < 1024
    ? n + " B"
    : n < 1048576
      ? (n / 1024).toFixed(1) + " KB"
      : (n / 1048576).toFixed(1) + " MB";
}
function storageSize(n: number) {
  return n < 1024
    ? `${n} B`
    : n < 1048576
      ? `${(n / 1024).toFixed(1)} KB`
      : n < 1073741824
        ? `${(n / 1048576).toFixed(1)} MB`
        : n < 1099511627776
          ? `${(n / 1073741824).toFixed(1)} GB`
          : `${(n / 1099511627776).toFixed(1)} TB`;
}
function duration(n: number) {
  return !n
    ? "—"
    : n < 3600
      ? Math.floor(n / 60) + " 分钟"
      : Math.floor(n / 3600) + " 小时";
}
