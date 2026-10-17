import test from "node:test";
import assert from "node:assert/strict";
import { proxyBackend } from "../lib/backend-proxy.ts";

test("runtime proxy preserves HTTPS Origin, CSRF and secure session cookies", async () => {
  const old = globalThis.fetch;
  try {
    globalThis.fetch = async (url, init) => {
      assert.equal(url.pathname, "/api/v1/auth/login");
      assert.equal(init.headers.get("origin"), "https://www.sumobai.com");
      assert.equal(init.headers.get("x-csrf-token"), "test-csrf");
      assert.equal(init.headers.get("x-forwarded-host"), "www.sumobai.com");
      assert.notEqual(init.headers.get("x-forwarded-proto"), "attacker");
      assert.equal(init.redirect, "manual");
      return Response.json(
        { ok: true },
        {
          headers: {
            "Set-Cookie":
              "__Host-els_session=test; Path=/; HttpOnly; Secure; SameSite=Strict",
          },
        },
      );
    };
    const result = await proxyBackend(
      new Request("https://www.sumobai.com/api/v1/auth/login", {
        method: "POST",
        headers: {
          Origin: "https://www.sumobai.com",
          "X-CSRF-Token": "test-csrf",
          "X-Forwarded-Proto": "attacker",
        },
        body: "{}",
      }),
    );
    assert.match(result.headers.get("set-cookie"), /HttpOnly; Secure/);
  } finally {
    globalThis.fetch = old;
  }
});
test("outage returns a safe error without retries; the next request recovers", async () => {
  const old = globalThis.fetch;
  let calls = 0;
  try {
    globalThis.fetch = async () => {
      calls++;
      throw new Error("getaddrinfo ENOTFOUND api:8080");
    };
    const response = await proxyBackend(
      new Request("http://localhost/api/v1/auth/me"),
    );
    assert.equal(response.status, 503);
    assert.equal(calls, 1);
    assert.equal(response.headers.get("retry-after"), "15");
    const body = await response.text();
    assert.match(body, /暂时不可用/);
    assert.doesNotMatch(body, /ENOTFOUND|8080/);
    globalThis.fetch = async () => Response.json({ status: "ok" });
    assert.equal(
      (await proxyBackend(new Request("http://localhost/api/v1/health")))
        .status,
      200,
    );
  } finally {
    globalThis.fetch = old;
  }
});

test('PDF byte ranges and validators survive the authenticated proxy', async()=>{const old=globalThis.fetch;try{globalThis.fetch=async(url,init)=>{assert.equal(init.headers.get('range'),'bytes=0-3');assert.equal(init.headers.get('if-range'),'"immutable-hash"');return new Response('%PDF',{status:206,headers:{'Content-Type':'application/pdf','Content-Range':'bytes 0-3/100','Accept-Ranges':'bytes'}})};const result=await proxyBackend(new Request('http://localhost:3000/api/v1/admin/resources/resource/file',{headers:{Range:'bytes=0-3','If-Range':'"immutable-hash"'}}));assert.equal(result.status,206);assert.equal(result.headers.get('content-range'),'bytes 0-3/100');assert.equal(await result.text(),'%PDF')}finally{globalThis.fetch=old}});
