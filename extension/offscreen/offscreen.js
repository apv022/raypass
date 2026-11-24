// Chrome's offscreen document (reason CLIPBOARD): a service worker has no document to clear the
// clipboard with, so the background opens this page, asks it to clear, and closes it again.
// It only ever writes an empty string; it never reads the clipboard.
'use strict';

chrome.runtime.onMessage.addListener((m, sender, reply) => {
  if (sender.id !== chrome.runtime.id || !m || m.type !== 'offscreen.clear') return false;
  const onCopy = (e) => { e.clipboardData.setData('text/plain', ''); e.preventDefault(); };
  document.addEventListener('copy', onCopy, { once: true });
  let ok = false;
  try { ok = document.execCommand('copy'); } catch { ok = false; }
  document.removeEventListener('copy', onCopy);
  reply({ ok });
  return false;
});
