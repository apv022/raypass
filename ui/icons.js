// Raypass glyphs the kit lacks, drawn like the kit's (24px grid, 1.75px round strokes, no fills).
// icon(name) gives the markup for html``: ours as an inline <svg class="ico">, anything else as
// the kit's <sol-icon>. Paths are constants written here, never data.
import { raw, html } from '/sol.js';

/** Our glyphs: name → SVG path "d" strings. */
export const PATHS = {
  // a key whose bow is a small sun: the Raypass mark
  raypass: ['M8.5 8.8a3.2 3.2 0 100 6.4 3.2 3.2 0 000-6.4z', 'M11.7 12H21', 'M17.6 12v2.8', 'M20.4 12v2.2', 'M8.5 7V5.4', 'M8.5 17v1.6', 'M3.5 12H1.9', 'M5 8.5l-1.1-1.1', 'M5 15.5l-1.1 1.1', 'M12 8.5l1.1-1.1', 'M12 15.5l1.1 1.1'],
  key: ['M8 9.2a3.8 3.8 0 100 7.6 3.8 3.8 0 000-7.6z', 'M11.6 11.6l7.9-7.1', 'M16.6 7.1l2.4 2.4', 'M14.4 9.1l1.9 1.9'],
  note: ['M7 3.5h10A1.5 1.5 0 0118.5 5v14a1.5 1.5 0 01-1.5 1.5H7A1.5 1.5 0 015.5 19V5A1.5 1.5 0 017 3.5z', 'M9 8h6', 'M9 11.5h6', 'M9 15h3.5'],
  card: ['M4.5 5.5h15A1.5 1.5 0 0121 7v10a1.5 1.5 0 01-1.5 1.5h-15A1.5 1.5 0 013 17V7a1.5 1.5 0 011.5-1.5z', 'M3 9.5h18', 'M6.5 14.5h4'],
  identity: ['M4.5 5h15A1.5 1.5 0 0121 6.5v11a1.5 1.5 0 01-1.5 1.5h-15A1.5 1.5 0 013 17.5v-11A1.5 1.5 0 014.5 5z', 'M8.5 8.8a2 2 0 100 4 2 2 0 000-4z', 'M5.6 16.2c.5-1.7 1.6-2.5 2.9-2.5s2.4.8 2.9 2.5', 'M14 10h4', 'M14 13.5h3'],
  wallet: ['M5 6.5h12.5A1.5 1.5 0 0119 8v1.5', 'M4 7.5v10A1.5 1.5 0 005.5 19h13a1.5 1.5 0 001.5-1.5v-7A1.5 1.5 0 0018.5 9H5.5A1.5 1.5 0 014 7.5 1.5 1.5 0 015.5 6', 'M16 13.8h.01', 'M15.5 5.2L13 4l-5 2.5'],
  shield: ['M12 3.5l7 2.6v5.4c0 4.4-2.9 7.6-7 9-4.1-1.4-7-4.6-7-9V6.1z', 'M9 12.2l2.2 2.2 4-4.3'],
  dice: ['M6.5 4h11A2.5 2.5 0 0120 6.5v11a2.5 2.5 0 01-2.5 2.5h-11A2.5 2.5 0 014 17.5v-11A2.5 2.5 0 016.5 4z', 'M8.5 8.5h.01', 'M15.5 8.5h.01', 'M12 12h.01', 'M8.5 15.5h.01', 'M15.5 15.5h.01'],
  'eye-off': ['M4 4l16 16', 'M9.9 6.1A8.6 8.6 0 0112 5.8c6 0 9.2 6.2 9.2 6.2a15 15 0 01-2.8 3.5', 'M6.5 7.6A15.4 15.4 0 002.8 12S6 18.2 12 18.2a8.3 8.3 0 004.3-1.2', 'M10 10a2.8 2.8 0 004 4'],
  open: ['M13.5 4.5H19.5V10.5', 'M19.5 4.5l-8.5 8.5', 'M17.5 14v4a1.5 1.5 0 01-1.5 1.5H6A1.5 1.5 0 014.5 18V8A1.5 1.5 0 016 6.5h4'],
  restore: ['M4.5 9.5h10a5 5 0 010 10H8', 'M8 5.5l-3.5 4 3.5 4'],
  history: ['M4.8 12a7.2 7.2 0 102.1-5.1', 'M4.5 4.5v3.3h3.3', 'M12 8v4.2l2.8 1.8'],
  timer: ['M12 6.5a7 7 0 100 14 7 7 0 000-14z', 'M12 10v3.5', 'M10 3h4', 'M18 6.5l1.3-1.3'],
  tray: ['M4.5 13l2-7a1.5 1.5 0 011.4-1h8.2a1.5 1.5 0 011.4 1l2 7', 'M4.5 13v4.5A1.5 1.5 0 006 19h12a1.5 1.5 0 001.5-1.5V13h-4.2a3.3 3.3 0 01-6.6 0z'],
  unlock: ['M7 10.5h10A1.5 1.5 0 0118.5 12v6.5A1.5 1.5 0 0117 20H7a1.5 1.5 0 01-1.5-1.5V12A1.5 1.5 0 017 10.5z', 'M8.5 10.5V8a3.5 3.5 0 016.8-1.2'],
  sliders: ['M4 7h9', 'M17 7h3', 'M15 5v4', 'M4 17h3', 'M11 17h9', 'M9 15v4'],
  fields: ['M4 7h16', 'M4 12h10', 'M4 17h13'],
};

/** Inline SVG markup for one of our glyphs (safe: built from the constants above only). */
export const svg = (name, size = 18, cls = '') => raw(`<svg class="ico${cls ? ' ' + cls : ''}" viewBox="0 0 24 24" width="${size}" height="${size}" aria-hidden="true">${PATHS[name].map((d) => `<path d="${d}"/>`).join('')}</svg>`);

/** An icon by name: ours, else the kit's <sol-icon>. */
export const icon = (name, size) => (PATHS[name] ? svg(name, size) : html`<sol-icon name="${name}"></sol-icon>`);
