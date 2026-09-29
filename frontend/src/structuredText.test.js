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

test('beautifies pydantic dict() output containing UUID, datetime, Decimal and enum reprs', () => {
  const input =
    "{'id': UUID('12345678-1234-5678-1234-567812345678'), 'created': datetime.datetime(2024, 1, 2, 3, 4, tzinfo=datetime.timezone.utc), " +
    "'price': Decimal('1.50'), 'status': <Status.ACTIVE: 'active'>, 'tags': {'a', 'b'}, 'raw': b'\\x00ab', 'ratio': -inf, 'none': None}";
  const result = formatStructuredText(input);
  assert.equal(result.format, 'python');
  assert.equal(
    result.text,
    `{
  'id': UUID('12345678-1234-5678-1234-567812345678'),
  'created': datetime.datetime(2024, 1, 2, 3, 4, tzinfo=datetime.timezone.utc),
  'price': Decimal('1.50'),
  'status': <Status.ACTIVE: 'active'>,
  'tags': {
    'a',
    'b'
  },
  'raw': b'\\x00ab',
  'ratio': -inf,
  'none': None
}`
  );
});

test('beautifies a pydantic model repr with keyword arguments and nested models', () => {
  const result = formatStructuredText(
    "User(id=UUID('12345678-1234-5678-1234-567812345678'), address=Address(city='X', zip=None), roles=['a', 'b'], meta={})"
  );
  assert.equal(
    result.text,
    `User(
  id=UUID('12345678-1234-5678-1234-567812345678'),
  address=Address(city='X', zip=None),
  roles=[
    'a',
    'b'
  ],
  meta={}
)`
  );
});

test('handles empty sets, ellipsis and enum reprs containing angle brackets in strings', () => {
  const result = formatStructuredText("{'a': set(), 'b': [...], 'c': <E.X: '>'>}");
  assert.match(result.text, /'a': set\(\)/);
  assert.match(result.text, /\.\.\./);
  assert.match(result.text, /'c': <E\.X: '>'>/);
});

test('still rejects operators and method calls on results', () => {
  assert.throws(() => formatStructuredText("{'a': 1 + 2}"), /not valid JSON or a supported Python literal/);
});
