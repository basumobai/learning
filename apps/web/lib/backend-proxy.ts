// Runtime proxy: preserve browser Origin and cookies, and contain infrastructure errors.
export async function proxyBackend(request: Request): Promise<Response> {
  const url = new URL(request.url);
  const target = new URL(
    process.env.API_INTERNAL_URL || "http://localhost:8080",
  );
  target.pathname = url.pathname;
  target.search = url.search;
  const headers = new Headers();
  for (const name of [
    "content-type",
    "cookie",
    "origin",
    "referer",
    "sec-fetch-site",
    "x-els-client",
    "x-csrf-token",
  ]) {
    const value = request.headers.get(name);
    if (value) headers.set(name, value);
  }
  // Incoming forwarded headers are never an authentication or CSRF trust source.
  headers.set("x-forwarded-host", request.headers.get("host") || url.host);
  headers.set("x-forwarded-proto", url.protocol.slice(0, -1));
  try {
    const response = await fetch(target, {
      method: request.method,
      headers,
      body: request.body,
      duplex: "half",
      redirect: "manual",
      cache: "no-store",
      signal: AbortSignal.timeout(150_000),
    } as RequestInit);
    const safe = new Headers(response.headers);
    for (const name of [
      "connection",
      "transfer-encoding",
      "content-encoding",
      "content-length",
    ])
      safe.delete(name);
    safe.set("cache-control", "no-store");
    return new Response(response.body, {
      status: response.status,
      headers: safe,
    });
  } catch {
    return Response.json(
      {
        error: "学习服务暂时不可用，请稍后点击重试。",
        code: "SERVICE_UNAVAILABLE",
      },
      {
        status: 503,
        headers: { "Retry-After": "15", "Cache-Control": "no-store" },
      },
    );
  }
}
