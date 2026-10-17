// Never intercept API requests, authenticated documents, audio or user answers.
const PUBLIC_CACHE = "learning-public-v1";
self.addEventListener("install", (event) => {
  event.waitUntil(
    caches
      .open(PUBLIC_CACHE)
      .then((cache) => cache.addAll(["/offline.html", "/icon.svg"])),
  );
  self.skipWaiting();
});
self.addEventListener("activate", (event) => {
  event.waitUntil(
    Promise.all([
      self.clients.claim(),
      caches
        .keys()
        .then((names) =>
          Promise.all(
            names
              .filter(
                (n) => n.startsWith("learning-public-") && n !== PUBLIC_CACHE,
              )
              .map((n) => caches.delete(n)),
          ),
        ),
    ]),
  );
});
self.addEventListener("fetch", (event) => {
  const url = new URL(event.request.url);
  if (event.request.method !== "GET" || url.origin !== self.location.origin)
    return;
  if (["/offline.html", "/icon.svg"].includes(url.pathname)) {
    event.respondWith(
      caches
        .match(event.request)
        .then((cached) => cached || fetch(event.request)),
    );
    return;
  }
  if (event.request.mode === "navigate") {
    event.respondWith(
      fetch(event.request).catch(() => caches.match("/offline.html")),
    );
  }
});
