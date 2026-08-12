import type { UploadTask } from "../api/types";

const resumeKey = "cloudbox.upload-resume.v1";

export type UploadResumePoint = {
	fingerprint: string;
	uploadID: string;
	parentID: number | null;
	createdAt: string;
};

export function fileFingerprint(file: File): string {
	return [file.name, file.size, file.lastModified].join(":");
}

export function readUploadResumePoints(): UploadResumePoint[] {
	try {
		const value = localStorage.getItem(resumeKey);
		const points = value ? JSON.parse(value) : [];
		return Array.isArray(points) ? points : [];
	} catch {
		localStorage.removeItem(resumeKey);
		return [];
	}
}

function writeUploadResumePoints(points: UploadResumePoint[]) {
	localStorage.setItem(resumeKey, JSON.stringify(points));
}

export function findUploadResumePoint(file: File, parentID: number | null): UploadResumePoint | undefined {
	const fingerprint = fileFingerprint(file);
	return readUploadResumePoints().find((point) => point.fingerprint === fingerprint && point.parentID === parentID);
}

export function saveUploadResumePoint(file: File, parentID: number | null, task: UploadTask) {
	const point: UploadResumePoint = {
		fingerprint: fileFingerprint(file),
		uploadID: task.id,
		parentID,
		createdAt: new Date().toISOString()
	};
	const remaining = readUploadResumePoints().filter((current) => current.fingerprint !== point.fingerprint || current.parentID !== parentID);
	writeUploadResumePoints([...remaining, point]);
}

export function removeUploadResumePoint(file: File, parentID: number | null) {
	const fingerprint = fileFingerprint(file);
	writeUploadResumePoints(readUploadResumePoints().filter((point) => point.fingerprint !== fingerprint || point.parentID !== parentID));
}

export function taskMatchesResumePoint(task: UploadTask, file: File, parentID: number | null): boolean {
	return task.status === "uploading" && task.original_name === file.name && task.file_size === file.size && task.parent_id === parentID;
}
