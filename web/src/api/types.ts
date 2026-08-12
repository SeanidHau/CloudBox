export interface UserFile {
  id: number;
  user_id: number;
  parent_id: number | null;
  original_name: string;
  storage_path: string;
  size: number;
  content_type: string;
  status: "active" | "deleted";
  availability: "ready" | "processing" | "unavailable";
  created_at: string;
}

export interface Folder {
  id: number;
  user_id: number;
  parent_id: number | null;
  name: string;
  size: number;
  created_at: string;
  updated_at: string;
}

export interface StorageUsage {
  used_bytes: number;
  quota_bytes: number;
  available_bytes: number;
}

export interface Share {
	token: string;
	user_file_id: number;
  expires_at: string | null;
  max_downloads: number | null;
	download_count: number;
	created_at: string;
	original_name: string;
	size: number;
	content_type: string;
	has_preview: boolean;
}

export interface PublicShareFile {
	original_name: string;
	size: number;
	content_type: string;
	has_preview: boolean;
}

export interface BackgroundJob {
  id: string;
  job_type: string;
  status: "queued" | "running" | "succeeded" | "failed";
  attempts: number;
  max_attempts: number;
  run_at: string;
  created_at: string;
  updated_at: string;
}

export interface UploadTask {
  id: string;
  user_id: number;
  parent_id: number | null;
  original_name: string;
  content_type: string;
  file_size: number;
  chunk_size: number;
  total_chunks: number;
  status: "uploading" | "completing" | "completed" | "failed";
  created_at: string;
  updated_at: string;
}

export interface UploadChunk {
	upload_id: string;
	number: number;
	size: number;
	created_at: string;
}

export interface UploadStatus {
	upload: UploadTask;
	chunks: UploadChunk[];
}

export interface Session {
  token: string;
  username: string;
  role?: "user" | "admin";
  must_change_password?: boolean;
}

export interface AccountUser {
  id: number;
  username: string;
  role: "user" | "admin";
  status: "active" | "disabled";
  storage_quota_bytes: number;
  must_change_password: boolean;
  created_at: string;
}

export interface Invitation {
  id: number;
  created_by_user_id: number;
  expires_at: string;
  used_by_user_id?: number;
  used_at?: string;
  revoked_at?: string;
  created_at: string;
}

export interface CreatedInvitation {
  invitation: Invitation;
  code: string;
}
