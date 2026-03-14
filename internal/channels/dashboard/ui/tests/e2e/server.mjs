import { createServer } from "node:http";
import { readFile } from "node:fs/promises";
import path from "node:path";

const port = Number(process.env.PORT || 4173);
const root = process.cwd();
const distRoot = path.join(root, "dist");

function contentType(filePath) {
  const ext = path.extname(filePath).toLowerCase();
  if (ext === ".html") return "text/html; charset=utf-8";
  if (ext === ".css") return "text/css; charset=utf-8";
  if (ext === ".js") return "text/javascript; charset=utf-8";
  if (ext === ".json") return "application/json; charset=utf-8";
  if (ext === ".svg") return "image/svg+xml";
  return "application/octet-stream";
}

function safePath(base, relativePath) {
  const target = path.resolve(base, relativePath);
  const normalizedBase = base.endsWith(path.sep) ? base : `${base}${path.sep}`;
  if (target === base || target.startsWith(normalizedBase)) {
    return target;
  }
  return "";
}

async function serveFirstExisting(res, candidates) {
  for (const filePath of candidates) {
    try {
      const body = await readFile(filePath);
      res.writeHead(200, { "Content-Type": contentType(filePath) });
      res.end(body);
      return;
    } catch (_error) {
      // try next candidate
    }
  }

  res.writeHead(404, { "Content-Type": "text/plain; charset=utf-8" });
  res.end("not found");
}

createServer(async (req, res) => {
  const method = req.method || "GET";
  if (method !== "GET" && method !== "HEAD") {
    res.writeHead(405, { "Content-Type": "text/plain; charset=utf-8" });
    res.end("method not allowed");
    return;
  }

  const url = new URL(req.url || "/", `http://127.0.0.1:${port}`);
  const pathname = decodeURIComponent(url.pathname);

  if (pathname === "/dashboard" || pathname === "/dashboard/") {
    await serveFirstExisting(res, [path.join(distRoot, "index.html"), path.join(root, "index.html")]);
    return;
  }
  if (pathname.startsWith("/dashboard/static/")) {
    const relative = pathname.slice("/dashboard/static/".length);
    const distFile = safePath(distRoot, relative);
    const rootFile = safePath(root, relative);
    if (!distFile || !rootFile) {
      res.writeHead(400, { "Content-Type": "text/plain; charset=utf-8" });
      res.end("bad path");
      return;
    }
    await serveFirstExisting(res, [distFile, rootFile]);
    return;
  }

  res.writeHead(404, { "Content-Type": "text/plain; charset=utf-8" });
  res.end("not found");
}).listen(port, "127.0.0.1", () => {
  // eslint-disable-next-line no-console
  console.log(`dashboard ui e2e server running on http://127.0.0.1:${port}`);
});
