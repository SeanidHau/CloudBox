import type { Session } from "../api/types";

const sessionKey = "cloudbox.session";

export function readSession(): Session | null {
  const value = sessionStorage.getItem(sessionKey);
  if (!value) return null;

  try {
    return JSON.parse(value) as Session;
  } catch {
    sessionStorage.removeItem(sessionKey);
    return null;
  }
}

export function writeSession(session: Session) {
  sessionStorage.setItem(sessionKey, JSON.stringify(session));
}

export function clearSession() {
  sessionStorage.removeItem(sessionKey);
}
