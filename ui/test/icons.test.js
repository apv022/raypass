// Checks that the UI's glyphs are well-formed SVG paths and that icon() falls back to the kit.
import { test, assert } from '/kit/testing.js';
import { PATHS, icon, svg } from '../icons.js';

test('every glyph path parses and stays on the 24px grid', () => {
  const ns = 'http://www.w3.org/2000/svg';
  const s = document.createElementNS(ns, 'svg');
  s.setAttribute('viewBox', '0 0 24 24');
  document.body.append(s);
  for (const [name, ds] of Object.entries(PATHS)) {
    for (const d of ds) {
      const p = document.createElementNS(ns, 'path');
      p.setAttribute('d', d);
      s.append(p);
      const b = p.getBBox();
      assert.ok(p.getTotalLength() > 0 || /h\.01/.test(d), `${name}: empty path`);
      assert.ok(b.x >= 0 && b.y >= 0 && b.x + b.width <= 24.01 && b.y + b.height <= 24.01, `${name}: off grid`);
    }
  }
  s.remove();
});

test('icon() uses ours or the kit', () => {
  assert.ok(String(icon('key')).startsWith('<svg class="ico"'));
  assert.equal(String(icon('trash')), '<sol-icon name="trash"></sol-icon>');
  assert.ok(String(svg('card', 24, 'big')).includes('class="ico big"'));
});
