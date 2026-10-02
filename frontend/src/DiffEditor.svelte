<script>
  import { onDestroy, onMount, tick } from 'svelte';
  import { EditorState, RangeSetBuilder, StateEffect, StateField } from '@codemirror/state';
  import { defaultKeymap, history, historyKeymap } from '@codemirror/commands';
  import { Decoration, EditorView, WidgetType, drawSelection, keymap, lineNumbers } from '@codemirror/view';
  import { applyLinkedScrollDelta, syncLinkedScrollPosition, wheelDeltaPixels } from './linkedScroll.js';

  export let leftText = '';
  export let rightText = '';
  export let rows = [];
  export let selectedRange = null;
  export let readOnly = false;
  export let onChange = () => {};
  export let onSelectRange = () => {};
  export let onContextMenu = () => {};
  export let onViewportChange = () => {};

  let leftHost;
  let rightHost;
  let leftView;
  let rightView;
  let suppressChange = false;
  let suppressSelection = false;
  let latestRows = [];
  let resizeObserver;
  let scrollRangeFrame = 0;

  const setRowsEffect = StateEffect.define();
  const setExtraSpaceEffect = StateEffect.define();

  // Extra space added to the shorter/narrower editor so both editors have the
  // same scroll range and linked scrolling can reach the end of either side.
  // It is applied through CodeMirror's content attributes (as CSS variables
  // used by app.css) so CodeMirror measures it rather than fighting it.
  const extraSpaceField = StateField.define({
    create: () => ({ right: 0, bottom: 0 }),
    update(value, transaction) {
      for (const effect of transaction.effects) {
        if (effect.is(setExtraSpaceEffect)) value = effect.value;
      }
      return value;
    },
    provide: (field) =>
      EditorView.contentAttributes.from(field, (value) => ({
        style: `--gc-extra-right: ${value.right}px; --gc-extra-bottom: ${value.bottom}px`
      }))
  });

  // Block spacer that stands in for lines that only exist on the other side,
  // so matching sections stay vertically aligned (like IntelliJ/VS Code).
  class GapWidget extends WidgetType {
    constructor(count) {
      super();
      this.count = count;
    }
    eq(other) {
      return other.count === this.count;
    }
    get estimatedHeight() {
      return this.count * 14;
    }
    toDOM(view) {
      const element = document.createElement('div');
      element.className = 'gc-gap-widget gc-semantic-orphan_gap';
      // Use the editor's measured line height so gaps match real lines exactly.
      element.style.height = `${this.count * view.defaultLineHeight}px`;
      element.setAttribute('aria-hidden', 'true');
      return element;
    }
    ignoreEvent() {
      return true;
    }
  }

  function segmentIsDiff(segment) {
    return segment?.isDiffToken ?? segment?.changed;
  }

  function semanticState(row, side) {
    const state = side === 'left' ? row.leftSemanticState : row.rightSemanticState;
    if (state) return state;
    if (row.status === 'left_only') return side === 'left' ? 'IMPORTANT_DIFF' : 'ORPHAN_GAP';
    if (row.status === 'right_only') return side === 'right' ? 'IMPORTANT_DIFF' : 'ORPHAN_GAP';
    if (row.status === 'equal') return 'MATCH';
    return 'IMPORTANT_DIFF';
  }

  function rowIsDifference(row) {
    if (!row) return false;
    if (row.status && row.status !== 'equal') return true;
    return [row.semanticState, row.leftSemanticState, row.rightSemanticState].some(
      (state) => state === 'IMPORTANT_DIFF' || state === 'UNIMPORTANT_DIFF' || state === 'ORPHAN_GAP'
    );
  }

  function differenceGroups() {
    const groups = [];
    let current = null;
    for (let index = 0; index < latestRows.length; index++) {
      if (!rowIsDifference(latestRows[index])) {
        current = null;
        continue;
      }
      if (!current) {
        current = { start: index, end: index };
        groups.push(current);
      } else {
        current.end = index;
      }
    }
    return groups;
  }

  function sideLineNumber(row, side) {
    return side === 'left' ? row.leftLineNumber : row.rightLineNumber;
  }

  function sideSegments(row, side) {
    const segments = side === 'left' ? row.leftSegments : row.rightSegments;
    if (segments?.length) return segments;
    const text = side === 'left' ? row.leftText : row.rightText;
    return text ? [{ text, isDiffToken: row.status !== 'equal', changed: row.status !== 'equal' }] : [];
  }

  function decorationField(side) {
    return StateField.define({
      create(state) {
        return buildDecorations(state, latestRows, side);
      },
      update(decorations, transaction) {
        let next = decorations.map(transaction.changes);
        for (const effect of transaction.effects) {
          if (effect.is(setRowsEffect)) {
            next = buildDecorations(transaction.state, effect.value, side);
          }
        }
        return next;
      },
      provide: (field) => EditorView.decorations.from(field)
    });
  }

  function buildDecorations(state, diffRows, side) {
    const entries = [];
    const add = (from, to, deco) => entries.push({ from, to, deco });
    const rowsForSide = diffRows || [];
    let pendingGap = 0;

    rowsForSide.forEach((row, index) => {
      const lineNumber = sideLineNumber(row, side);
      if (!lineNumber || lineNumber < 1 || lineNumber > state.doc.lines) {
        if (!lineNumber) pendingGap++;
        return;
      }
      const line = state.doc.line(lineNumber);
      if (pendingGap > 0) {
        add(line.from, line.from, Decoration.widget({ widget: new GapWidget(pendingGap), block: true, side: -1 }));
        pendingGap = 0;
      }
      const semantic = semanticState(row, side).toLowerCase();
      const selected = selectedRange && index >= selectedRange.start && index <= selectedRange.end ? ' gc-selected-line' : '';
      add(
        line.from,
        line.from,
        Decoration.line({
          class: `gc-line gc-status-${row.status} gc-semantic-${semantic} gc-side-${side}${selected}`
        })
      );

      let offset = line.from;
      for (const segment of sideSegments(row, side)) {
        const length = segment.text?.length || 0;
        const from = offset;
        const to = Math.min(line.to, offset + length);
        if (segmentIsDiff(segment) && to > from) {
          add(from, to, Decoration.mark({ class: 'gc-diff-token' }));
        }
        offset += length;
      }
    });

    if (pendingGap > 0) {
      const end = state.doc.length;
      add(end, end, Decoration.widget({ widget: new GapWidget(pendingGap), block: true, side: 1 }));
    }

    entries.sort((a, b) => a.from - b.from || a.deco.startSide - b.deco.startSide);
    const builder = new RangeSetBuilder();
    for (const { from, to, deco } of entries) builder.add(from, to, deco);
    return builder.finish();
  }

  function editorExtensions(side) {
    return [
      lineNumbers(),
      history(),
      drawSelection(),
      decorationField(side),
      extraSpaceField,
      keymap.of([...defaultKeymap, ...historyKeymap]),
      EditorState.tabSize.of(2),
      EditorState.readOnly.of(readOnly),
      EditorView.updateListener.of((update) => {
        if (update.docChanged && !suppressChange) {
          onChange(side, update.state.doc.toString());
        }
        if (update.selectionSet && !suppressSelection) {
          emitSelection(side, update.view);
        }
        if (update.docChanged || update.geometryChanged) {
          scheduleEqualScrollRanges();
        }
      })
    ];
  }

  function emitSelection(side, view) {
    const selection = view.state.selection.main;
    const startLine = view.state.doc.lineAt(selection.from).number;
    const endLine = view.state.doc.lineAt(selection.to).number;
    const start = rowIndexForLine(side, startLine);
    const end = rowIndexForLine(side, endLine);
    if (start === null || end === null) return;
    onSelectRange({ start: Math.min(start, end), end: Math.max(start, end), side });
  }

  function rowIndexForLine(side, lineNumber) {
    const key = side === 'left' ? 'leftLineNumber' : 'rightLineNumber';
    const index = latestRows.findIndex((row) => row[key] === lineNumber);
    return index === -1 ? null : index;
  }

  function currentRightRowIndex() {
    if (selectedRange?.start !== undefined && selectedRange?.start !== null) return selectedRange.start;
    if (!rightView) return null;
    const lineNumber = rightView.state.doc.lineAt(rightView.state.selection.main.head).number;
    return rowIndexForLine('right', lineNumber);
  }

  function rightLineForGroup(group) {
    if (!rightView || !group) return 1;
    for (let index = group.start; index <= group.end; index++) {
      const lineNumber = latestRows[index]?.rightLineNumber;
      if (lineNumber) return lineNumber;
    }
    const insertIndex = latestRows[group.start]?.rightInsertIndex || 0;
    return Math.max(1, Math.min(rightView.state.doc.lines, insertIndex + 1));
  }

  function targetGroupIndex(groups, direction) {
    const current = currentRightRowIndex();
    if (current === null || current === undefined) return direction === 'previous' ? groups.length - 1 : 0;
    if (direction === 'previous') {
      const previous = groups.findLastIndex((group) => group.end < current);
      return previous === -1 ? groups.length - 1 : previous;
    }
    const next = groups.findIndex((group) => group.start > current);
    return next === -1 ? 0 : next;
  }

  export function goToDifference(direction = 'next') {
    if (!rightView) return;
    const groups = differenceGroups();
    if (!groups.length) return;
    const group = groups[targetGroupIndex(groups, direction)];
    const line = rightView.state.doc.line(rightLineForGroup(group));
    suppressSelection = true;
    rightView.dispatch({
      selection: { anchor: line.from },
      effects: EditorView.scrollIntoView(line.from, { y: 'center' })
    });
    suppressSelection = false;
    rightView.focus();
    onSelectRange({ start: group.start, end: group.end, side: 'right' });
  }

  function replaceDocument(view, value) {
    if (!view) return;
    const text = value || '';
    if (view.state.doc.toString() === text) return;
    suppressChange = true;
    view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: text } });
    suppressChange = false;
    scheduleEqualScrollRanges();
  }

  function refreshDecorations() {
    leftView?.dispatch({ effects: setRowsEffect.of(latestRows) });
    rightView?.dispatch({ effects: setRowsEffect.of(latestRows) });
  }

  function scrollDOMs() {
    return [leftView?.scrollDOM, rightView?.scrollDOM].filter(Boolean);
  }

  function reportViewport(scroller) {
    if (!scroller) return;
    onViewportChange({
      scrollTop: scroller.scrollTop,
      clientHeight: scroller.clientHeight,
      scrollHeight: scroller.scrollHeight
    });
  }

  function syncScroll(source) {
    if (!source) return;
    syncLinkedScrollPosition(scrollDOMs(), source.scrollDOM);
    reportViewport(source.scrollDOM);
  }

  function handleLeftScroll() {
    syncScroll(leftView);
  }

  function handleRightScroll() {
    syncScroll(rightView);
  }

  function handleWheel(event, source) {
    if (!source || event.ctrlKey) return;
    const deltas = wheelDeltaPixels(event, source.scrollDOM);
    if (!deltas.deltaX && !deltas.deltaY) return;
    event.preventDefault();
    syncLinkedScrollPosition(scrollDOMs(), source.scrollDOM);
    applyLinkedScrollDelta(scrollDOMs(), deltas.deltaX, deltas.deltaY);
    reportViewport(source.scrollDOM);
  }

  function handleLeftWheel(event) {
    handleWheel(event, leftView);
  }

  function handleRightWheel(event) {
    handleWheel(event, rightView);
  }

  function naturalHeight(view) {
    // Scrollable height (includes block gap widgets) minus our own extra padding.
    return view.scrollDOM.scrollHeight - view.state.field(extraSpaceField).bottom;
  }

  function naturalWidth(view) {
    return view.scrollDOM.scrollWidth - view.state.field(extraSpaceField).right;
  }

  function equalizeScrollRanges() {
    if (!leftView || !rightView) return;
    const views = [leftView, rightView];
    const heights = views.map(naturalHeight);
    const widths = views.map(naturalWidth);
    const maximumHeight = Math.max(...heights);
    const maximumWidth = Math.max(...widths);
    views.forEach((view, index) => {
      const next = {
        right: Math.max(0, Math.round(maximumWidth - widths[index])),
        bottom: Math.max(0, Math.round(maximumHeight - heights[index]))
      };
      const current = view.state.field(extraSpaceField);
      if (Math.abs(current.right - next.right) > 1 || Math.abs(current.bottom - next.bottom) > 1) {
        view.dispatch({ effects: setExtraSpaceEffect.of(next) });
      }
    });
    syncLinkedScrollPosition(scrollDOMs(), leftView.scrollDOM);
    reportViewport(leftView.scrollDOM);
  }

  function scheduleEqualScrollRanges() {
    if (!leftView || !rightView) return;
    cancelAnimationFrame(scrollRangeFrame);
    scrollRangeFrame = requestAnimationFrame(equalizeScrollRanges);
  }

  function editorForTarget(target) {
    if (leftView?.dom.contains(target)) return { side: 'left', view: leftView };
    if (rightView?.dom.contains(target)) return { side: 'right', view: rightView };
    return null;
  }

  function handleContextMenu(event) {
    const editor = editorForTarget(event.target);
    if (!editor) return;
    const position = editor.view.posAtCoords({ x: event.clientX, y: event.clientY });
    if (position === null) return;
    const lineNumber = editor.view.state.doc.lineAt(position).number;
    const rowIndex = rowIndexForLine(editor.side, lineNumber);
    if (rowIndex === null) return;

    event.preventDefault();
    const range = { start: rowIndex, end: rowIndex, side: editor.side };
    onSelectRange(range);
    onContextMenu({
      x: event.clientX,
      y: event.clientY,
      side: editor.side,
      rowIndex,
      row: latestRows[rowIndex]
    });
  }

  function createEditor(parent, side, doc) {
    return new EditorView({
      state: EditorState.create({
        doc: doc || '',
        extensions: editorExtensions(side)
      }),
      parent
    });
  }

  onMount(() => {
    latestRows = rows || [];
    leftView = createEditor(leftHost, 'left', leftText);
    rightView = createEditor(rightHost, 'right', rightText);
    leftView.scrollDOM.addEventListener('scroll', handleLeftScroll);
    rightView.scrollDOM.addEventListener('scroll', handleRightScroll);
    leftView.scrollDOM.addEventListener('wheel', handleLeftWheel, { passive: false });
    rightView.scrollDOM.addEventListener('wheel', handleRightWheel, { passive: false });
    leftHost.addEventListener('contextmenu', handleContextMenu);
    rightHost.addEventListener('contextmenu', handleContextMenu);
    resizeObserver = new ResizeObserver(scheduleEqualScrollRanges);
    resizeObserver.observe(leftHost);
    resizeObserver.observe(rightHost);
    tick().then(() => {
      if (leftView) {
        reportViewport(leftView.scrollDOM);
      }
      scheduleEqualScrollRanges();
    });
  });

  onDestroy(() => {
    leftView?.scrollDOM.removeEventListener('scroll', handleLeftScroll);
    rightView?.scrollDOM.removeEventListener('scroll', handleRightScroll);
    leftView?.scrollDOM.removeEventListener('wheel', handleLeftWheel);
    rightView?.scrollDOM.removeEventListener('wheel', handleRightWheel);
    leftHost?.removeEventListener('contextmenu', handleContextMenu);
    rightHost?.removeEventListener('contextmenu', handleContextMenu);
    resizeObserver?.disconnect();
    cancelAnimationFrame(scrollRangeFrame);
    leftView?.destroy();
    rightView?.destroy();
    leftView = null;
    rightView = null;
  });

  $: if (leftView && rightView) {
    replaceDocument(leftView, leftText);
    replaceDocument(rightView, rightText);
  }

  $: {
    latestRows = rows || [];
    refreshDecorations();
  }

  $: {
    selectedRange;
    refreshDecorations();
  }
</script>

<div class="code-diff-editor">
  <div class="code-diff-pane code-diff-pane-left" bind:this={leftHost}></div>
  <div class="code-diff-pane code-diff-pane-right" bind:this={rightHost}></div>
</div>
