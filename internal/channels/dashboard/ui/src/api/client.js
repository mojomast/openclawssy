const STORAGE_KEY = "openclawssy.dashboard.bearer";

export class ApiError extends Error {
  constructor({ message, status, code, details, url }) {
    super(message || "API request failed");
    this.name = "ApiError";
    this.status = status || 0;
    this.code = code || "api.error";
    this.details = details;
    this.url = url || "";
  }
}

export function resolveBearerToken(options = {}) {
  const {
    query = window.location.search,
    queryKeys = ["token", "bearer", "bearer_token"],
    storage = window.localStorage,
    storageKey = STORAGE_KEY,
    promptFn = window.prompt,
  } = options;

  const params = new URLSearchParams(query);
  for (const key of queryKeys) {
    const value = (params.get(key) || "").trim();
    if (value) {
      storage.setItem(storageKey, value);
      return value;
    }
  }

  const stored = (storage.getItem(storageKey) || "").trim();
  if (stored) {
    return stored;
  }

  const prompted = (promptFn("Enter dashboard bearer token") || "").trim();
  if (prompted) {
    storage.setItem(storageKey, prompted);
  }
  return prompted;
}

function parseBody(text, contentType) {
  if (!text) {
    return null;
  }
  if ((contentType || "").includes("application/json")) {
    try {
      return JSON.parse(text);
    } catch (_err) {
      return { message: text };
    }
  }
  return text;
}

export function createApiClient(options = {}) {
  const {
    baseUrl = "",
    fetchImpl = window.fetch.bind(window),
    tokenResolver = resolveBearerToken,
  } = options;

  async function request(path, requestOptions = {}) {
    const { skipAuth = false, headers = {}, body, method = "GET" } = requestOptions;
    const allHeaders = new Headers(headers);
    const token = skipAuth ? "" : tokenResolver();
    if (token) {
      allHeaders.set("Authorization", `Bearer ${token}`);
    }
    if (body !== undefined && !allHeaders.has("Content-Type")) {
      allHeaders.set("Content-Type", "application/json");
    }

    const response = await fetchImpl(baseUrl + path, {
      ...requestOptions,
      method,
      headers: allHeaders,
      body: body === undefined || typeof body === "string" ? body : JSON.stringify(body),
    });

    const text = await response.text();
    const data = parseBody(text, response.headers.get("Content-Type"));

    if (!response.ok) {
      const details = typeof data === "object" && data ? data : { message: text };
      throw new ApiError({
        status: response.status,
        code: details?.error?.code || `http.${response.status}`,
        message: details?.error?.message || details?.message || response.statusText,
        details,
        url: path,
      });
    }

    return data;
  }

  return {
    request,
    get: (path, options = {}) => request(path, { ...options, method: "GET" }),
    post: (path, body, options = {}) => request(path, { ...options, method: "POST", body }),
    patch: (path, body, options = {}) => request(path, { ...options, method: "PATCH", body }),
    put: (path, body, options = {}) => request(path, { ...options, method: "PUT", body }),
    delete: (path, options = {}) => request(path, { ...options, method: "DELETE" }),
    resolveBearerToken: () => tokenResolver(),
  };
}
