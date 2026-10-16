import katex from "katex";
export function mathSegments(text: string) {
  const pattern =
    /\$\$([\s\S]+?)\$\$|\\\[([\s\S]+?)\\\]|\\\(([\s\S]+?)\\\)|(?<!\\)\$([^$\n]+?)\$/g;
  const result: { text: string; html?: string; display?: boolean }[] = [];
  let end = 0;
  for (const match of text.matchAll(pattern)) {
    if (match.index! > end) result.push({ text: text.slice(end, match.index) });
    const formula = match[1] ?? match[2] ?? match[3] ?? match[4];
    const display = match[1] !== undefined || match[2] !== undefined;
    if (formula.length > 4096) result.push({ text: match[0] });
    else
      result.push({
        text: match[0],
        html: katex.renderToString(formula, {
          displayMode: display,
          throwOnError: false,
          trust: false,
          strict: "ignore",
          maxSize: 12,
          maxExpand: 1000,
          output: "htmlAndMathml",
          macros: {},
        }),
        display,
      });
    end = match.index! + match[0].length;
  }
  if (end < text.length) result.push({ text: text.slice(end) });
  return result;
}
