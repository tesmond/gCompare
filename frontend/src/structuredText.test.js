import assert from 'node:assert/strict';
import test from 'node:test';

import { formatStructuredText } from './structuredText.js';

test('beautifies a single-line JSON response', () => {
  const result = formatStructuredText('{"name":"station","values":[1,2],"active":true}');
  assert.equal(result.format, 'json');
  assert.equal(
    result.text,
    `{
  "name": "station",
  "values": [
    1,
    2
  ],
  "active": true
}`
  );
});

test('beautifies a nested Python dictionary without changing Python literals', () => {
  const result = formatStructuredText("{'name': 'station', 'flags': [True, False, None], 'point': (1, 2)}");
  assert.equal(result.format, 'python');
  assert.equal(
    result.text,
    `{
  'name': 'station',
  'flags': [
    True,
    False,
    None
  ],
  'point': (
    1,
    2
  )
}`
  );
});

test('detects valid JSON before the overlapping Python subset', () => {
  const result = formatStructuredText('{"value": null}');
  assert.equal(result.format, 'json');
  assert.match(result.text, /"value": null/);
});

test('preserves large JSON integers exactly', () => {
  const result = formatStructuredText('{"id":9223372036854775807,"id":9223372036854775808}');
  assert.equal(
    result.text,
    `{
  "id": 9223372036854775807,
  "id": 9223372036854775808
}`
  );
});

test('rejects Python expressions instead of evaluating them', () => {
  assert.throws(
    () => formatStructuredText("{'value': __import__('os').getcwd()}"),
    /not valid JSON or a supported Python literal/
  );
});

test('rejects empty text', () => {
  assert.throws(() => formatStructuredText('   '), /Nothing to beautify/);
});
