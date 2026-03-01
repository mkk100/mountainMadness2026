const VIEWER_STORAGE_KEY = "rml_viewer_id";
const COOKIE_MAX_AGE_SECONDS = 60 * 60 * 24 * 365;

export function getOrCreateViewerId(): string {
  const existingLocal = window.localStorage.getItem(VIEWER_STORAGE_KEY);
  const existingCookie = getCookie(VIEWER_STORAGE_KEY);

  if (existingLocal) {
    if (!existingCookie) {
      setCookie(VIEWER_STORAGE_KEY, existingLocal);
    }
    return existingLocal;
  }

  if (existingCookie) {
    window.localStorage.setItem(VIEWER_STORAGE_KEY, existingCookie);
    return existingCookie;
  }

  const nextId = crypto.randomUUID();
  window.localStorage.setItem(VIEWER_STORAGE_KEY, nextId);
  setCookie(VIEWER_STORAGE_KEY, nextId);
  return nextId;
}

export function markDecisionResponded(slug: string): void {
  const key = responseMarkerKey(slug);
  window.localStorage.setItem(key, "1");
  setCookie(key, "1");
}

export function hasDecisionResponded(slug: string): boolean {
  const key = responseMarkerKey(slug);
  if (window.localStorage.getItem(key) === "1") {
    return true;
  }

  return getCookie(key) === "1";
}

function responseMarkerKey(slug: string): string {
  return `rml_responded_${slug.replace(/[^a-zA-Z0-9_-]/g, "_")}`;
}

function setCookie(name: string, value: string): void {
  document.cookie = `${name}=${encodeURIComponent(value)}; Max-Age=${COOKIE_MAX_AGE_SECONDS}; Path=/; SameSite=Lax`;
}

function getCookie(name: string): string | null {
  const prefix = `${name}=`;
  const parts = document.cookie.split(";").map((part) => part.trim());
  for (const part of parts) {
    if (part.startsWith(prefix)) {
      return decodeURIComponent(part.slice(prefix.length));
    }
  }
  return null;
}
