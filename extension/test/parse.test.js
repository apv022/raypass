// The scripts that need the extension's APIs to run (background, content script, popup,
// options) are not run here, but each must at least compile: a syntax error in one of them
// would otherwise only show up once the extension is loaded in a browser.
import { test, assert } from './testing.js';

for (const path of ['../background.js', '../content.js', '../popup/popup.js', '../options/options.js']) {
  test(`parse: ${path.slice(3)} compiles`, async () => {
    const res = await fetch(path);
    assert.ok(res.ok, `fetch ${path}`);
    // Static imports are only valid in a module; the rest compiles as a function body.
    const src = (await res.text()).replace(/^import\s+['"][^'"]+['"];$/gm, '');
    new Function(src); // throws SyntaxError, never runs the code
  });
}
