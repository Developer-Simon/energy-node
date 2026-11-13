import { test } from 'node:test';
import assert from 'node:assert/strict';
import { loadScripts } from './helpers/load.mjs';
import { installEventSource } from './helpers/eventsource.mjs';

const plain = (value) => JSON.parse(JSON.stringify(value));

function setup() {
  const dom = loadScripts(['api.js', 'diagnose-stream.js']);
  const window = dom.window;
  window.Api.configure({ basePath: '', token: 'tok' });
  return { window, DiagnoseStream: window.DiagnoseStream };
}

test('open connects to the diagnose stream with the token', () => {
  const { window, DiagnoseStream } = setup();
  const sources = installEventSource(window);
  const stream = DiagnoseStream.open({});
  assert.equal(sources.length, 1);
  assert.equal(sources[0].url, '/api/diagnose/stream?token=tok');
  stream.close();
  assert.equal(sources[0].closed, true);
});

test('progress events reach onProgress and the last one closes the source', () => {
  const { window, DiagnoseStream } = setup();
  const sources = installEventSource(window);
  const calls = [];
  DiagnoseStream.open({
    onProgress: (p) => calls.push(plain(p)),
    onEnd: (e) => calls.push(['end', e]),
  });
  sources[0].emit('progress', 1, { view: { units: [] }, pending: ['system'] });
  assert.equal(sources[0].closed, false);
  sources[0].emit('progress', 2, { view: { units: [] }, pending: [] });
  assert.equal(sources[0].closed, true);
  assert.deepEqual(calls, [
    { view: { units: [] }, pending: ['system'] },
    { view: { units: [] }, pending: [] },
  ]);
});

test('end event passes code and detail to onEnd and closes the source', () => {
  const { window, DiagnoseStream } = setup();
  const sources = installEventSource(window);
  const calls = [];
  DiagnoseStream.open({
    onProgress: (p) => calls.push(['progress', p]),
    onEnd: (e) => calls.push(['end', plain(e)]),
  });
  sources[0].emit('end', 3, { code: 'DIAGNOSE_FAILED', detail: 'boom' });
  assert.equal(sources[0].closed, true);
  assert.deepEqual(calls, [['end', { code: 'DIAGNOSE_FAILED', detail: 'boom' }]]);
});

test('error before the end closes, reports the stream as lost and does not reconnect', () => {
  const { window, DiagnoseStream } = setup();
  const sources = installEventSource(window);
  const calls = [];
  DiagnoseStream.open({
    onProgress: (p) => calls.push(['progress', plain(p)]),
    onEnd: (e) => calls.push(['end', plain(e)]),
  });
  sources[0].emit('progress', 1, { view: { units: [] }, pending: ['system'] });
  sources[0].fail();
  assert.equal(sources[0].closed, true);
  assert.equal(sources.length, 1);
  assert.deepEqual(calls, [
    ['progress', { view: { units: [] }, pending: ['system'] }],
    ['end', { code: 'DIAGNOSE_STREAM_LOST' }],
  ]);
});

test('error after the last progress does not report the stream as lost', () => {
  const { window, DiagnoseStream } = setup();
  const sources = installEventSource(window);
  const calls = [];
  DiagnoseStream.open({
    onProgress: () => calls.push('progress'),
    onEnd: (e) => calls.push(['end', plain(e)]),
  });
  sources[0].emit('progress', 1, { view: {}, pending: [] });
  sources[0].fail();
  assert.deepEqual(calls, ['progress']);
});

test('end is reported once even when an error follows', () => {
  const { window, DiagnoseStream } = setup();
  const sources = installEventSource(window);
  const calls = [];
  DiagnoseStream.open({ onEnd: (e) => calls.push(plain(e)) });
  sources[0].emit('end', 1, { code: 'DIAGNOSE_FAILED', detail: 'x' });
  sources[0].fail();
  assert.deepEqual(calls, [{ code: 'DIAGNOSE_FAILED', detail: 'x' }]);
});

test('invalid JSON in an event is ignored and the stream stays open', () => {
  const { window, DiagnoseStream } = setup();
  const sources = installEventSource(window);
  const calls = [];
  DiagnoseStream.open({
    onProgress: (p) => calls.push(plain(p)),
    onEnd: (e) => calls.push(['end', plain(e)]),
  });
  sources[0].listeners.progress[0]({ type: 'progress', data: '{broken' });
  sources[0].listeners.end[0]({ type: 'end', data: 'not json' });
  assert.equal(sources[0].closed, false);
  assert.deepEqual(calls, []);
  sources[0].emit('progress', 2, { view: {}, pending: [] });
  assert.deepEqual(calls, [{ view: {}, pending: [] }]);
});

test('close stops the stream and later events are ignored', () => {
  const { window, DiagnoseStream } = setup();
  const sources = installEventSource(window);
  const calls = [];
  const stream = DiagnoseStream.open({
    onProgress: () => calls.push('progress'),
    onEnd: () => calls.push('end'),
  });
  stream.close();
  assert.equal(sources[0].closed, true);
  sources[0].emit('progress', 1, { view: {}, pending: ['system'] });
  sources[0].emit('end', 2, { code: 'DIAGNOSE_FAILED' });
  sources[0].fail();
  assert.deepEqual(calls, []);
});
