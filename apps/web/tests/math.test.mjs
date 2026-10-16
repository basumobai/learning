import test from "node:test";
import assert from "node:assert/strict";
import { mathSegments } from "../lib/math.ts";
test("formulas render with accessible MathML and text is kept outside math", () => {
  const r = mathSegments("由 $F=ma$ 得 $$a=\\frac{F}{m}$$");
  assert.equal(r[0].text, "由 ");
  assert.ok(r[1].html.includes("<math"));
  assert.ok(r.at(-1).display);
});
test("untrusted math cannot create links or external image loads", () => {
  const r = mathSegments(
    "$\\href{javascript:alert(1)}{x}$ and $\\includegraphics{https://evil.test/image}$",
  );
  assert.ok(
    !r.some((s) => /<a\s|<img\s|href="javascript:/i.test(s.html || "")),
  );
});
