// The options page: how to install the native host (with this browser's own extension ID in the
// command) and a connection test that walks the whole path: extension, host, sol, Raypass.
const $ = (id) => document.getElementById(id);
const firefox = chrome.runtime.getURL('').startsWith('moz-extension:');

$('cmd').textContent = firefox
  ? 'raypass-native install'
  : `raypass-native install --extension-id ${chrome.runtime.id}`;

function setStatus(kind, text) {
  $('dot').className = 'dot ' + kind;
  $('status-text').textContent = text;
}

async function test() {
  setStatus('', 'Testing…');
  $('facts').classList.add('hidden');
  let r;
  try {
    r = await chrome.runtime.sendMessage({ type: 'ext.ping' });
  } catch (e) {
    setStatus('bad', String(e?.message || e));
    return;
  }
  if (!r?.ok) {
    const missing = r?.error?.code === 'host_missing';
    setStatus('bad', missing ? 'The native host is not installed for this browser (see below).' : r?.error?.message || 'failed');
    return;
  }
  $('facts').classList.remove('hidden');
  $('f-host').textContent = `${r.host.host} ${r.host.version}`;
  $('f-sol').textContent = r.host.sol || 'not found';
  if (r.error) {
    $('f-vault').textContent = r.error.message;
    setStatus('warn', r.error.code === 'unavailable' ? 'Host installed, but Sol is not running.' : 'Host installed, Raypass answered with an error.');
    return;
  }
  const st = r.status || {};
  $('f-vault').textContent = !st.initialized ? 'not set up yet' : st.locked ? `locked · ${st.items ?? '?'} items` : `unlocked · ${st.items ?? '?'} items`;
  setStatus('ok', 'Connected to Raypass.');
}

$('test').addEventListener('click', test);
$('web').addEventListener('click', () => chrome.tabs.create({ url: 'http://raypass.localhost:7777/' }));
