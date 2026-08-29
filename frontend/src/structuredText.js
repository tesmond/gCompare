class JSONLiteralParser {
  constructor(source) {
    this.source = source;
    this.index = 0;
  }

  parse() {
    const value = this.parseValue();
    this.skipWhitespace();
    if (this.index !== this.source.length) this.fail('Unexpected trailing text');
    return value;
  }

  parseValue() {
    this.skipWhitespace();
    const character = this.source[this.index];
    if (character === '{') return this.parseObject();
    if (character === '[') return this.parseArray();
    if (character === '"') return this.parseString();
    if (character === '-' || /\d/.test(character || '')) return this.parseNumber();
    return this.parseConstant();
  }

  parseObject() {
    this.expect('{');
    const entries = [];
    this.skipWhitespace();
    if (this.consume('}')) return { type: 'object', entries };
    while (true) {
      this.skipWhitespace();
      if (this.source[this.index] !== '"') this.fail('JSON object keys must be strings');
      const key = this.parseString();
      this.skipWhitespace();
      this.expect(':');
      const value = this.parseValue();
      entries.push({ key, value });
      this.skipWhitespace();
      if (this.consume('}')) break;
      this.expect(',');
    }
    return { type: 'object', entries };
  }

  parseArray() {
    this.expect('[');
    const values = [];
    this.skipWhitespace();
    if (this.consume(']')) return { type: 'array', values };
    while (true) {
      values.push(this.parseValue());
      this.skipWhitespace();
      if (this.consume(']')) break;
      this.expect(',');
    }
    return { type: 'array', values };
  }

  parseString() {
    const start = this.index;
    this.index++;
    while (this.index < this.source.length) {
      const character = this.source[this.index++];
      if (character === '\\') {
        this.index++;
        continue;
      }
      if (character !== '"') continue;
      const raw = this.source.slice(start, this.index);
      try {
        return { type: 'string', value: JSON.parse(raw) };
      } catch {
        this.fail('Invalid JSON string');
      }
    }
    this.fail('Unterminated JSON string');
  }

  parseNumber() {
    const match = this.source.slice(this.index).match(/^-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?/);
    if (!match) this.fail('Invalid JSON number');
    this.index += match[0].length;
    return { type: 'number', value: match[0] };
  }

  parseConstant() {
    for (const value of ['true', 'false', 'null']) {
      if (this.source.startsWith(value, this.index)) {
        this.index += value.length;
        return { type: 'constant', value };
      }
    }
    this.fail('Expected a JSON value');
  }

  skipWhitespace() {
    while (/[ \t\r\n]/.test(this.source[this.index] || '')) this.index++;
  }

  consume(character) {
    if (this.source[this.index] !== character) return false;
    this.index++;
    return true;
  }

  expect(character) {
    if (!this.consume(character)) this.fail(`Expected ${character}`);
  }

  fail(message) {
    throw new Error(`${message} at character ${this.index + 1}`);
  }
}

class PythonLiteralParser {
  constructor(source) {
    this.source = source;
    this.index = 0;
  }

  parse() {
    const value = this.parseValue();
    this.skipWhitespace();
    if (this.index !== this.source.length) this.fail('Unexpected trailing text');
    return value;
  }

  parseValue() {
    this.skipWhitespace();
    const character = this.source[this.index];
    if (character === '{') return this.parseDictionary();
    if (character === '[') return this.parseSequence('[', ']', 'list');
    if (character === '(') return this.parseSequence('(', ')', 'tuple');
    if (character === "'" || character === '"') return this.parseString();
    if (character === '+' || character === '-' || character === '.' || /\d/.test(character || '')) {
      return this.parseNumber();
    }
    return this.parseIdentifier();
  }

  parseDictionary() {
    this.expect('{');
    const entries = [];
    this.skipWhitespace();
    if (this.consume('}')) return { type: 'dict', entries };
    while (true) {
      const key = this.parseValue();
      this.skipWhitespace();
      this.expect(':');
      const value = this.parseValue();
      entries.push({ key, value });
      this.skipWhitespace();
      if (this.consume('}')) break;
      this.expect(',');
      this.skipWhitespace();
      if (this.consume('}')) break;
    }
    return { type: 'dict', entries };
  }

  parseSequence(open, close, type) {
    this.expect(open);
    const values = [];
    this.skipWhitespace();
    if (this.consume(close)) return { type, values };
    while (true) {
      values.push(this.parseValue());
      this.skipWhitespace();
      if (this.consume(close)) break;
      this.expect(',');
      this.skipWhitespace();
      if (this.consume(close)) break;
    }
    return { type, values };
  }

  parseString() {
    const quote = this.source[this.index++];
    let value = '';
    while (this.index < this.source.length) {
      const character = this.source[this.index++];
      if (character === quote) return { type: 'string', value };
      if (character !== '\\') {
        value += character;
        continue;
      }
      if (this.index >= this.source.length) this.fail('Unterminated escape sequence');
      const escaped = this.source[this.index++];
      const simpleEscapes = {
        a: '\x07',
        b: '\b',
        f: '\f',
        n: '\n',
        r: '\r',
        t: '\t',
        v: '\x0b',
        '\\': '\\',
        "'": "'",
        '"': '"'
      };
      if (Object.hasOwn(simpleEscapes, escaped)) {
        value += simpleEscapes[escaped];
        continue;
      }
      if (escaped === 'x' || escaped === 'u' || escaped === 'U') {
        const length = escaped === 'x' ? 2 : escaped === 'u' ? 4 : 8;
        const hexadecimal = this.source.slice(this.index, this.index + length);
        if (!new RegExp(`^[0-9a-fA-F]{${length}}$`).test(hexadecimal)) this.fail('Invalid Unicode escape');
        const codePoint = Number.parseInt(hexadecimal, 16);
        if (codePoint > 0x10ffff) this.fail('Unicode escape is out of range');
        value += String.fromCodePoint(codePoint);
        this.index += length;
        continue;
      }
      value += `\\${escaped}`;
    }
    this.fail('Unterminated string');
  }

  parseNumber() {
    const remaining = this.source.slice(this.index);
    const match = remaining.match(
      /^[+-]?(?:0[xX][0-9a-fA-F](?:_?[0-9a-fA-F])*|0[oO][0-7](?:_?[0-7])*|0[bB][01](?:_?[01])*|(?:\d(?:_?\d)*(?:\.(?:\d(?:_?\d)*)?)?|\.\d(?:_?\d)*)(?:[eE][+-]?\d(?:_?\d)*)?)/
    );
    if (!match) this.fail('Invalid number');
    this.index += match[0].length;
    return { type: 'number', value: match[0].replaceAll('_', '') };
  }

  parseIdentifier() {
    const match = this.source.slice(this.index).match(/^[A-Za-z_]\w*/);
    if (!match) this.fail('Expected a Python literal');
    this.index += match[0].length;
    if (match[0] === 'True' || match[0] === 'False' || match[0] === 'None') {
      return { type: 'constant', value: match[0] };
    }
    this.fail(`Unsupported expression ${match[0]}`);
  }

  skipWhitespace() {
    while (/\s/.test(this.source[this.index] || '')) this.index++;
  }

  consume(character) {
    if (this.source[this.index] !== character) return false;
    this.index++;
    return true;
  }

  expect(character) {
    if (!this.consume(character)) this.fail(`Expected ${character}`);
  }

  fail(message) {
    throw new Error(`${message} at character ${this.index + 1}`);
  }
}

function pythonString(value) {
  return `'${value
    .replaceAll('\\', '\\\\')
    .replaceAll("'", "\\'")
    .replaceAll('\x07', '\\a')
    .replaceAll('\b', '\\b')
    .replaceAll('\f', '\\f')
    .replaceAll('\n', '\\n')
    .replaceAll('\r', '\\r')
    .replaceAll('\t', '\\t')
    .replaceAll('\x0b', '\\v')}'`;
}

function formatJSON(node, level = 0) {
  if (node.type === 'string') return JSON.stringify(node.value);
  if (node.type === 'number' || node.type === 'constant') return node.value;

  const indentation = '  '.repeat(level);
  const childIndentation = '  '.repeat(level + 1);
  if (node.type === 'object') {
    if (!node.entries.length) return '{}';
    const entries = node.entries.map(
      ({ key, value }) => `${childIndentation}${JSON.stringify(key.value)}: ${formatJSON(value, level + 1)}`
    );
    return `{\n${entries.join(',\n')}\n${indentation}}`;
  }

  if (!node.values.length) return '[]';
  const values = node.values.map((value) => `${childIndentation}${formatJSON(value, level + 1)}`);
  return `[\n${values.join(',\n')}\n${indentation}]`;
}

function formatPython(node, level = 0) {
  if (node.type === 'string') return pythonString(node.value);
  if (node.type === 'number' || node.type === 'constant') return node.value;

  const indentation = '  '.repeat(level);
  const childIndentation = '  '.repeat(level + 1);
  if (node.type === 'dict') {
    if (!node.entries.length) return '{}';
    const entries = node.entries.map(
      ({ key, value }) => `${childIndentation}${formatPython(key, level + 1)}: ${formatPython(value, level + 1)}`
    );
    return `{\n${entries.join(',\n')}\n${indentation}}`;
  }

  const open = node.type === 'tuple' ? '(' : '[';
  const close = node.type === 'tuple' ? ')' : ']';
  if (!node.values.length) return `${open}${close}`;
  const values = node.values.map((value) => `${childIndentation}${formatPython(value, level + 1)}`);
  const singleTupleComma = node.type === 'tuple' && node.values.length === 1 ? ',' : '';
  return `${open}\n${values.join(',\n')}${singleTupleComma}\n${indentation}${close}`;
}

export function formatStructuredText(text) {
  const source = String(text || '').trim();
  if (!source) throw new Error('Nothing to beautify.');

  try {
    const value = new JSONLiteralParser(source).parse();
    return { format: 'json', text: formatJSON(value) };
  } catch {
    // Continue with the safe Python-literal parser.
  }

  try {
    const value = new PythonLiteralParser(source).parse();
    return { format: 'python', text: formatPython(value) };
  } catch {
    throw new Error('Text is not valid JSON or a supported Python literal.');
  }
}
