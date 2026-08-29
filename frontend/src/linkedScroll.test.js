import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

import { applyLinkedScrollDelta, syncLinkedScrollPosition, wheelDeltaPixels } from './linkedScroll.js';

function scroller({ left = 0, top = 0, width = 200, height = 300, scrollWidth = 800, scrollHeight = 1200 } = {}) {
  return {
    scrollLeft: left,
    scrollTop: top,
    clientWidth: width,
    clientHeight: height,
    scrollWidth,
    scrollHeight
  };
}

test('applies the same horizontal and vertical distance to both panes', () => {
  const left = scroller({ left: 80, top: 120 });
  const right = scroller({ left: 80, top: 120 });

  const applied = applyLinkedScrollDelta([left, right], 55, 70);

  assert.deepEqual(applied, { deltaX: 55, deltaY: 70 });
  assert.equal(left.scrollLeft, 135);
  assert.equal(right.scrollLeft, 135);
  assert.equal(left.scrollTop, 190);
  assert.equal(right.scrollTop, 190);
});

test('limits both panes to the distance available in the shorter pane', () => {
  const left = scroller({ left: 90, width: 200, scrollWidth: 310 });
  const right = scroller({ left: 90, width: 200, scrollWidth: 800 });

  const applied = applyLinkedScrollDelta([left, right], 50, 0);

  assert.equal(applied.deltaX, 20);
  assert.equal(left.scrollLeft, 110);
  assert.equal(right.scrollLeft, 110);
});

test('native scrollbar movement restores one shared position', () => {
  const left = scroller({ left: 240, top: 380 });
  const right = scroller({ left: 20, top: 40 });

  const position = syncLinkedScrollPosition([left, right], left);

  assert.deepEqual(position, { scrollLeft: 240, scrollTop: 380 });
  assert.equal(right.scrollLeft, 240);
  assert.equal(right.scrollTop, 380);
});

test('the diff editor does not enable line wrapping', async () => {
  const source = await readFile(new URL('./DiffEditor.svelte', import.meta.url), 'utf8');
  assert.doesNotMatch(source, /EditorView\.lineWrapping/);
});

test('shift-wheel becomes a horizontal linked delta', () => {
  const result = wheelDeltaPixels(
    { deltaX: 0, deltaY: 3, deltaMode: 1, shiftKey: true },
    scroller(),
    18
  );
  assert.deepEqual(result, { deltaX: 54, deltaY: 0 });
});
