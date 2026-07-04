<script>
  import { onDestroy, onMount, tick } from 'svelte';
  import { EditorState, RangeSetBuilder, StateEffect, StateField } from '@codemirror/state';
  import { defaultKeymap, history, historyKeymap } from '@codemirror/commands';
  import { Decoration, EditorView, drawSelection, keymap, lineNumbers } from '@codemirror/view';

  export let leftText = '';
  export let rightText = '';
  export let rows = [];
  export let selectedRange = null;
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
  let suppressScroll = false;
  let latestRows = [];

  const setRowsEffect = StateEffect.define();

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
    const builder = new RangeSetBuilder();
    const orderedRows = [...(diffRows || [])]
      .map((row, index) => ({ row, index, lineNumber: sideLineNumber(row, side) }))
      .filter((item) => item.lineNumber)
      .sort((a, b) => a.lineNumber - b.lineNumber);

    for (const { row, index, lineNumber } of orderedRows) {
      if (lineNumber < 1 || lineNumber > state.doc.lines) continue;
      const line = state.doc.line(lineNumber);
      const semantic = semanticState(row, side).toLowerCase();
      const selected = selectedRange && index >= selectedRange.start && index <= selectedRange.end ? ' gc-selected-line' : '';
      builder.add(
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
          builder.add(from, to, Decoration.mark({ class: 'gc-diff-token' }));
        }
        offset += length;
      }
    }

    return builder.finish();
  }

  function editorExtensions(side) {
    return [
      lineNumbers(),
      history(),
      drawSelection(),
      decorationField(side),
      keymap.of([...defaultKeymap, ...historyKeymap]),
      EditorState.tabSize.of(2),
      EditorView.lineWrapping,
      EditorView.updateListener.of((update) => {
        if (update.docChanged && !suppressChange) {
          onChange(side, update.state.doc.toString());
        }
        if (update.selectionSet && !suppressSelection) {
          emitSelection(side, update.view);
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
  }

  function refreshDecorations() {
    leftView?.dispatch({ effects: setRowsEffect.of(latestRows) });
    rightView?.dispatch({ effects: setRowsEffect.of(latestRows) });
  }

  function syncScroll(source, target) {
    if (!source || !target || suppressScroll) return;
    suppressScroll = true;
    target.scrollDOM.scrollTop = source.scrollDOM.scrollTop;
    target.scrollDOM.scrollLeft = source.scrollDOM.scrollLeft;
    onViewportChange({
      scrollTop: source.scrollDOM.scrollTop,
      clientHeight: source.scrollDOM.clientHeight
    });
    requestAnimationFrame(() => {
      suppressScroll = false;
    });
  }

  function handleLeftScroll() {
    syncScroll(leftView, rightView);
  }

  function handleRightScroll() {
    syncScroll(rightView, leftView);
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
    leftHost.addEventListener('contextmenu', handleContextMenu);
    rightHost.addEventListener('contextmenu', handleContextMenu);
    tick().then(() => {
      if (leftView) {
        onViewportChange({ scrollTop: leftView.scrollDOM.scrollTop, clientHeight: leftView.scrollDOM.clientHeight });
      }
    });
  });

  onDestroy(() => {
    leftView?.scrollDOM.removeEventListener('scroll', handleLeftScroll);
    rightView?.scrollDOM.removeEventListener('scroll', handleRightScroll);
    leftHost?.removeEventListener('contextmenu', handleContextMenu);
    rightHost?.removeEventListener('contextmenu', handleContextMenu);
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
