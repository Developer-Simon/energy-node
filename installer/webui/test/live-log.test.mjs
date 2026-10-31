import { test } from 'node:test';
import assert from 'node:assert/strict';
import { loadScripts } from './helpers/load.mjs';
import { installEventSource } from './helpers/eventsource.mjs';

const plain = (value) => JSON.parse(JSON.stringify(value));
const DASH = 'energy-node-dashboard.service';
const UPD = 'energy-node-updater.service';

function setup() {
  const dom = loadScripts(['api.js', 'live-log.js']);
  const window = dom.window;
  window.Api.configure({ basePath: '', token: 'tok' });
  return { window, LiveLog: window.LiveLog };
}

function scrub(known, text) {
  const { LiveLog } = setup();
  return LiveLog.scrubber(known).text(text);
}

test('buffer keeps 2000 lines per unit', () => {
  const { LiveLog } = setup();
  const buf = LiveLog.buffer();
  for (let i = 1; i <= 2005; i += 1) {
    buf.push({ unit: DASH, at: 0, msg: `line ${i}` });
  }
  const lines = buf.lines(DASH);
  assert.equal(lines.length, 2000);
  assert.equal(lines[0].msg, 'line 6');
  assert.equal(buf.total(DASH), 2005);
  assert.deepEqual(plain(buf.lines(UPD)), []);
});

test('scrub masks addresses and keeps loopback', () => {
  assert.equal(
    scrub({}, 'connect 192.168.1.20:1883 and 127.0.0.1:1883, again 192.168.1.20, peer 10.0.0.5'),
    'connect <ip-1>:1883 and 127.0.0.1:1883, again <ip-1>, peer <ip-2>',
  );
});

test('scrub masks ipv6 and mac but not times', () => {
  assert.equal(
    scrub({}, '10:01:02 fe80::1c2b:3aff:fe4d:5e6f via dc:a6:32:01:02:03 ::1'),
    '10:01:02 <ipv6-1> via <mac-1> ::1',
  );
});

test('scrub masks tailnet names', () => {
  assert.equal(scrub({}, 'https://energy-node.tail1234.ts.net/'), 'https://<tailnet-1>/');
});

test('scrub masks user only as a whole word', () => {
  assert.equal(
    scrub({ user: 'pi' }, 'pip install /home/pi/x api pi'),
    'pip install /home/<user>/x api <user>',
  );
});

test('scrub masks hostname and secrets', () => {
  assert.equal(
    scrub({ hostname: 'energy-node', secrets: ['hunter2-secret'] },
      'energy-node-dashboard[1]: energy-node up, pw hunter2-secret'),
    'energy-node-dashboard[1]: <hostname> up, pw ***',
  );
});

test('scrub leaves std::cout style names and short hosts alone', () => {
  assert.equal(scrub({ host: 'a' }, 'std::cout Bad::Thing a'), 'std::cout Bad::Thing a');
});

test('formatLine prints local time with ident and pid', () => {
  const { LiveLog } = setup();
  const at = new Date(2026, 10, 1, 9, 5, 7, 42).getTime();
  assert.equal(
    LiveLog.formatLine({ at, ident: 'systemd', pid: '1', msg: 'Started x' }),
    '09:05:07.042 systemd[1]: Started x',
  );
  assert.equal(LiveLog.formatLine({ at, msg: 'Started x' }), '09:05:07.042 Started x');
});

test('open dispatches events and resumes', () => {
  const { window, LiveLog } = setup();
  const sources = installEventSource(window);
  const calls = [];
  const stream = LiveLog.open({
    onHello: (h) => calls.push(['hello', h.units]),
    onLine: (l) => calls.push(['line', l.msg]),
    onStatus: (s) => calls.push(['status', s.unit]),
    onEnd: (e) => calls.push(['end', e.code]),
    onState: (s) => calls.push(['state', s]),
  });
  assert.equal(sources.length, 1);
  assert.equal(sources[0].url, '/api/unit-logs?token=tok');
  sources[0].emit('hello', 0, { units: [DASH] });
  sources[0].emit('line', 1, { unit: DASH, msg: 'a' });
  sources[0].emit('status', 0, { unit: DASH, active: 'active' });
  sources[0].fail();
  sources[0].emit('hello', 0, { units: [DASH] });
  sources[0].emit('line', 2, { unit: DASH, msg: 'b' });
  sources[0].emit('end', 0, { code: 'UNIT_LOGS_FAILED' });
  assert.deepEqual(plain(calls), [
    ['hello', [DASH]], ['state', 'live'],
    ['line', 'a'],
    ['status', DASH],
    ['state', 'reconnecting'],
    ['hello', [DASH]], ['state', 'live'],
    ['line', 'b'],
    ['end', 'UNIT_LOGS_FAILED'], ['state', 'ended'],
  ]);
  assert.equal(sources[0].closed, true);
  stream.close();
});

test('stage storage survives a throwing localStorage', () => {
  const { window, LiveLog } = setup();
  Object.defineProperty(window, 'localStorage', {
    get() { throw new Error('denied'); },
    configurable: true,
  });
  assert.equal(LiveLog.loadStage(), 100);
  assert.doesNotThrow(() => LiveLog.saveStage(500));
});

test('stage storage ignores values outside STAGES', () => {
  const { window, LiveLog } = setup();
  const store = { 'energy-node.diagnose.log-lines': '7' };
  Object.defineProperty(window, 'localStorage', {
    value: {
      getItem: (key) => (key in store ? store[key] : null),
      setItem: (key, value) => { store[key] = value; },
    },
    configurable: true,
  });
  assert.equal(LiveLog.loadStage(), 100);
  store['energy-node.diagnose.log-lines'] = '500';
  assert.equal(LiveLog.loadStage(), 500);
  LiveLog.saveStage(0);
  assert.equal(store['energy-node.diagnose.log-lines'], '0');
  LiveLog.saveStage(3);
  assert.equal(store['energy-node.diagnose.log-lines'], '0');
});

function bufferWith(LiveLog, unit, count, text) {
  const buf = LiveLog.buffer();
  for (let i = 1; i <= count; i += 1) {
    buf.push({ unit, at: 0, msg: text ? text(i) : `line ${i}` });
  }
  return buf;
}

const CRASH = {
  [DASH]: { load: 'loaded', active: 'activating', sub: 'auto-restart', result: 'exit-code', exit_status: 1, restarts: 9, pid: 0, since: 0 },
};

test('reportSection names last n of total', () => {
  const { LiveLog } = setup();
  const buf = bufferWith(LiveLog, DASH, 30);
  const section = LiveLog.reportSection(buf, DASH, 100, CRASH);
  const lines = section.split('\n');
  assert.equal(
    lines[0],
    '== log energy-node-dashboard.service · activating (auto-restart) · exit 1 · restarts 9 · last 30 of 30 lines ==',
  );
  assert.equal(lines.length, 31);
  assert.match(lines[1], /^\d{2}:\d{2}:\d{2}\.\d{3} line 1$/);
});

test('reportSection is empty without lines to show', () => {
  const { LiveLog } = setup();
  assert.equal(LiveLog.reportSection(bufferWith(LiveLog, DASH, 30), DASH, 0, CRASH), '');
});

test('issueMarkdown fits the budget and says so', () => {
  const { LiveLog } = setup();
  const text = 'x'.repeat(100);
  const buf = LiveLog.buffer();
  for (let i = 0; i < 2000; i += 1) {
    buf.push({ unit: DASH, at: 0, msg: text });
    buf.push({ unit: UPD, at: 0, msg: text });
  }
  const md = LiveLog.issueMarkdown('report', buf, 2000, CRASH);
  assert.ok(md.length <= LiveLog.MARKDOWN_BUDGET, `length ${md.length}`);
  assert.ok(md.includes("older lines removed to fit GitHub's limit"));
});

test('issueMarkdown cuts only the longer log first', () => {
  const { LiveLog } = setup();
  const buf = LiveLog.buffer();
  for (let i = 0; i < 2000; i += 1) {
    buf.push({ unit: DASH, at: 0, msg: 'y'.repeat(100) });
  }
  for (let i = 0; i < 10; i += 1) {
    buf.push({ unit: UPD, at: 0, msg: 'short' });
  }
  const md = LiveLog.issueMarkdown('report', buf, 2000, {});
  assert.ok(md.length <= LiveLog.MARKDOWN_BUDGET);
  const updater = md.slice(md.indexOf(`<summary>${UPD}`));
  assert.equal(updater.includes('older lines removed'), false);
  assert.equal(updater.split('short').length - 1, 10);
});

test('issueMarkdown fence survives backticks', () => {
  const { LiveLog } = setup();
  const buf = LiveLog.buffer();
  buf.push({ unit: DASH, at: 0, msg: 'a ``` b </details> c' });
  const md = LiveLog.issueMarkdown('report', buf, 100, {});
  const open = md.indexOf('````text\n');
  assert.ok(open !== -1, 'four-backtick fence opens');
  const close = md.indexOf('\n````\n', open);
  assert.ok(close !== -1, 'four-backtick fence closes');
  const body = md.slice(open, close);
  assert.ok(body.includes('</details>'));
  assert.ok(body.includes('a ``` b'));
});

test('issueMarkdown with count 0 has no details', () => {
  const { LiveLog } = setup();
  const buf = bufferWith(LiveLog, DASH, 30);
  const md = LiveLog.issueMarkdown('report', buf, 0, CRASH);
  assert.equal(md.includes('<details>'), false);
  assert.ok(md.startsWith('## energy-node diagnose\n\n```text\nreport\n```'));
  assert.equal(md.endsWith('```'), true);
});

test('issueMarkdown summary names status and line count', () => {
  const { LiveLog } = setup();
  const buf = bufferWith(LiveLog, DASH, 2);
  const md = LiveLog.issueMarkdown('report', buf, 100, CRASH);
  assert.ok(md.includes(
    '<details><summary>energy-node-dashboard.service · activating (auto-restart) · exit 1 · restarts 9 · last 2 lines</summary>',
  ));
  assert.ok(md.includes('<details><summary>energy-node-updater.service · no status yet · last 0 lines</summary>'));
});
