import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Archive,
  ArrowLeft,
  Check,
  ChevronLeft,
  ChevronRight,
  CircleHelp,
  Cloud,
  Download,
  File,
  FileArchive,
  FileImage,
  FileText,
  FileVideo,
  Folder,
  FolderOpen,
  FolderPlus,
  HardDrive,
  ImageOff,
  LayoutGrid,
  Link,
  LoaderCircle,
  LogOut,
  MoreHorizontal,
  MoveRight,
  Pencil,
  Plus,
  Search,
  Settings,
  ShieldCheck,
  Share2,
  Trash2,
  Upload,
  X
} from "lucide-react";
import {
  type ChangeEvent,
  type DragEvent,
  type FormEvent,
  type ReactNode,
  useEffect,
  useMemo,
  useRef,
  useState
} from "react";
import { Navigate, NavLink, Route, Routes, useLocation, useNavigate, useParams, useSearchParams } from "react-router-dom";
import { api, ApiError } from "./api/client";
import type { AccountUser, CreatedInvitation, Folder as FolderType, Invitation, PublicShareFile, Session, Share, StorageUsage, UserFile } from "./api/types";
import { clearSession, readSession, writeSession } from "./auth/session";
import {
  findUploadResumePoint,
  removeUploadResumePoint,
  saveUploadResumePoint,
  taskMatchesResumePoint
} from "./upload/resume";

type Toast = { message: string; tone: "success" | "error" | "info" } | null;
type SelectedItem =
  | { type: "file"; value: UserFile }
  | { type: "folder"; value: FolderType }
  | null;
type UploadItem = { id: string; name: string; progress: number; state: "uploading" | "finished" | "failed" };
type DirectoryEntry =
  | { type: "file"; value: UserFile }
  | { type: "folder"; value: FolderType };

export default function App() {
  const [session, setSession] = useState<Session | null>(() => readSession());

  useEffect(() => {
    const onUnauthorized = () => setSession(null);
    window.addEventListener("cloudbox:unauthorized", onUnauthorized);
    return () => window.removeEventListener("cloudbox:unauthorized", onUnauthorized);
  }, []);

  return (
    <Routes>
      <Route path="/share/:token" element={<PublicSharePage />} />
      <Route path="/login" element={session ? <Navigate to="/files" replace /> : <LoginPage onAuthenticated={setSession} />} />
      <Route path="*" element={session ? session.must_change_password ? <ChangePasswordPage session={session} onChanged={() => setSession(null)} /> : <Workspace session={session} onLogout={() => setSession(null)} /> : <Navigate to="/login" replace />} />
    </Routes>
  );
}

function LoginPage({ onAuthenticated }: { onAuthenticated: (session: Session) => void }) {
  const navigate = useNavigate();
  const [mode, setMode] = useState<"login" | "register">("login");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [inviteCode, setInviteCode] = useState("");
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError("");
    setSubmitting(true);
    try {
      if (mode === "register") {
        await api.register(username, password, inviteCode);
      }
      const { token, user } = await api.login(username, password);
      const next = { token, username: user.username, role: user.role, must_change_password: user.must_change_password };
      writeSession(next);
      onAuthenticated(next);
      const pendingDownload = sessionStorage.getItem("cloudbox.pending-share-download");
      const destination = new URLSearchParams(location.search).get("next");
      navigate(pendingDownload && destination?.startsWith("/share/") ? destination : "/files", { replace: true });
    } catch (err) {
      setError(messageOf(err));
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <main className="auth-shell">
      <section className="auth-intro" aria-label="CloudBox 介绍">
        <div className="brand-mark brand-mark-large"><Cloud size={28} strokeWidth={2.4} /></div>
        <p className="eyebrow">CLOUDBOX WORKSPACE</p>
        <h1>管理每一份<br />重要文件。</h1>
        <p className="auth-copy">上传、整理和共享都在一个安静、清晰的工作台中完成。</p>
        <div className="auth-features">
          <span><ShieldCheck size={16} /> 文件处理状态可见</span>
          <span><HardDrive size={16} /> 容量配额可见</span>
          <span><Share2 size={16} /> 可控外链分享</span>
        </div>
      </section>
      <section className="auth-form-area">
        <form className="auth-form" onSubmit={submit}>
          <div className="auth-heading">
            <div className="brand-inline"><span className="brand-mark"><Cloud size={18} /></span> CloudBox</div>
            <h2>{mode === "login" ? "欢迎回来" : "创建工作区账号"}</h2>
            <p>{mode === "login" ? "使用你的账号进入文件工作台。" : "使用管理员创建的邀请码注册账号。"}</p>
          </div>
          <label>
            用户名
            <input value={username} onChange={(event) => setUsername(event.target.value)} placeholder="输入用户名" autoComplete="username" required />
          </label>
          <label>
            密码
            <input value={password} onChange={(event) => setPassword(event.target.value)} placeholder="至少 6 位字符" type="password" autoComplete={mode === "login" ? "current-password" : "new-password"} required />
          </label>
          {mode === "register" && <label>
            邀请码
            <input value={inviteCode} onChange={(event) => setInviteCode(event.target.value)} placeholder="输入管理员提供的邀请码" autoComplete="off" required />
          </label>}
          {error && <p className="form-error">{error}</p>}
          <button className="primary-button auth-submit" type="submit" disabled={submitting}>
            {submitting && <LoaderCircle size={17} className="spin" />}
            {mode === "login" ? "登录" : "创建账号"}
          </button>
          <button className="text-button auth-switch" type="button" onClick={() => { setMode(mode === "login" ? "register" : "login"); setError(""); setInviteCode(""); }}>
            {mode === "login" ? "没有账号？注册一个" : "已有账号？返回登录"}
          </button>
        </form>
      </section>
    </main>
  );
}

function ChangePasswordPage({ session, onChanged }: { session: Session; onChanged: () => void }) {
  const [currentPassword, setCurrentPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError("");
    setSubmitting(true);
    try {
      await api.changePassword(currentPassword, newPassword);
      clearSession();
      onChanged();
    } catch (err) {
      setError(messageOf(err));
    } finally {
      setSubmitting(false);
    }
  }
  return <main className="auth-shell"><section className="auth-intro" aria-label="CloudBox 账号安全"><div className="brand-mark brand-mark-large"><Cloud size={28} strokeWidth={2.4} /></div><p className="eyebrow">ACCOUNT SECURITY</p><h1>设置新的<br />登录密码。</h1><p className="auth-copy">为了保护账号，请先修改管理员提供的临时密码。</p></section><section className="auth-form-area"><form className="auth-form" onSubmit={submit}><div className="auth-heading"><div className="brand-inline"><span className="brand-mark"><Cloud size={18} /></span> CloudBox</div><h2>修改密码</h2><p>账号：{session.username}</p></div><label>临时密码<input value={currentPassword} onChange={(event) => setCurrentPassword(event.target.value)} type="password" autoComplete="current-password" required /></label><label>新密码<input value={newPassword} onChange={(event) => setNewPassword(event.target.value)} type="password" autoComplete="new-password" placeholder="至少 6 位字符" required /></label>{error && <p className="form-error">{error}</p>}<button className="primary-button auth-submit" type="submit" disabled={submitting}>{submitting && <LoaderCircle size={17} className="spin" />}保存新密码并重新登录</button></form></section></main>;
}

function PublicSharePage() {
	const { token = "" } = useParams();
	const [password, setPassword] = useState("");
	const [error, setError] = useState("");
	const [file, setFile] = useState<PublicShareFile | null>(null);
	const [preview, setPreview] = useState<string | null>(null);
	const [loading, setLoading] = useState(false);
	const [downloading, setDownloading] = useState(false);
	const navigate = useNavigate();

	useEffect(() => () => { if (preview) URL.revokeObjectURL(preview); }, [preview]);
	useEffect(() => {
		const pending = sessionStorage.getItem("cloudbox.pending-share-download");
		if (!pending || !readSession()) return;
		try {
			const parsed = JSON.parse(pending) as { token?: string; password?: string };
			if (parsed.token !== token) return;
			sessionStorage.removeItem("cloudbox.pending-share-download");
			setPassword(parsed.password ?? "");
			void api.downloadShared(token, parsed.password ?? "").catch((err) => setError(messageOf(err)));
		} catch {
			sessionStorage.removeItem("cloudbox.pending-share-download");
		}
	}, [token]);

	async function unlock(event: FormEvent<HTMLFormElement>) {
		event.preventDefault();
		setError("");
		setLoading(true);
		try {
			const { file: sharedFile } = await api.publicShareInfo(token, password);
			setFile(sharedFile);
			if (sharedFile.has_preview && sharedFile.content_type.startsWith("image/")) {
				const blob = await api.publicSharePreview(token, password);
				setPreview(URL.createObjectURL(blob));
			}
		} catch (err) {
			setFile(null);
			setError(messageOf(err));
		} finally {
			setLoading(false);
		}
	}

	async function download(event: FormEvent<HTMLFormElement>) {
		event.preventDefault();
		const session = readSession();
		if (!session) {
			sessionStorage.setItem("cloudbox.pending-share-download", JSON.stringify({ token, password }));
			navigate(`/login?next=${encodeURIComponent(`/share/${token}`)}`, { replace: true });
			return;
		}
		setError("");
    setDownloading(true);
    try {
      // 密码只在本次下载请求中作为请求头发送，不会保存到浏览器存储。
      await api.downloadShared(token, password);
    } catch (err) {
      setError(messageOf(err));
    } finally {
      setDownloading(false);
    }
  }

  return (
    <main className="public-share-page">
      <header className="public-share-header">
        <div className="brand-inline"><span className="brand-mark"><Cloud size={18} /></span>CloudBox</div>
      </header>
      <section className="public-share-card" aria-label="共享文件下载">
        <div className="public-share-icon"><Share2 size={28} /></div>
        <p className="eyebrow">SHARED FILE</p>
        <h1>{file ? file.original_name : "有人向你分享了一个文件"}</h1>
        <p className="public-share-copy">验证访问权限后可查看图片预览。下载或保存文件需要登录。</p>
        {file?.has_preview && preview && <img className="public-share-preview" src={preview} alt={`${file.original_name} 预览`} />}
        {file && !file.has_preview && <p className="public-share-file-meta">{shortType(file.content_type)} · {formatBytes(file.size)}{file.content_type.startsWith("video/") && " · 视频暂不支持在线播放"}</p>}
        {!file && <form className="public-share-form" onSubmit={unlock}>
          <label>
            访问密码 <span className="optional">如有</span>
            <input value={password} onChange={(event) => setPassword(event.target.value)} type="password" placeholder="未设置密码则留空" autoComplete="current-password" />
          </label>
          {error && <p className="form-error">{error}</p>}
          <button className="primary-button public-share-download" type="submit" disabled={loading || !token}>
            {loading ? <LoaderCircle size={17} className="spin" /> : <ShieldCheck size={17} />}
            {loading ? "正在验证" : "查看分享内容"}
          </button>
        </form>}
        {file && <form className="public-share-form" onSubmit={download}>
          {error && <p className="form-error">{error}</p>}
          <button className="primary-button public-share-download" type="submit" disabled={downloading || !token}>
            {downloading ? <LoaderCircle size={17} className="spin" /> : <Download size={17} />}
            {downloading ? "正在准备下载" : "登录后下载文件"}
          </button>
        </form>}
        <p className="public-share-note"><ShieldCheck size={15} />图片预览不计入下载次数；实际下载或保存副本会受分享限制约束。</p>
      </section>
    </main>
  );
}

function Workspace({ session, onLogout }: { session: Session; onLogout: () => void }) {
  const location = useLocation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [searchParams, setSearchParams] = useSearchParams();
  const [selected, setSelected] = useState<SelectedItem>(null);
  const [search, setSearch] = useState("");
  const [searchKind, setSearchKind] = useState<"" | "image" | "video" | "other">("");
  const [searchPeriod, setSearchPeriod] = useState<"" | "7d" | "30d" | "year" | "older">("");
  const [toast, setToast] = useState<Toast>(null);
  const [dialog, setDialog] = useState<"upload" | "folder" | "rename" | "move" | "share" | "save-share" | null>(null);
  const [uploads, setUploads] = useState<UploadItem[]>([]);
  const [imageViewer, setImageViewer] = useState<UserFile | null>(null);
  const [dragging, setDragging] = useState(false);
  const [helpOpen, setHelpOpen] = useState(false);
  const [accountMenuOpen, setAccountMenuOpen] = useState(false);
  const accountMenuRef = useRef<HTMLDivElement>(null);
  const processingFilesRef = useRef(new Set<number>());

  const view = location.pathname === "/trash" ? "trash" : location.pathname === "/shares" ? "shares" : location.pathname === "/settings" ? "settings" : "files";
  const folderPath = readFolderPath(searchParams.get("path"), toID(searchParams.get("folder")));
  const folderNames = readFolderNames(searchParams.get("names"), folderPath.length);
  const parentID = folderPath.at(-1) ?? null;
  const filesQuery = useQuery({
    queryKey: ["files", parentID],
    queryFn: () => api.listFiles(parentID),
    enabled: view === "files"
  });
  const trashQuery = useQuery({ queryKey: ["trash"], queryFn: api.listTrash, enabled: view === "trash" });
  const foldersQuery = useQuery({
    queryKey: ["folders", parentID],
    queryFn: () => api.listFolders(parentID),
    enabled: view === "files"
  });
  const rootFoldersQuery = useQuery({ queryKey: ["folders", null], queryFn: () => api.listFolders(null) });
  const storageQuery = useQuery({ queryKey: ["storage"], queryFn: api.storage });
  const sharesQuery = useQuery({ queryKey: ["shares"], queryFn: api.listShares, enabled: view === "shares" });
  const searchRange = useMemo(() => {
    const now = new Date();
    const yearStart = new Date(now.getFullYear(), 0, 1);
    if (searchPeriod === "7d") return { since: new Date(now.getTime() - 7 * 24 * 60 * 60 * 1000).toISOString() };
    if (searchPeriod === "30d") return { since: new Date(now.getTime() - 30 * 24 * 60 * 60 * 1000).toISOString() };
    if (searchPeriod === "year") return { since: yearStart.toISOString() };
    if (searchPeriod === "older") return { before: yearStart.toISOString() };
    return {};
  }, [searchPeriod]);
  const isSearching = view === "files" && Boolean(search.trim() || searchKind || searchPeriod);
  const searchQuery = useQuery({
    queryKey: ["file-search", search.trim(), searchKind, searchPeriod],
    queryFn: () => api.searchFiles({ query: search.trim() || undefined, kind: searchKind || undefined, ...searchRange }),
    enabled: isSearching
  });

  useEffect(() => setSelected(null), [location.pathname, parentID]);
  useEffect(() => {
    if (!toast) return undefined;
    const timer = window.setTimeout(() => setToast(null), 4200);
    return () => window.clearTimeout(timer);
  }, [toast]);
  useEffect(() => {
    function closeTransientUI(event: KeyboardEvent) {
      if (event.key !== "Escape") return;
      setHelpOpen(false);
      setAccountMenuOpen(false);
    }

    window.addEventListener("keydown", closeTransientUI);
    return () => window.removeEventListener("keydown", closeTransientUI);
  }, []);
  useEffect(() => {
    if (!accountMenuOpen) return undefined;

    function closeAccountMenu(event: MouseEvent) {
      if (!accountMenuRef.current?.contains(event.target as Node)) {
        setAccountMenuOpen(false);
      }
    }

    window.addEventListener("mousedown", closeAccountMenu);
    return () => window.removeEventListener("mousedown", closeAccountMenu);
  }, [accountMenuOpen]);

  const files = view === "trash" ? trashQuery.data?.files ?? [] : isSearching ? searchQuery.data?.files ?? [] : filesQuery.data?.files ?? [];
  const folders = isSearching ? [] : foldersQuery.data?.folders ?? [];
  const filteredFiles = useMemo(() => {
    if (isSearching) return files;
    const normalized = search.trim().toLowerCase();
    return normalized ? files.filter((item) => item.original_name.toLowerCase().includes(normalized)) : files;
  }, [files, isSearching, search]);
  const filteredFolders = useMemo(() => {
    if (isSearching) return [];
    const normalized = search.trim().toLowerCase();
    return normalized ? folders.filter((item) => item.name.toLowerCase().includes(normalized)) : folders;
  }, [folders, isSearching, search]);
  const entries = useMemo<DirectoryEntry[]>(() => [
    ...filteredFolders.map((value) => ({ type: "folder" as const, value })),
    ...filteredFiles.map((value) => ({ type: "file" as const, value }))
  ], [filteredFiles, filteredFolders]);
  const previewableImages = useMemo(() => files.filter(isInlinePreviewable), [files]);
  const isLoading = (view === "files" && (isSearching ? searchQuery.isLoading : filesQuery.isLoading || foldersQuery.isLoading)) || (view === "trash" && trashQuery.isLoading) || (view === "shares" && sharesQuery.isLoading);
  const loadError = view === "files" ? isSearching ? searchQuery.error : filesQuery.error ?? foldersQuery.error : view === "trash" ? trashQuery.error : sharesQuery.error;

  function invalidateWorkspace() {
    void queryClient.invalidateQueries({ queryKey: ["files"] });
    void queryClient.invalidateQueries({ queryKey: ["folders"] });
    void queryClient.invalidateQueries({ queryKey: ["trash"] });
    void queryClient.invalidateQueries({ queryKey: ["storage"] });
    void queryClient.invalidateQueries({ queryKey: ["shares"] });
    void queryClient.invalidateQueries({ queryKey: ["file-search"] });
  }

  function show(message: string, tone: NonNullable<Toast>["tone"] = "success") {
    setToast({ message, tone });
  }

  useEffect(() => {
    const tracked = processingFilesRef.current;
    const readyNow = files.filter((file) => file.availability === "ready" && tracked.has(file.id));
    readyNow.forEach((file) => {
      tracked.delete(file.id);
      show(`“${file.original_name}”已处理完成，现在可以访问。`, "info");
    });
    files.filter((file) => file.availability === "processing").forEach((file) => tracked.add(file.id));

    if (!files.some((file) => file.availability === "processing")) return undefined;
    const timer = window.setInterval(() => {
      void queryClient.invalidateQueries({ queryKey: ["files"] });
    }, 4000);
    return () => window.clearInterval(timer);
  }, [files, queryClient]);

  async function handleUpload(fileList: FileList | File[]) {
    const queue = Array.from(fileList);
    if (queue.length === 0) return;
    setDialog(null);
    setDragging(false);
    for (const file of queue) {
      const id = `${file.name}-${file.lastModified}-${crypto.randomUUID()}`;
      setUploads((items) => [...items, { id, name: file.name, progress: 0, state: "uploading" }]);
      try {
        let resumeUploadID: string | undefined;
        if (file.size > 5 * 1024 * 1024) {
          const point = findUploadResumePoint(file, parentID);
          if (point) {
            const { uploads } = await api.listUploads();
            const task = uploads.find((item) => item.id === point.uploadID);
            if (task && taskMatchesResumePoint(task, file, parentID)) {
              if (window.confirm(`检测到“${file.name}”的未完成上传，是否继续？`)) {
                resumeUploadID = task.id;
              } else {
                await api.cancelUpload(task.id).catch(() => undefined);
                removeUploadResumePoint(file, parentID);
              }
            } else {
              removeUploadResumePoint(file, parentID);
            }
          }
        }

        await api.uploadFile(file, parentID, (progress) => {
          setUploads((items) => items.map((item) => item.id === id ? { ...item, progress } : item));
        }, (task) => saveUploadResumePoint(file, parentID, task), resumeUploadID);
        removeUploadResumePoint(file, parentID);
        setUploads((items) => items.map((item) => item.id === id ? { ...item, progress: 100, state: "finished" } : item));
        window.setTimeout(() => setUploads((items) => items.filter((item) => item.id !== id)), 2600);
      } catch (err) {
        setUploads((items) => items.map((item) => item.id === id ? { ...item, state: "failed" } : item));
        show(`${file.name} 上传已暂停：${messageOf(err)}。重新选择同一文件可继续上传。`, "error");
      }
    }
    invalidateWorkspace();
  }

  async function removeFile(file: UserFile, permanent = false) {
    let keepShares = false;
    if (permanent) {
      if (!window.confirm(`彻底删除“${file.original_name}”？该操作不可恢复。`)) return;
    } else if (!window.confirm(`将“${file.original_name}”移入回收站，并撤销全部分享链接？`)) {
      if (!window.confirm("是否保留已有分享链接并继续移入回收站？保留后，持有链接的人仍可访问该文件。")) return;
      keepShares = true;
    }
    try {
      if (permanent) await api.permanentlyDeleteFile(file.id);
      else await api.deleteFile(file.id, keepShares);
      setSelected(null);
      invalidateWorkspace();
      show(permanent ? "文件已彻底删除" : "文件已移入回收站");
    } catch (err) {
      show(messageOf(err), "error");
    }
  }

  async function restoreFile(file: UserFile) {
    try {
      await api.restoreFile(file.id);
      setSelected(null);
      invalidateWorkspace();
      show("文件已恢复到原目录");
    } catch (err) {
      show(messageOf(err), "error");
    }
  }

  async function removeFolder(folder: FolderType) {
    if (!window.confirm(`删除空文件夹“${folder.name}”？`)) return;
    try {
      await api.deleteFolder(folder.id);
      setSelected(null);
      invalidateWorkspace();
      show("文件夹已删除");
    } catch (err) {
      show(messageOf(err), "error");
    }
  }

  async function downloadFile(file: UserFile) {
    try {
      await api.download(file.id);
    } catch (err) {
      const apiError = err instanceof ApiError ? err : null;
      const hint = apiError?.status === 423 ? "文件正在处理中，暂时不可下载。" : apiError?.status === 403 ? "该文件当前不可访问。" : messageOf(err);
      show(hint, "error");
    }
  }

  function openFolder(folder: FolderType) {
    clearSearch();
    setFolderPath([...folderPath, folder.id], [...folderNames, folder.name]);
  }

  function setFolderPath(nextPath: number[], nextNames: string[] = []) {
    if (nextPath.length === 0) {
      setSearchParams({});
      return;
    }

    setSearchParams({
      folder: String(nextPath.at(-1)),
      path: nextPath.join(","),
      names: nextNames.join(",")
    });
  }

  function goUp() {
    clearSearch();
    setFolderPath(folderPath.slice(0, -1), folderNames.slice(0, -1));
  }

  function goToPath(index: number) {
    clearSearch();
    setFolderPath(folderPath.slice(0, index + 1), folderNames.slice(0, index + 1));
  }

  function clearSearch() {
    setSearch("");
    setSearchKind("");
    setSearchPeriod("");
  }

  function logout() {
    setAccountMenuOpen(false);
    clearSession();
    onLogout();
    navigate("/login", { replace: true });
  }

  const currentTitle = view === "trash" ? "回收站" : view === "shares" ? "分享链接" : view === "settings" ? "设置" : isSearching ? "搜索结果" : parentID ? folderNames.at(-1) || "当前文件夹" : "我的文件";

  return (
    <div className="workspace-shell">
      <aside className="sidebar">
        <div className="sidebar-brand"><img className="brand-image" src="/cloudbox-icon.png" alt="CloudBox" /><strong>CloudBox</strong></div>
        <nav className="side-nav" aria-label="主导航">
          <NavLink to="/files" title="我的文件" className={({ isActive }) => navClass(isActive && view === "files")}><LayoutGrid size={18} />我的文件</NavLink>
          <NavLink to="/shares" title="分享链接" className={({ isActive }) => navClass(isActive)}><Share2 size={18} />分享链接</NavLink>
          <NavLink to="/trash" title="回收站" className={({ isActive }) => navClass(isActive)}><Trash2 size={18} />回收站</NavLink>
        </nav>
        <div className="sidebar-storage">
          <div className="storage-label"><span>存储空间</span><HardDrive size={15} /></div>
          <div className="meter"><span style={{ width: `${usagePercent(storageQuery.data)}%` }} /></div>
          <p>{formatBytes(storageQuery.data?.used_bytes ?? 0)} / {formatBytes(storageQuery.data?.quota_bytes ?? 0)}</p>
        </div>
        <div className="sidebar-bottom">
          <NavLink to="/settings" title="设置" className={({ isActive }) => navClass(isActive)}><Settings size={18} />设置</NavLink>
          <button className="side-action" type="button" title="退出登录" onClick={logout}><LogOut size={18} />退出登录</button>
        </div>
      </aside>

      <main className="workspace-main">
        <header className="topbar">
          <div className="breadcrumb">
            {view === "files" ? <>
              <button type="button" className="breadcrumb-root" onClick={() => { setFolderPath([]); clearSearch(); }}>我的文件</button>
              {folderPath.map((id, index) => <span className="breadcrumb-item" key={id}><ChevronRight size={16} />{index === folderPath.length - 1 ? <span>{folderNames[index] || "当前文件夹"}</span> : <button type="button" className="breadcrumb-root" onClick={() => goToPath(index)}>{folderNames[index] || "当前文件夹"}</button>}</span>)}
            </> : <span className="topbar-context">{currentTitle}</span>}
          </div>
          <div className="topbar-actions">
            {view === "files" && <label className="search-box"><Search size={17} /><input value={search} onChange={(event) => setSearch(event.target.value)} placeholder="搜索全部文件" /></label>}
            <button type="button" className="icon-button" title="帮助" aria-label="打开帮助" aria-expanded={helpOpen} onClick={() => { setHelpOpen(true); setAccountMenuOpen(false); }}><CircleHelp size={19} /></button>
            <div className="account-menu-wrap" ref={accountMenuRef}>
              <button type="button" className="user-button" title={`当前用户：${session.username}`} aria-label="打开账户菜单" aria-expanded={accountMenuOpen} onClick={() => { setAccountMenuOpen((open) => !open); setHelpOpen(false); }}><span>{session.username.slice(0, 1).toUpperCase()}</span></button>
              {accountMenuOpen && <div className="account-menu" role="menu" aria-label="账户菜单">
                <div className="account-menu-identity"><strong>{session.username}</strong><span>当前登录账号</span></div>
                <button type="button" role="menuitem" onClick={() => { setAccountMenuOpen(false); navigate("/settings"); }}><Settings size={16} />账户设置</button>
                <button type="button" role="menuitem" className="account-menu-logout" onClick={logout}><LogOut size={16} />退出登录</button>
              </div>}
            </div>
          </div>
        </header>

        <section className="content-area">
          <div className="page-heading">
            <div><p className="eyebrow">WORKSPACE</p>{view === "files" && parentID && <button type="button" className="back-button" onClick={goUp}><ArrowLeft size={16} />返回上一级</button>}<h1>{currentTitle}</h1></div>
            {view === "files" && <div className="heading-actions">
              <button type="button" className="secondary-button" onClick={() => setDialog("folder")}><FolderPlus size={17} />新建文件夹</button>
              <button type="button" className="primary-button" onClick={() => setDialog("upload")}><Upload size={17} />上传文件</button>
            </div>}
            {view === "shares" && <div className="heading-actions">
              <button type="button" className="primary-button" onClick={() => setDialog("save-share")}><Download size={17} />保存分享文件</button>
            </div>}
          </div>

          {view === "files" && <div className="search-filters" aria-label="搜索筛选">
            <span>筛选</span>
            <select value={searchKind} onChange={(event) => setSearchKind(event.target.value as typeof searchKind)} aria-label="文件类型">
              <option value="">全部类型</option><option value="image">图片</option><option value="video">视频</option><option value="other">其他</option>
            </select>
            <select value={searchPeriod} onChange={(event) => setSearchPeriod(event.target.value as typeof searchPeriod)} aria-label="创建时间">
              <option value="">全部时间</option><option value="7d">最近 7 日</option><option value="30d">最近 30 日</option><option value="year">今年</option><option value="older">更早</option>
            </select>
            {isSearching && <button type="button" className="filter-clear" onClick={clearSearch}>清除筛选</button>}
          </div>}

          {view === "settings" ? (
            <SettingsView session={session} usage={storageQuery.data} />
          ) : view === "shares" ? (
            <SharesView shares={sharesQuery.data?.shares ?? []} loading={isLoading} error={loadError} onCopy={() => show("分享链接已复制到剪贴板")} onRevoke={async (share) => {
              if (!window.confirm("撤销该分享链接？")) return;
              try { await api.revokeShare(share.token); invalidateWorkspace(); show("分享链接已撤销"); } catch (err) { show(messageOf(err), "error"); }
            }} />
          ) : (
            <div className={`file-layout ${view} ${selected ? "details-open" : ""}`}>
              <section className="file-surface" onDragOver={(event) => { event.preventDefault(); setDragging(true); }} onDragLeave={() => setDragging(false)} onDrop={(event) => { event.preventDefault(); void handleUpload(event.dataTransfer.files); }}>
                {dragging && <div className="drop-overlay"><Upload size={24} /><strong>释放以上传到当前目录</strong><span>上传后会完成处理，完成后即可访问</span></div>}
                <div className="file-table-wrap">
                  <div className="section-label">{view === "trash" ? "已删除文件" : isSearching ? "搜索结果" : "目录内容"} <span>{view === "trash" ? filteredFiles.length : entries.length}</span></div>
                  <div className="file-table">
                    <div className="file-row file-row-head"><span>名称</span><span>大小</span><span>类型</span><span>创建时间</span><span /></div>
                    {isLoading && <LoadingRows />}
                    {loadError && <div className="empty-state error-state"><p>无法加载内容</p><span>{messageOf(loadError)}</span></div>}
                    {!isLoading && !loadError && (view === "trash" ? filteredFiles.length : entries.length) === 0 && <EmptyFiles trash={view === "trash"} searching={Boolean(search)} />}
                    {view === "trash" ? filteredFiles.map((file) => <FileRow key={file.id} file={file} selected={selected?.type === "file" && selected.value.id === file.id} trash onSelect={() => setSelected({ type: "file", value: file })} onDownload={() => void downloadFile(file)} onDelete={() => void removeFile(file, true)} onRestore={() => void restoreFile(file)} />) : entries.map((entry) => <DirectoryEntryRow key={`${entry.type}-${entry.value.id}`} entry={entry} selected={selected?.type === entry.type && selected.value.id === entry.value.id} onSelect={() => setSelected(entry)} onOpenFolder={() => entry.type === "folder" && openFolder(entry.value)} onDownload={() => entry.type === "file" && void downloadFile(entry.value)} onDelete={() => entry.type === "file" && void removeFile(entry.value)} />)}
                  </div>
                  {view === "files" && <MediaGrid entries={entries} onOpenFolder={openFolder} onSelect={(entry) => setSelected(entry)} onPreview={(file) => setImageViewer(file)} />}
                </div>
              </section>
              {selected && <DetailPanel selected={selected} trash={view === "trash"} onClose={() => setSelected(null)} onDownload={() => selected.type === "file" && void downloadFile(selected.value)} onDelete={() => selected.type === "file" ? void removeFile(selected.value, view === "trash") : void removeFolder(selected.value)} onRestore={() => selected.type === "file" && void restoreFile(selected.value)} onRename={() => setDialog("rename")} onMove={() => setDialog("move")} onShare={() => setDialog("share")} onPreview={() => selected.type === "file" && isInlinePreviewable(selected.value) && setImageViewer(selected.value)} />}
            </div>
          )}
        </section>
      </main>

      <nav className="mobile-nav" aria-label="移动端导航">
        <NavLink to="/files" className={({ isActive }) => navClass(isActive && view === "files")}><LayoutGrid size={19} /><span>文件</span></NavLink>
        <NavLink to="/shares" className={({ isActive }) => navClass(isActive)}><Share2 size={19} /><span>分享</span></NavLink>
        <NavLink to="/trash" className={({ isActive }) => navClass(isActive)}><Trash2 size={19} /><span>回收站</span></NavLink>
        <NavLink to="/settings" className={({ isActive }) => navClass(isActive)}><Settings size={19} /><span>我的</span></NavLink>
      </nav>

      {uploads.length > 0 && <UploadQueue items={uploads} />}
      {toast && <div className={`toast toast-${toast.tone}`}>{toast.tone === "success" ? <ShieldCheck size={17} /> : <CircleHelp size={17} />}<span>{toast.message}</span><button type="button" aria-label="关闭提示" onClick={() => setToast(null)}><X size={15} /></button></div>}
      {dialog === "upload" && <UploadDialog onClose={() => setDialog(null)} onUpload={handleUpload} />}
      {dialog === "folder" && <FolderDialog onClose={() => setDialog(null)} onSubmit={async (name) => {
        try { await api.createFolder(name, parentID); invalidateWorkspace(); setDialog(null); show("文件夹已创建"); } catch (err) { show(messageOf(err), "error"); }
      }} />}
      {dialog === "rename" && selected && <RenameDialog selected={selected} onClose={() => setDialog(null)} onSubmit={async (name) => {
        try {
          if (selected.type === "file") await api.renameFile(selected.value.id, name);
          else await api.renameFolder(selected.value.id, name);
          invalidateWorkspace(); setDialog(null); show("名称已更新");
        } catch (err) { show(messageOf(err), "error"); }
      }} />}
      {dialog === "move" && selected && <MoveDialog selected={selected} folders={rootFoldersQuery.data?.folders ?? []} currentParentID={parentID} onClose={() => setDialog(null)} onSubmit={async (target) => {
        try {
          if (selected.type === "file") await api.moveFile(selected.value.id, target);
          else await api.moveFolder(selected.value.id, target);
          invalidateWorkspace(); setDialog(null); show("已移动到目标目录");
        } catch (err) { show(messageOf(err), "error"); }
      }} />}
      {dialog === "share" && selected?.type === "file" && <ShareDialog file={selected.value} onClose={() => setDialog(null)} onSubmit={async (data) => {
        try {
          const { share } = await api.createShare(selected.value.id, data);
          await navigator.clipboard.writeText(publicShareURL(share.token));
          invalidateWorkspace(); setDialog(null); show("分享链接已创建并复制到剪贴板");
        } catch (err) { show(messageOf(err), "error"); }
      }} />}
      {dialog === "save-share" && <SaveShareDialog folders={rootFoldersQuery.data?.folders ?? []} onClose={() => setDialog(null)} onSubmit={async (data) => {
        try {
          const { file } = await api.saveSharedFile(data.token, data.password, data.parent_id);
          invalidateWorkspace();
          setDialog(null);
          show(`“${file.original_name}”已保存到我的文件`);
        } catch (err) { show(messageOf(err), "error"); }
      }} />}
      {helpOpen && <HelpDialog onClose={() => setHelpOpen(false)} />}
      {imageViewer && <ImageViewer file={imageViewer} images={previewableImages} onClose={() => setImageViewer(null)} onSelect={setImageViewer} onDownload={(file) => void downloadFile(file)} onShare={(file) => { setSelected({ type: "file", value: file }); setImageViewer(null); setDialog("share"); }} onDelete={(file) => { setImageViewer(null); void removeFile(file); }} />}
    </div>
  );
}

function FileRow({ file, selected, trash, onSelect, onDownload, onDelete, onRestore }: { file: UserFile; selected: boolean; trash: boolean; onSelect: () => void; onDownload: () => void; onDelete: () => void; onRestore: () => void }) {
  const accessible = file.availability === "ready";
  return <div className={`file-row ${selected ? "selected" : ""}`} onClick={onSelect} onDoubleClick={accessible ? onDownload : undefined}>
    <span className="file-name"><FileKindIcon file={file} /><strong>{file.original_name}</strong></span>
    <span>{formatBytes(file.size)}</span><span className="file-type">{shortType(file.content_type)}</span><span>{trash && file.cleanup_at ? `清理于 ${formatDate(file.cleanup_at)}` : formatDate(file.created_at)}</span>
    <span className="row-actions" onClick={(event) => event.stopPropagation()}>
      {trash ? <><IconAction label="恢复" onClick={onRestore}><Archive size={16} /></IconAction><IconAction label="彻底删除" tone="danger" onClick={onDelete}><Trash2 size={16} /></IconAction></> : <>{accessible && <IconAction label="下载" onClick={onDownload}><Download size={16} /></IconAction>}<IconAction label="移入回收站" tone="danger" onClick={onDelete}><Trash2 size={16} /></IconAction></>}
    </span>
  </div>;
}

function DirectoryEntryRow({ entry, selected, onSelect, onOpenFolder, onDownload, onDelete }: { entry: DirectoryEntry; selected: boolean; onSelect: () => void; onOpenFolder: () => void; onDownload: () => void; onDelete: () => void }) {
  const isFolder = entry.type === "folder";
  const name = isFolder ? entry.value.name : entry.value.original_name;
  const createdAt = isFolder ? entry.value.created_at : entry.value.created_at;

  const accessible = isFolder || entry.value.availability === "ready";
  return <div className={`file-row ${selected ? "selected" : ""} ${isFolder ? "folder-row" : ""}`} onClick={onSelect} onDoubleClick={isFolder ? onOpenFolder : accessible ? onDownload : undefined}>
    <span className="file-name">{isFolder ? <FolderOpen size={20} className="kind-folder" fill="currentColor" /> : <FileKindIcon file={entry.value} />}<strong>{name}</strong></span>
    <span>{formatBytes(entry.value.size)}</span>
    <span className="file-type">{isFolder ? "文件夹" : shortType(entry.value.content_type)}</span>
    <span>{formatDate(createdAt)}</span>
    <span className="row-actions" onClick={(event) => event.stopPropagation()}>
      {isFolder ? <IconAction label="打开文件夹" onClick={onOpenFolder}><ChevronRight size={17} /></IconAction> : <>{accessible && <IconAction label="下载" onClick={onDownload}><Download size={16} /></IconAction>}<IconAction label="移入回收站" tone="danger" onClick={onDelete}><Trash2 size={16} /></IconAction></>}
    </span>
  </div>;
}

function DetailPanel({ selected, trash, onClose, onDownload, onDelete, onRestore, onRename, onMove, onShare, onPreview }: { selected: Exclude<SelectedItem, null>; trash: boolean; onClose: () => void; onDownload: () => void; onDelete: () => void; onRestore: () => void; onRename: () => void; onMove: () => void; onShare: () => void; onPreview: () => void }) {
  const file = selected.type === "file" ? selected.value : null;
  const accessible = file?.availability === "ready";
  const [preview, setPreview] = useState<string | null>(null);
  useEffect(() => {
    if (!file?.content_type.startsWith("image/")) { setPreview(null); return undefined; }
    let objectURL: string | null = null;
    void api.thumbnail(file.id).then((blob) => { objectURL = URL.createObjectURL(blob); setPreview(objectURL); }).catch(() => setPreview(null));
    return () => { if (objectURL) URL.revokeObjectURL(objectURL); };
  }, [file?.id, file?.content_type]);

  const name = selected.type === "file" ? selected.value.original_name : selected.value.name;
  return <aside className="detail-panel">
    <div className="detail-top"><span>详细信息</span><button className="icon-button" type="button" onClick={onClose} aria-label="关闭详情" title="关闭详情"><X size={18} /></button></div>
    <div className="preview-box">{file && preview ? <button type="button" className="preview-trigger" onClick={accessible ? onPreview : undefined} disabled={!accessible} title="查看图片"><img src={preview} alt={`${name} 缩略图`} /></button> : file ? <FileKindIcon file={file} size={54} /> : <Folder size={56} fill="currentColor" />}{file?.content_type.startsWith("image/") && !preview && <span className="preview-unavailable"><ImageOff size={16} />暂无预览</span>}</div>
    <div className="detail-name"><strong>{name}</strong><span>{file ? shortType(file.content_type) : "文件夹"}</span></div>
    <div className="detail-actions">
      {file && !trash && accessible && <><button className="primary-button compact" type="button" onClick={onDownload}><Download size={16} />下载</button><button className="secondary-button compact" type="button" onClick={onShare}><Link size={16} />分享</button></>}
      {file && trash && <button className="primary-button compact" type="button" onClick={onRestore}><Archive size={16} />恢复文件</button>}
      {!trash && <><button className="icon-action-wide" type="button" onClick={onRename}><Pencil size={16} />重命名</button><button className="icon-action-wide" type="button" onClick={onMove}><MoveRight size={16} />移动到</button></>}
      <button className="icon-action-wide danger" type="button" onClick={onDelete}><Trash2 size={16} />{trash ? "彻底删除" : "移入回收站"}</button>
    </div>
    <dl className="metadata-list">
      <div><dt>类型</dt><dd>{file ? shortType(file.content_type) : "文件夹"}</dd></div>
      <div><dt>大小</dt><dd>{formatBytes(file ? file.size : selected.value.size)}</dd></div>
      <div><dt>创建时间</dt><dd>{formatDate(selected.value.created_at)}</dd></div>
      {file && <div><dt>状态</dt><dd><span className={`availability-${file.availability}`}>{availabilityLabel(file.availability)}</span></dd></div>}
    </dl>
    {file && file.availability === "processing" && <p className="scan-note"><LoaderCircle size={15} className="spin" />文件正在处理中，完成后可下载和分享。</p>}
    {file && file.availability === "unavailable" && <p className="scan-note unavailable-note"><ImageOff size={15} />该文件当前不可访问。</p>}
    {file?.content_type === "image/heic" && <p className="scan-note"><ImageOff size={15} />该图片格式暂不支持浏览器预览，可下载后查看。</p>}
    {file?.content_type.startsWith("video/") && <p className="scan-note"><FileVideo size={15} />视频暂不支持在线播放，可下载后观看。</p>}
  </aside>;
}

function MediaGrid({ entries, onOpenFolder, onSelect, onPreview }: { entries: DirectoryEntry[]; onOpenFolder: (folder: FolderType) => void; onSelect: (entry: DirectoryEntry) => void; onPreview: (file: UserFile) => void }) {
  return <div className="mobile-media-grid">{entries.map((entry) => {
    const folder = entry.type === "folder";
    const file = folder ? null : entry.value;
    return <MediaTile key={`mobile-${entry.type}-${entry.value.id}`} entry={entry} onOpenFolder={onOpenFolder} onSelect={onSelect} onPreview={onPreview} />;
  })}</div>;
}

function MediaTile({ entry, onOpenFolder, onSelect, onPreview }: { entry: DirectoryEntry; onOpenFolder: (folder: FolderType) => void; onSelect: (entry: DirectoryEntry) => void; onPreview: (file: UserFile) => void }) {
  const folder = entry.type === "folder";
  const file = folder ? null : entry.value;
  const canPreview = Boolean(file && isInlinePreviewable(file));
  const [thumbnail, setThumbnail] = useState<string | null>(null);

  useEffect(() => {
    if (!file || !isThumbnailSupported(file)) { setThumbnail(null); return undefined; }
    let objectURL: string | null = null;
    void api.thumbnail(file.id).then((blob) => { objectURL = URL.createObjectURL(blob); setThumbnail(objectURL); }).catch(() => setThumbnail(null));
    return () => { if (objectURL) URL.revokeObjectURL(objectURL); };
  }, [file?.id, file?.content_type, file?.availability]);

  return <button type="button" className="mobile-media-tile" onClick={() => folder ? onOpenFolder(entry.value) : canPreview ? onPreview(file!) : onSelect(entry)}>
    <span className="mobile-media-icon">{thumbnail ? <img src={thumbnail} alt="" /> : folder ? <FolderOpen size={31} fill="currentColor" /> : <FileKindIcon file={file!} size={31} />}</span>
    <strong>{folder ? entry.value.name : file!.original_name}</strong>
    <small>{folder ? `${formatBytes(entry.value.size)} · 文件夹` : `${formatBytes(file!.size)} · ${availabilityLabel(file!.availability)}`}</small>
  </button>;
}

function ImageViewer({ file, images, onClose, onSelect, onDownload, onShare, onDelete }: { file: UserFile; images: UserFile[]; onClose: () => void; onSelect: (file: UserFile) => void; onDownload: (file: UserFile) => void; onShare: (file: UserFile) => void; onDelete: (file: UserFile) => void }) {
  const [source, setSource] = useState<string | null>(null);
  const [error, setError] = useState("");
  const currentIndex = images.findIndex((item) => item.id === file.id);
  const previous = currentIndex > 0 ? images[currentIndex - 1] : null;
  const next = currentIndex >= 0 && currentIndex < images.length - 1 ? images[currentIndex + 1] : null;
  useEffect(() => {
    let objectURL: string | null = null;
    setSource(null);
    setError("");
    void api.preview(file.id).then((blob) => { objectURL = URL.createObjectURL(blob); setSource(objectURL); }).catch((err) => setError(messageOf(err)));
    return () => { if (objectURL) URL.revokeObjectURL(objectURL); };
  }, [file.id]);
  useEffect(() => {
    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") onClose();
      if (event.key === "ArrowLeft" && previous) onSelect(previous);
      if (event.key === "ArrowRight" && next) onSelect(next);
    }
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [next, onClose, onSelect, previous]);
  return <div className="image-viewer" role="dialog" aria-modal="true" aria-label={`${file.original_name} 图片预览`} onClick={onClose}>
    <div className="image-viewer-toolbar" onClick={(event) => event.stopPropagation()}><strong>{file.original_name}</strong><div><IconAction label="下载" onClick={() => onDownload(file)}><Download size={18} /></IconAction><IconAction label="分享" onClick={() => onShare(file)}><Link size={18} /></IconAction><IconAction label="移入回收站" tone="danger" onClick={() => onDelete(file)}><Trash2 size={18} /></IconAction><button className="icon-button" type="button" onClick={onClose} aria-label="关闭图片预览"><X size={20} /></button></div></div>
    {previous && <button className="image-viewer-nav previous" type="button" title="上一张" aria-label="上一张" onClick={(event) => { event.stopPropagation(); onSelect(previous); }}><ChevronLeft size={24} /></button>}
    {next && <button className="image-viewer-nav next" type="button" title="下一张" aria-label="下一张" onClick={(event) => { event.stopPropagation(); onSelect(next); }}><ChevronRight size={24} /></button>}
    {source ? <img src={source} alt={file.original_name} onClick={(event) => event.stopPropagation()} /> : <p>{error || "正在加载图片…"}</p>}
  </div>;
}

function SharesView({ shares, loading, error, onCopy, onRevoke }: { shares: Share[]; loading: boolean; error: unknown; onCopy: () => void; onRevoke: (share: Share) => void }) {
  const [copiedToken, setCopiedToken] = useState<string | null>(null);

  async function copyShareLink(token: string) {
    try {
      await navigator.clipboard.writeText(publicShareURL(token));
      setCopiedToken(token);
      onCopy();
      window.setTimeout(() => setCopiedToken((current) => current === token ? null : current), 1800);
    } catch {
      // 剪贴板权限被拒绝时，使用已有的全局提示说明失败原因。
      setCopiedToken(null);
    }
  }

  return <section className="share-surface"><div className="share-intro"><div><p className="eyebrow">EXTERNAL ACCESS</p><h2>分享链接</h2><p>从文件详情创建链接。默认有效期为 7 天，可设置密码和下载次数。</p></div><Share2 size={28} /></div><div className="share-table"><div className="share-row share-row-head"><span>分享文件</span><span>下载次数</span><span>过期时间</span><span /></div>{loading && <LoadingRows />}{Boolean(error) && <div className="empty-state error-state"><p>无法加载分享链接</p><span>{messageOf(error)}</span></div>}{!loading && !error && shares.length === 0 && <div className="empty-state"><Share2 size={28} /><p>还没有有效的分享链接</p><span>在文件详情中点击“分享”即可创建。</span></div>}{shares.map((share) => {
    const copied = copiedToken === share.token;
    return <div className="share-row" key={share.token}><span className="share-file-summary"><FileKindIcon file={{ id: 0, user_id: 0, parent_id: null, original_name: share.original_name, storage_path: "", size: share.size, content_type: share.content_type, status: "active", availability: "ready", created_at: share.created_at }} /><span><strong>{share.original_name}</strong><small>{formatBytes(share.size)} · {shortType(share.content_type)}</small></span><span className="share-token"><button type="button" className={copied ? "share-copy copied" : "share-copy"} title={copied ? "链接已复制" : "复制公开链接"} aria-label={copied ? "链接已复制" : "复制公开链接"} onClick={() => void copyShareLink(share.token)}>{copied ? <Check size={15} /> : <Link size={15} />}</button>{copied && <span className="copy-status" role="status">已复制</span>}</span></span><span>{share.download_count}{share.max_downloads ? ` / ${share.max_downloads}` : ""}</span><span>{share.expires_at ? formatDate(share.expires_at) : "7 天后过期"}</span><span><IconAction label="撤销分享" tone="danger" onClick={() => onRevoke(share)}><Trash2 size={16} /></IconAction></span></div>;
  })}</div></section>;
}

function SettingsView({ session, usage }: { session: Session; usage: StorageUsage | undefined }) {
  const queryClient = useQueryClient();
  const [notice, setNotice] = useState("");
  const [latestInvitation, setLatestInvitation] = useState<CreatedInvitation | null>(null);
  const isAdmin = session.role === "admin";
  const usersQuery = useQuery({ queryKey: ["admin-users"], queryFn: api.listAdminUsers, enabled: isAdmin });
  const invitationsQuery = useQuery({ queryKey: ["admin-invitations"], queryFn: api.listInvitations, enabled: isAdmin });
  const users = usersQuery.data?.users ?? [];
  const invitations = invitationsQuery.data?.invitations ?? [];

  function refreshAdmin() {
    void queryClient.invalidateQueries({ queryKey: ["admin-users"] });
    void queryClient.invalidateQueries({ queryKey: ["admin-invitations"] });
  }
  async function createInvitation() {
    try {
      const { invitation } = await api.createInvitation();
      setLatestInvitation(invitation);
      setNotice("邀请码已创建，仅在当前页面展示一次。");
      refreshAdmin();
    } catch (err) { setNotice(messageOf(err)); }
  }
  async function revokeInvitation(id: number) {
    if (!window.confirm("撤销这个未使用的邀请码？")) return;
    try { await api.revokeInvitation(id); setNotice("邀请码已撤销。"); refreshAdmin(); } catch (err) { setNotice(messageOf(err)); }
  }
  async function updateQuota(user: AccountUser, value: string) {
    const quota = Number(value) * 1024 * 1024 * 1024;
    if (!Number.isFinite(quota) || quota <= 0) { setNotice("请输入有效的容量档位。"); return; }
    try { await api.setAdminUserQuota(user.id, quota); setNotice(`已更新 ${user.username} 的容量。`); refreshAdmin(); } catch (err) { setNotice(messageOf(err)); }
  }
  async function toggleUser(user: AccountUser) {
    const next = user.status === "active" ? "disabled" : "active";
    if (!window.confirm(next === "disabled" ? `停用账号“${user.username}”？其现有登录状态会立即失效。` : `重新启用账号“${user.username}”？`)) return;
    try { await api.setAdminUserStatus(user.id, next); setNotice(`账号“${user.username}”已${next === "disabled" ? "停用" : "启用"}。`); refreshAdmin(); } catch (err) { setNotice(messageOf(err)); }
  }
  async function resetPassword(user: AccountUser) {
    if (!window.confirm(`重置“${user.username}”的密码？旧登录状态将立即失效。`)) return;
    try { const result = await api.resetAdminUserPassword(user.id); setNotice(`临时密码：${result.temporary_password}。请安全地转交给用户；关闭提示后无法再次查看。`); refreshAdmin(); } catch (err) { setNotice(messageOf(err)); }
  }
  async function revokeShares(user: AccountUser) {
    if (!window.confirm(`撤销“${user.username}”的全部分享链接？`)) return;
    try { const result = await api.revokeAdminUserShares(user.id); setNotice(`已撤销 ${result.revoked} 个分享链接。`); } catch (err) { setNotice(messageOf(err)); }
  }

  return <section className="settings-layout"><div className="settings-section"><p className="eyebrow">ACCOUNT</p><h2>账号与存储</h2><div className="setting-row"><div className="avatar-large">{session.username.slice(0, 1).toUpperCase()}</div><div><strong>{session.username}</strong><p>{isAdmin ? "管理员账号" : "当前登录账号"}</p></div></div></div><div className="settings-section"><div className="setting-heading"><div><p className="eyebrow">STORAGE</p><h2>容量使用情况</h2></div><strong>{usagePercent(usage)}%</strong></div><div className="large-meter"><span style={{ width: `${usagePercent(usage)}%` }} /></div><div className="usage-numbers"><span>已使用 <strong>{formatBytes(usage?.used_bytes ?? 0)}</strong></span><span>剩余 <strong>{formatBytes(usage?.available_bytes ?? 0)}</strong></span><span>总容量 <strong>{formatBytes(usage?.quota_bytes ?? 0)}</strong></span></div></div>{isAdmin && <><section className="settings-section admin-section"><div className="setting-heading"><div><p className="eyebrow">INVITATIONS</p><h2>邀请码</h2></div><button className="primary-button compact" type="button" onClick={() => void createInvitation()}>创建邀请码</button></div><p className="settings-copy">邀请码有效期为 7 天且仅可使用一次。创建后的邀请码不会再次显示。</p>{latestInvitation && <div className="secret-result"><strong>邀请码</strong><code>{latestInvitation.code}</code><button type="button" className="secondary-button compact" onClick={() => void navigator.clipboard.writeText(latestInvitation.code).then(() => setNotice("邀请码已复制。"))}>复制</button></div>}<div className="admin-list">{invitations.length === 0 ? <p>尚未创建邀请码。</p> : invitations.map((invitation) => <div className="admin-list-row" key={invitation.id}><span><strong>创建于 {formatDate(invitation.created_at)}</strong><small>{invitation.used_at ? "已使用" : invitation.revoked_at ? "已撤销" : `有效至 ${formatDate(invitation.expires_at)}`}</small></span>{!invitation.used_at && !invitation.revoked_at && <IconAction label="撤销邀请码" tone="danger" onClick={() => void revokeInvitation(invitation.id)}><Trash2 size={16} /></IconAction>}</div>)}</div></section><section className="settings-section admin-section"><p className="eyebrow">USERS</p><h2>用户管理</h2><p className="settings-copy">管理员可调整容量、停用账号、重置密码和撤销分享，但不能浏览用户私有文件。</p><div className="admin-list">{usersQuery.isLoading ? <p>正在加载用户…</p> : users.map((user) => <div className="admin-user-row" key={user.id}><span><strong>{user.username}</strong><small>{user.role === "admin" ? "管理员" : "普通用户"} · {user.status === "active" ? "正常" : "已停用"} · {formatBytes(user.storage_quota_bytes)}</small></span><select aria-label={`${user.username} 容量`} value={String(user.storage_quota_bytes / (1024 * 1024 * 1024))} onChange={(event) => void updateQuota(user, event.target.value)} disabled={user.id === users.find((candidate) => candidate.username === session.username)?.id}><option value="1">1 GB</option><option value="5">5 GB</option><option value="10">10 GB</option></select>{user.username !== session.username && <div className="admin-row-actions"><IconAction label={user.status === "active" ? "停用账号" : "启用账号"} tone={user.status === "active" ? "danger" : undefined} onClick={() => void toggleUser(user)}>{user.status === "active" ? <Archive size={16} /> : <Check size={16} />}</IconAction><IconAction label="重置密码" onClick={() => void resetPassword(user)}><Pencil size={16} /></IconAction><IconAction label="撤销全部分享" tone="danger" onClick={() => void revokeShares(user)}><Link size={16} /></IconAction></div>}</div>)}</div></section></>}{notice && <p className="settings-notice" role="status">{notice}</p>}</section>;
}

function UploadDialog({ onClose, onUpload }: { onClose: () => void; onUpload: (files: FileList | File[]) => void }) {
  const inputRef = useRef<HTMLInputElement>(null);
  const [dropping, setDropping] = useState(false);
  return <Dialog title="上传文件" onClose={onClose}><div className={`upload-dropzone ${dropping ? "is-dropping" : ""}`} onDragOver={(event) => { event.preventDefault(); setDropping(true); }} onDragLeave={() => setDropping(false)} onDrop={(event) => { event.preventDefault(); onUpload(event.dataTransfer.files); }}><Upload size={30} /><strong>拖入文件到这里</strong><span>大于 5 MB 的文件会自动使用分片上传。</span><button className="secondary-button" type="button" onClick={() => inputRef.current?.click()}>选择文件</button><input ref={inputRef} type="file" multiple hidden onChange={(event: ChangeEvent<HTMLInputElement>) => { if (event.target.files) onUpload(event.target.files); }} /></div></Dialog>;
}

function FolderDialog({ onClose, onSubmit }: { onClose: () => void; onSubmit: (name: string) => void }) {
  const [name, setName] = useState("");
  return <Dialog title="新建文件夹" onClose={onClose}><form className="dialog-form" onSubmit={(event) => { event.preventDefault(); onSubmit(name); }}><label>文件夹名称<input autoFocus value={name} onChange={(event) => setName(event.target.value)} placeholder="例如：项目资料" required /></label><DialogActions onClose={onClose} submit="创建文件夹" /></form></Dialog>;
}

function RenameDialog({ selected, onClose, onSubmit }: { selected: Exclude<SelectedItem, null>; onClose: () => void; onSubmit: (name: string) => void }) {
  const [name, setName] = useState(selected.type === "file" ? selected.value.original_name : selected.value.name);
  return <Dialog title="重命名" onClose={onClose}><form className="dialog-form" onSubmit={(event) => { event.preventDefault(); onSubmit(name); }}><label>新名称<input autoFocus value={name} onChange={(event) => setName(event.target.value)} required /></label><DialogActions onClose={onClose} submit="保存修改" /></form></Dialog>;
}

function MoveDialog({ selected, folders, currentParentID, onClose, onSubmit }: { selected: Exclude<SelectedItem, null>; folders: FolderType[]; currentParentID: number | null; onClose: () => void; onSubmit: (target: number | null) => void }) {
  const [target, setTarget] = useState<number | null>(currentParentID);
  const choices = folders.filter((folder) => selected.type !== "folder" || folder.id !== selected.value.id);
  return <Dialog title="移动到" onClose={onClose}><form className="dialog-form" onSubmit={(event) => { event.preventDefault(); onSubmit(target); }}><label>目标目录<select value={target ?? "root"} onChange={(event) => setTarget(event.target.value === "root" ? null : Number(event.target.value))}><option value="root">我的文件（根目录）</option>{choices.map((folder) => <option key={folder.id} value={folder.id}>{folder.name}</option>)}</select></label><p className="field-hint">当前版本可选择根目录或顶层文件夹。</p><DialogActions onClose={onClose} submit="移动" /></form></Dialog>;
}

function ShareDialog({ file, onClose, onSubmit }: { file: UserFile; onClose: () => void; onSubmit: (data: { password?: string; expires_at?: string; max_downloads?: number }) => void }) {
  const [password, setPassword] = useState("");
  const [validDays, setValidDays] = useState("7");
  const [maxDownloads, setMaxDownloads] = useState("");
  return <Dialog title="创建分享链接" onClose={onClose}><form className="dialog-form" onSubmit={(event) => {
    event.preventDefault();
    const days = Number(validDays);
    const expiresAt = new Date(Date.now() + days * 24 * 60 * 60 * 1000).toISOString();
    onSubmit({ password: password || undefined, expires_at: expiresAt, max_downloads: maxDownloads ? Number(maxDownloads) : undefined });
  }}><p className="dialog-subtitle">为“{file.original_name}”创建受控分享链接。链接默认有效 7 天，下载次数不限。</p><label>访问密码 <span className="optional">可选</span><input value={password} onChange={(event) => setPassword(event.target.value)} type="password" placeholder="留空则无需密码" /></label><label>有效天数<input value={validDays} onChange={(event) => setValidDays(event.target.value)} type="number" min="1" max="365" step="1" required /><span className="field-hint">链接将在创建后按填写天数自动过期。</span></label><label>最大下载次数 <span className="optional">高级设置</span><input value={maxDownloads} onChange={(event) => setMaxDownloads(event.target.value)} type="number" min="1" placeholder="留空则不限次数" /></label><DialogActions onClose={onClose} submit="创建并复制链接" /></form></Dialog>;
}

function SaveShareDialog({ folders, onClose, onSubmit }: { folders: FolderType[]; onClose: () => void; onSubmit: (data: { token: string; password: string; parent_id: number | null }) => void }) {
  const [shareReference, setShareReference] = useState("");
  const [password, setPassword] = useState("");
  const [parentID, setParentID] = useState<number | null>(null);
  const token = shareTokenFromReference(shareReference);

  return <Dialog title="保存分享文件" onClose={onClose}><form className="dialog-form" onSubmit={(event) => { event.preventDefault(); onSubmit({ token, password, parent_id: parentID }); }}><p className="dialog-subtitle">输入分享令牌或完整公开链接，将文件保存到自己的工作区。</p><label>分享令牌或链接<input autoFocus value={shareReference} onChange={(event) => setShareReference(event.target.value)} placeholder="粘贴令牌或 https://.../share/令牌" required /></label><label>访问密码 <span className="optional">如有</span><input value={password} onChange={(event) => setPassword(event.target.value)} type="password" placeholder="分享未设置密码则留空" autoComplete="current-password" /></label><label>保存到<select value={parentID ?? "root"} onChange={(event) => setParentID(event.target.value === "root" ? null : Number(event.target.value))}><option value="root">我的文件（根目录）</option>{folders.map((folder) => <option key={folder.id} value={folder.id}>{folder.name}</option>)}</select></label><p className="field-hint">保存后会创建自己的文件记录，不需要先下载到本机。</p><DialogActions onClose={onClose} submit="保存到我的文件" disabled={!token} /></form></Dialog>;
}

function HelpDialog({ onClose }: { onClose: () => void }) {
  return <Dialog title="使用帮助" onClose={onClose}><div className="help-content"><section><strong>整理文件</strong><p>上传文件后可新建文件夹、移动、重命名或移入回收站。</p></section><section><strong>分享文件</strong><p>在文件详情中创建分享链接；收到链接后可在“分享链接”中保存到自己的文件。</p></section><section><strong>查看目录</strong><p>双击文件夹进入，使用“返回上一级”或顶部路径回到其他目录。</p></section><button type="button" className="primary-button help-confirm" onClick={onClose}>知道了</button></div></Dialog>;
}

function Dialog({ title, onClose, children }: { title: string; onClose: () => void; children: ReactNode }) {
  return <div className="dialog-backdrop" role="presentation" onMouseDown={onClose}><section className="dialog" role="dialog" aria-modal="true" aria-label={title} onMouseDown={(event) => event.stopPropagation()}><header><h2>{title}</h2><button className="icon-button" type="button" aria-label="关闭" onClick={onClose}><X size={18} /></button></header>{children}</section></div>;
}

function DialogActions({ onClose, submit, disabled = false }: { onClose: () => void; submit: string; disabled?: boolean }) { return <div className="dialog-actions"><button type="button" className="text-button" onClick={onClose}>取消</button><button type="submit" className="primary-button" disabled={disabled}>{submit}</button></div>; }
function UploadQueue({ items }: { items: UploadItem[] }) { return <div className="upload-queue">{items.map((item) => <div className="upload-queue-item" key={item.id}><div><span className={item.state === "failed" ? "queue-failed" : "queue-active"}>{item.state === "uploading" ? <LoaderCircle size={15} className="spin" /> : item.state === "finished" ? <ShieldCheck size={15} /> : <X size={15} />}</span><strong>{item.name}</strong></div><span>{item.state === "failed" ? "失败" : item.state === "finished" ? "已完成" : `${item.progress}%`}</span>{item.state !== "failed" && <div className="queue-meter"><i style={{ width: `${item.progress}%` }} /></div>}</div>)}</div>; }
function EmptyFiles({ trash, searching }: { trash: boolean; searching: boolean }) { return <div className="empty-state">{trash ? <Trash2 size={28} /> : <File size={28} />}<p>{searching ? "未找到匹配文件" : trash ? "回收站为空" : "这个目录还没有文件"}</p><span>{searching ? "换一个关键词试试。" : trash ? "删除的文件会显示在这里。" : "拖入文件，或点击右上角上传。"}</span></div>; }
function LoadingRows() { return <div className="loading-rows"><LoaderCircle size={20} className="spin" /><span>正在加载…</span></div>; }
function IconAction({ label, tone, onClick, children }: { label: string; tone?: "danger"; onClick: () => void; children: ReactNode }) { return <button className={`row-icon ${tone === "danger" ? "danger" : ""}`} type="button" title={label} aria-label={label} onClick={onClick}>{children}</button>; }
function FileKindIcon({ file, size = 20 }: { file: UserFile; size?: number }) { const type = file.content_type; if (type.startsWith("image/")) return <FileImage size={size} className="kind-image" />; if (type.startsWith("video/")) return <FileVideo size={size} className="kind-video" />; if (type.includes("zip") || type.includes("compressed") || /\.(zip|rar|7z|tar|gz)$/i.test(file.original_name)) return <FileArchive size={size} className="kind-archive" />; if (type.startsWith("text/") || type.includes("pdf") || type.includes("word") || type.includes("sheet")) return <FileText size={size} className="kind-text" />; return <File size={size} className="kind-file" />; }
function navClass(active: boolean) { return `side-link ${active ? "active" : ""}`; }
function toID(value: string | null) { const id = Number(value); return Number.isInteger(id) && id > 0 ? id : null; }
function readFolderPath(value: string | null, fallback: number | null) {
  const path = (value ?? "").split(",").map(Number).filter((id) => Number.isInteger(id) && id > 0);
  return path.length > 0 ? path : fallback ? [fallback] : [];
}
function readFolderNames(value: string | null, expectedLength: number) {
  const names = (value ?? "").split(",").map((name) => name.trim()).filter(Boolean);
  return names.length === expectedLength ? names : Array.from({ length: expectedLength }, () => "当前文件夹");
}
function formatBytes(bytes: number) { if (!bytes) return "0 B"; const units = ["B", "KB", "MB", "GB", "TB"]; const index = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1); return `${(bytes / 1024 ** index).toFixed(index === 0 ? 0 : bytes / 1024 ** index >= 10 ? 1 : 2)} ${units[index]}`; }
function formatDate(value: string) { const date = new Date(value); return Number.isNaN(date.getTime()) ? "-" : new Intl.DateTimeFormat("zh-CN", { year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" }).format(date); }
function publicShareURL(token: string) { return `${window.location.origin}/share/${encodeURIComponent(token)}`; }
function shareTokenFromReference(value: string) {
  const reference = value.trim();
  if (!reference) return "";

  try {
    const parts = new URL(reference).pathname.split("/").filter(Boolean);
    const shareIndex = parts.lastIndexOf("share");
    if (shareIndex >= 0 && parts[shareIndex + 1]) return decodeURIComponent(parts[shareIndex + 1]);
  } catch {
    // 不是 URL 时按原始令牌处理。
  }

  return /\s/.test(reference) ? "" : reference;
}
function shortType(value: string) { if (!value) return "未知"; if (value.startsWith("image/")) return "图片"; if (value.startsWith("video/")) return "视频"; if (value.startsWith("text/")) return "文本"; if (value.includes("pdf")) return "PDF"; return value.split("/").at(-1)?.toUpperCase() ?? value; }
function availabilityLabel(value: UserFile["availability"]) { return value === "ready" ? "可用" : value === "processing" ? "处理中" : "不可用"; }

function isInlinePreviewable(file: UserFile) {
  return file.availability === "ready" && ["image/jpeg", "image/png", "image/webp", "image/gif"].includes(file.content_type);
}

function isThumbnailSupported(file: UserFile) {
  return file.availability === "ready" && ["image/jpeg", "image/png", "image/webp", "image/gif"].includes(file.content_type);
}
function usagePercent(usage: StorageUsage | undefined) { if (!usage?.quota_bytes) return 0; return Math.min(Math.round((usage.used_bytes / usage.quota_bytes) * 100), 100); }
function messageOf(error: unknown) { return error instanceof Error ? error.message : "发生未知错误"; }
