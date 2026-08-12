import { clearSession, readSession } from "../auth/session";
import type { BackgroundJob, Folder, PublicShareFile, Share, StorageUsage, UploadStatus, UploadTask, UserFile } from "./types";

export class ApiError extends Error {
  constructor(
    message: string,
    public readonly status: number
  ) {
    super(message);
  }
}

// 后端保留稳定的英文错误码文案，界面层在此统一转换为面向用户的中文提示。
function userFacingError(message: string, status: number): string {
  const messages: Record<string, string> = {
    "invalid username or password": "用户名或密码不正确。",
    "username already exists": "该用户名已被使用。",
    "username is required": "请输入用户名。",
    "password is required": "请输入密码。",
    "invalid authorization header": "登录状态无效，请重新登录。",
    "invalid token": "登录状态无效，请重新登录。",
    "file is required": "请选择要上传的文件。",
    "file not found": "文件不存在，或你没有访问权限。",
    "folder not found": "目标文件夹不存在，或你没有访问权限。",
    "folder name is required": "请输入文件夹名称。",
    "folder already exists": "当前目录中已存在同名文件夹。",
    "folder is not empty": "文件夹内仍有内容，无法删除。",
    "storage quota exceeded": "存储空间不足，无法完成上传。",
    "file object not found": "未找到可用于秒传的文件内容。",
    "chunk number is invalid": "分片编号无效，请重新上传文件。",
    "chunk size does not match expected size": "分片大小与服务端预期不一致，请重新上传文件。",
    "chunk content is required": "上传分片内容为空，请重新上传文件。",
    "upload task is not accepting chunks": "该上传任务已结束或不可继续上传，请重新选择文件。",
    "upload chunks are incomplete": "文件分片不完整，请重新上传文件。",
    "upload chunk hash does not match content": "文件分片校验失败，请重新上传文件。",
    "file hash does not match uploaded content": "文件完整性校验失败，请重新上传文件。",
    "file scan is not complete": "文件正在等待安全扫描完成，暂时不可用。",
    "file is infected": "该文件未通过安全扫描，无法访问。",
    "share not found": "分享链接不存在或已失效。",
    "share password is required": "该分享链接需要访问密码。",
    "invalid share password": "分享链接密码不正确。",
    "share has expired": "分享链接已过期。",
    "share download limit reached": "该分享链接的下载次数已用完。"
	,"shared file is not available": "该分享文件暂时不可用，请等待安全扫描完成。"
  };

  if (messages[message]) return messages[message];
  if (status === 401) return "登录已失效，请重新登录。";
  if (status === 403) return "你没有执行此操作的权限。";
  if (status === 404) return "请求的资源不存在，或你没有访问权限。";
  if (status === 409) return "当前操作与已有数据冲突，请刷新后重试。";
  if (status === 423) return "文件暂时不可用，请等待安全扫描完成。";
  if (status >= 500) return "服务暂时异常，请稍后重试。";
  return "请求未能完成，请检查输入后重试。";
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const session = readSession();
  const headers = new Headers(init.headers);

  if (session?.token) headers.set("Authorization", `Bearer ${session.token}`);
  if (init.body && !(init.body instanceof FormData) && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }

  const response = await fetch(path, { ...init, headers });
  if (response.status === 401 && session) {
    clearSession();
    window.dispatchEvent(new Event("cloudbox:unauthorized"));
  }
  if (!response.ok) {
    const body = (await response.json().catch(() => null)) as { error?: string } | null;
    throw new ApiError(userFacingError(body?.error ?? "", response.status), response.status);
  }
  if (response.status === 204) return undefined as T;
  return (await response.json()) as T;
}

async function publicRequest<T>(path: string, password: string): Promise<T> {
	const headers = password ? { "X-Share-Password": password } : undefined;
	const response = await fetch(path, { headers });
	if (!response.ok) {
		const body = (await response.json().catch(() => null)) as { error?: string } | null;
		throw new ApiError(userFacingError(body?.error ?? "", response.status), response.status);
	}
	return (await response.json()) as T;
}

export const api = {
  login: (username: string, password: string) =>
    request<{ token: string }>("/api/auth/login", {
      method: "POST",
      body: JSON.stringify({ username, password })
    }),
  register: (username: string, password: string) =>
    request<{ user: { id: number; username: string } }>("/api/auth/register", {
      method: "POST",
      body: JSON.stringify({ username, password })
    }),
  listFiles: (parentId: number | null) =>
    request<{ files: UserFile[] }>(parentId ? `/api/files?parent_id=${parentId}` : "/api/files"),
  listTrash: () => request<{ files: UserFile[] }>("/api/files/trash"),
  listFolders: (parentId: number | null) =>
    request<{ folders: Folder[] }>(parentId ? `/api/folders?parent_id=${parentId}` : "/api/folders"),
  storage: async () => {
    const { storage } = await request<{ storage: StorageUsage }>("/api/storage");
    return storage;
  },
  upload: (file: File, parentId: number | null) => {
    const form = new FormData();
    form.append("file", file);
    if (parentId) form.append("parent_id", String(parentId));
    return request<{ file: UserFile }>("/api/files", { method: "POST", body: form });
  },
  createFolder: (name: string, parentId: number | null) =>
    request<{ folder: Folder }>("/api/folders", {
      method: "POST",
      body: JSON.stringify({ name, parent_id: parentId })
    }),
  renameFile: (id: number, originalName: string) =>
    request<{ file: UserFile }>(`/api/files/${id}/rename`, {
      method: "PATCH",
      body: JSON.stringify({ original_name: originalName })
    }),
  moveFile: (id: number, parentId: number | null) =>
    request<{ file: UserFile }>(`/api/files/${id}/move`, {
      method: "PATCH",
      body: JSON.stringify({ parent_id: parentId })
    }),
  renameFolder: (id: number, name: string) =>
    request<{ folder: Folder }>(`/api/folders/${id}/rename`, {
      method: "PATCH",
      body: JSON.stringify({ name })
    }),
  moveFolder: (id: number, parentId: number | null) =>
    request<{ folder: Folder }>(`/api/folders/${id}/move`, {
      method: "PATCH",
      body: JSON.stringify({ parent_id: parentId })
    }),
  deleteFolder: (id: number) => request<void>(`/api/folders/${id}`, { method: "DELETE" }),
	deleteFile: (id: number, keepShares = false) =>
		request<void>(`/api/files/${id}${keepShares ? "?keep_shares=true" : ""}`, { method: "DELETE" }),
  restoreFile: (id: number) => request<void>(`/api/files/${id}/restore`, { method: "POST" }),
  permanentlyDeleteFile: (id: number) => request<void>(`/api/files/${id}/permanent`, { method: "DELETE" }),
  listShares: () => request<{ shares: Share[] }>("/api/shares"),
  createShare: (fileId: number, input: { password?: string; expires_at?: string; max_downloads?: number }) =>
    request<{ share: Share }>(`/api/files/${fileId}/shares`, {
      method: "POST",
      body: JSON.stringify(input)
    }),
	revokeShare: (token: string) => request<void>(`/api/shares/${token}`, { method: "DELETE" }),
	publicShareInfo: (token: string, password: string) =>
		publicRequest<{ file: PublicShareFile }>(`/api/shares/${encodeURIComponent(token)}`, password),
  saveSharedFile: (token: string, password: string, parentId: number | null) =>
    request<{ file: UserFile }>(`/api/shares/${encodeURIComponent(token)}/save`, {
      method: "POST",
      body: JSON.stringify({ password, parent_id: parentId })
    }),
  enqueueVerification: (id: number) => request<{ job: BackgroundJob }>(`/api/files/${id}/verify`, { method: "POST" }),
  getJob: (id: string) => request<{ job: BackgroundJob }>(`/api/jobs/${id}`),
  initUpload: (file: File, parentId: number | null, chunkSize: number) =>
    request<{ upload: UploadTask }>("/api/uploads/init", {
      method: "POST",
      body: JSON.stringify({
        parent_id: parentId,
        original_name: file.name,
        content_type: file.type || "application/octet-stream",
        file_size: file.size,
        chunk_size: chunkSize
      })
    }),
  async uploadChunk(uploadID: string, number: number, chunk: Blob) {
    await request<{ chunk: unknown }>(`/api/uploads/${uploadID}/chunks/${number}`, {
      method: "PUT",
      headers: { "Content-Type": "application/octet-stream" },
      body: chunk
    });
  },
	completeUpload: (uploadID: string) =>
		request<{ file: UserFile }>(`/api/uploads/${uploadID}/complete`, { method: "POST" }),
	getUploadStatus: (uploadID: string) => request<UploadStatus>(`/api/uploads/${uploadID}`),
	listUploads: () => request<{ uploads: UploadTask[] }>("/api/uploads"),
	cancelUpload: (uploadID: string) => request<void>(`/api/uploads/${uploadID}`, { method: "DELETE" }),
	async uploadFile(
		file: File,
		parentId: number | null,
		onProgress?: (value: number) => void,
		onTask?: (upload: UploadTask) => void,
		resumeUploadID?: string,
	): Promise<{ file: UserFile }> {
    const chunkSize = 5 * 1024 * 1024;
    if (file.size <= chunkSize) {
      const result = await this.upload(file, parentId);
      onProgress?.(100);
      return result;
    }

		let upload: UploadTask;
		let uploadedNumbers = new Set<number>();
		if (resumeUploadID) {
			const status = await this.getUploadStatus(resumeUploadID);
			upload = status.upload;
			if (
				upload.status !== "uploading" ||
				upload.original_name !== file.name ||
				upload.file_size !== file.size ||
				upload.parent_id !== parentId
			) {
				throw new ApiError("上传任务与当前文件不匹配，请重新开始上传。", 409);
			}
			uploadedNumbers = new Set(status.chunks.map((chunk) => chunk.number));
		} else {
			const initialized = await this.initUpload(file, parentId, chunkSize);
			upload = initialized.upload;
		}

		onTask?.(upload);
		onProgress?.(Math.round((uploadedNumbers.size / upload.total_chunks) * 100));
		// 服务端分片编号从 0 开始，已确认的分片不会重复传输。
		for (let number = 0; number < upload.total_chunks; number += 1) {
			if (uploadedNumbers.has(number)) continue;
			const start = number * upload.chunk_size;
			const end = Math.min(start + upload.chunk_size, file.size);
			await this.uploadChunk(upload.id, number, file.slice(start, end));
			uploadedNumbers.add(number);
			onProgress?.(Math.round((uploadedNumbers.size / upload.total_chunks) * 100));
		}
		return await this.completeUpload(upload.id);
	},
  async thumbnail(id: number): Promise<Blob> {
    const session = readSession();
    const response = await fetch(`/api/files/${id}/thumbnail`, {
      headers: session?.token ? { Authorization: `Bearer ${session.token}` } : undefined
    });
    if (!response.ok) {
      const body = (await response.json().catch(() => null)) as { error?: string } | null;
      throw new ApiError(userFacingError(body?.error ?? "", response.status), response.status);
    }
    return response.blob();
  },
  async download(id: number) {
    const session = readSession();
    const response = await fetch(`/api/files/${id}/download`, {
      headers: session?.token ? { Authorization: `Bearer ${session.token}` } : undefined
    });
    if (!response.ok) {
      const body = (await response.json().catch(() => null)) as { error?: string } | null;
      throw new ApiError(userFacingError(body?.error ?? "", response.status), response.status);
    }
    const blob = await response.blob();
    const disposition = response.headers.get("Content-Disposition") ?? "";
    const name = disposition.match(/filename="?([^";]+)"?/)?.[1] ?? "download";
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = name;
    anchor.click();
    URL.revokeObjectURL(url);
  },
	async publicSharePreview(token: string, password: string): Promise<Blob> {
		const headers = password ? { "X-Share-Password": password } : undefined;
		const response = await fetch(`/api/shares/${encodeURIComponent(token)}/preview`, { headers });
		if (!response.ok) {
			const body = (await response.json().catch(() => null)) as { error?: string } | null;
			throw new ApiError(userFacingError(body?.error ?? "", response.status), response.status);
		}
		return response.blob();
	},
  async downloadShared(token: string, password: string) {
		const session = readSession();
		const headers = new Headers();
		if (session?.token) headers.set("Authorization", `Bearer ${session.token}`);
		if (password) headers.set("X-Share-Password", password);
		const response = await fetch(`/api/shares/${encodeURIComponent(token)}/download`, { headers });
		if (!response.ok) {
			const body = (await response.json().catch(() => null)) as { error?: string } | null;
			throw new ApiError(userFacingError(body?.error ?? "", response.status), response.status);
		}

		const blob = await response.blob();
    const disposition = response.headers.get("Content-Disposition") ?? "";
    const name = disposition.match(/filename="?([^";]+)"?/)?.[1] ?? "download";
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = name;
    anchor.click();
    URL.revokeObjectURL(url);
  }
};
