const VIEWER_STORAGE_KEY = "rml_viewer_id";

export function getOrCreateViewerId(): string {
  const existing = window.localStorage.getItem(VIEWER_STORAGE_KEY);
  if (existing) {
    return existing;
  }

  const nextId = crypto.randomUUID();
  window.localStorage.setItem(VIEWER_STORAGE_KEY, nextId);
  return nextId;
}
