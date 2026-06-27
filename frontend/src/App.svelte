<script>
  import { tick } from 'svelte';

  const appApi = () => window.go?.main?.App;

  let tabs = [];
  let activeTabID = '';
  let nextID = 1;
  let error = '';
  let contextMenu = null;
  let contextMenuElement;
  let pendingClose = null;
  let shiftAnchor = null;
  let leftComparePane;
  let centerComparePane;
  let rightComparePane;
  let mapScrollTop = 0;
  let mapViewportHeight = 0;
  const fileRowHeight = 28;
  const emptyBrowser = () => ({
    mode: 'empty',
    path: '',
    parent: '',
    entries: [],
    lines: [],
    warning: '',
    loading: false,
    error: ''
  });
  const emptySourceState = () => ({
    sourceLock: '',
    leftSource: null,
    rightSource: null,
    browsers: {
      left: emptyBrowser(),
      right: emptyBrowser()
    }
  });

  $: activeTab = tabs.find((tab) => tab.id === activeTabID);
  $: sourceTab = activeTab?.mode === 'new' ? activeTab : null;
  $: sourceLock = sourceTab?.sourceLock || '';
  $: leftSource = sourceTab?.leftSource || null;
  $: rightSource = sourceTab?.rightSource || null;
  $: browsers = sourceTab?.browsers || emptySourceState().browsers;

  function newID() {
    return `tab-${nextID++}`;
  }

  function backend() {
    const api = appApi();
    if (!api) {
      throw new Error('Wails backend is not available. Run this app with Wails to compare files.');
    }
    return api;
  }

  function basename(path) {
    return path?.split(/[\\/]/).filter(Boolean).pop() || path || '';
  }

  function fileTitle(left, right) {
    return `File: ${basename(left)} ↔ ${basename(right)}`;
  }

  function folderTitle(left, right) {
    return `Folder: ${basename(left)} ↔ ${basename(right)}`;
  }

  function sourceTitle(left, right, lock = '') {
    const label = (type) => (type === 'folder' ? 'Folder' : 'File');
    if (left && right && left.type === right.type) {
      return `${label(left.type)}: ${basename(left.path)} ↔ ${basename(right.path)}`;
    }
    const source = left || right;
    if (source) return `${label(source.type)}: ${basename(source.path)}`;
    if (lock) return `${label(lock)} comparison`;
    return 'New comparison';
  }

  function tabDirty(tab) {
    return tab?.mode === 'file' && Boolean(tab?.result?.leftDirty || tab?.result?.rightDirty);
  }

  function tabModeLabel(tab) {
    return tab?.mode === 'new' ? 'new' : tab?.mode || '';
  }

  function getTab(tabID) {
    return tabs.find((tab) => tab.id === tabID);
  }

  function createSourceTab() {
    const tab = {
      id: newID(),
      mode: 'new',
      title: 'New comparison',
      ...emptySourceState()
    };
    tabs = [...tabs, tab];
    activeTabID = tab.id;
    error = '';
    return tab;
  }

  function updateSourceTab(tabID, patcher) {
    tabs = tabs.map((tab) => {
      if (tab.id !== tabID || tab.mode !== 'new') return tab;
      const patch = typeof patcher === 'function' ? patcher(tab) : patcher;
      return { ...tab, ...patch };
    });
  }

  async function chooseTwo(kind) {
    const api = backend();
    const chooser = kind === 'folder' ? api.ChooseDirectory : api.ChooseFile;
    const left = await chooser();
    if (!left) return null;
    const right = await chooser();
    if (!right) return null;
    return { left, right };
  }

  async function openFolderComparison(paths, options = {}) {
    error = '';
    let targetID = options.tabID || '';
    try {
      const chosen = paths || (await chooseTwo('folder'));
      if (!chosen) return;
      targetID = targetID || newID();
      const tab = {
        id: targetID,
        mode: 'folder',
        title: folderTitle(chosen.left, chosen.right),
        leftPath: chosen.left,
        rightPath: chosen.right,
        result: null,
        loading: true,
        error: '',
        selectedRow: null
      };
      if (options.tabID) {
        updateTab(tab.id, tab);
      } else {
        tabs = [...tabs, tab];
      }
      activeTabID = tab.id;
      const result = await backend().OpenFolderComparison(tab.id, chosen.left, chosen.right);
      updateTab(tab.id, { result, loading: false });
    } catch (err) {
      if (targetID) {
        failTab(targetID, err);
      } else {
        error = err?.message || String(err);
      }
    }
  }

  async function openFileComparison(paths, options = {}) {
    error = '';
    let targetID = options.tabID || '';
    try {
      const chosen = paths || (await chooseTwo('file'));
      if (!chosen) return;
      targetID = targetID || newID();
      const tab = {
        id: targetID,
        mode: 'file',
        title: fileTitle(chosen.left, chosen.right),
        leftPath: chosen.left,
        rightPath: chosen.right,
        result: null,
        loading: true,
        error: '',
        selectionStart: null,
        selectionEnd: null
      };
      if (options.tabID) {
        updateTab(tab.id, tab);
      } else {
        tabs = [...tabs, tab];
      }
      activeTabID = tab.id;
      const result = await backend().OpenFileComparison(tab.id, chosen.left, chosen.right);
      updateTab(tab.id, { result, loading: false });
    } catch (err) {
      if (targetID) {
        failTab(targetID, err);
      } else {
        error = err?.message || String(err);
      }
    }
  }

  async function refreshActive() {
    if (!activeTab) return;
    error = '';
    updateTab(activeTab.id, { loading: true, error: '' });
    try {
      if (activeTab.mode === 'folder') {
        const result = await backend().RefreshFolderComparison(activeTab.id, activeTab.leftPath, activeTab.rightPath);
        updateTab(activeTab.id, { result, loading: false });
      } else {
        const result = await backend().RefreshFileComparison(activeTab.id);
        updateTab(activeTab.id, { result, loading: false });
      }
    } catch (err) {
      failActive(err);
    }
  }

  async function saveActive(side = 'both') {
    if (!activeTab || activeTab.mode !== 'file') return;
    error = '';
    try {
      const result = await backend().SaveFileComparison(activeTab.id, side);
      updateTab(activeTab.id, { result });
    } catch (err) {
      failActive(err);
    }
  }

  async function discardActive() {
    if (!activeTab || activeTab.mode !== 'file') return;
    if (!confirm('Discard unsaved changes in this comparison?')) return;
    error = '';
    try {
      const result = await backend().DiscardFileChanges(activeTab.id);
      updateTab(activeTab.id, { result });
    } catch (err) {
      failActive(err);
    }
  }

  function requestClose(tab) {
    if (tab.mode === 'file' && tabDirty(tab)) {
      pendingClose = tab;
      return;
    }
    closeTab(tab.id);
  }

  async function closeWithSave() {
    if (!pendingClose) return;
    const tabID = pendingClose.id;
    pendingClose = null;
    try {
      await backend().SaveFileComparison(tabID, 'both');
      closeTab(tabID);
    } catch (err) {
      failActive(err);
    }
  }

  function closeWithDiscard() {
    if (!pendingClose) return;
    closeTab(pendingClose.id);
    pendingClose = null;
  }

  function closeTab(tabID) {
    const tab = tabs.find((item) => item.id === tabID);
    if (tab?.mode === 'file') {
      backend().CloseFileComparison(tabID).catch(() => {});
    } else if (tab?.mode === 'folder') {
      backend().CloseFolderComparison(tabID).catch(() => {});
    }
    tabs = tabs.filter((item) => item.id !== tabID);
    if (activeTabID === tabID) {
      activeTabID = tabs.at(-1)?.id || '';
    }
  }

  function updateTab(tabID, patch) {
    tabs = tabs.map((tab) => (tab.id === tabID ? { ...tab, ...patch } : tab));
  }

  function failActive(err) {
    if (!activeTab) {
      error = err?.message || String(err);
      return;
    }
    failTab(activeTab.id, err);
  }

  function failTab(tabID, err) {
    const message = err?.message || String(err);
    error = message;
    updateTab(tabID, { loading: false, error: message });
  }

  function canSelectTypeFor(tab, type) {
    return tab?.mode === 'new' && (type === 'file' || type === 'folder') && (!tab.sourceLock || tab.sourceLock === type);
  }

  async function loadBrowser(side, path, tabID = activeTabID) {
    const tab = getTab(tabID);
    if (tab?.mode !== 'new') return;
    updateSourceTab(tabID, (current) => ({
      browsers: {
        ...current.browsers,
        [side]: { ...current.browsers[side], loading: true, error: '' }
      }
    }));
    try {
      const listing = await backend().ListDirectory(path || '');
      updateSourceTab(tabID, (current) => ({
        browsers: {
          ...current.browsers,
          [side]: {
            mode: 'folder',
            path: listing.path,
            parent: listing.parent,
            entries: listing.entries || [],
            lines: [],
            warning: '',
            loading: false,
            error: ''
          }
        }
      }));
    } catch (err) {
      updateSourceTab(tabID, (current) => ({
        browsers: {
          ...current.browsers,
          [side]: {
            ...current.browsers[side],
            loading: false,
            error: err?.message || String(err)
          }
        }
      }));
    }
  }

  function canSelectType(type) {
    return canSelectTypeFor(activeTab, type);
  }

  function selectedSourcePatch(tab, side, path, type) {
    const nextSource = { path, type };
    let nextLeft = side === 'left' ? nextSource : tab.leftSource;
    let nextRight = side === 'right' ? nextSource : tab.rightSource;
    if (side === 'left') {
      if (nextRight && nextRight.type !== type) nextRight = null;
    } else if (nextLeft && nextLeft.type !== type) {
      nextLeft = null;
    }
    return {
      leftSource: nextLeft,
      rightSource: nextRight,
      sourceLock: type,
      title: sourceTitle(nextLeft, nextRight, type)
    };
  }

  function selectSource(tabID, side, path, type) {
    const tab = getTab(tabID);
    if (!canSelectTypeFor(tab, type)) return null;
    const patch = selectedSourcePatch(tab, side, path, type);
    updateSourceTab(tabID, patch);
    error = '';
    return patch;
  }

  async function selectFolderSource(side, path) {
    const tabID = activeTabID;
    const patch = selectSource(tabID, side, path, 'folder');
    if (!patch) return;
    await loadBrowser(side, path, tabID);
    if (patch.leftSource?.type === 'folder' && patch.rightSource?.type === 'folder') {
      await openFolderComparison({ left: patch.leftSource.path, right: patch.rightSource.path }, { tabID });
    }
  }

  async function selectFileSource(side, path) {
    const tabID = activeTabID;
    const tab = getTab(tabID);
    if (!canSelectTypeFor(tab, 'file')) return;
    updateSourceTab(tabID, (current) => ({
      browsers: {
        ...current.browsers,
        [side]: { ...current.browsers[side], mode: 'file', path, loading: true, error: '', warning: '', lines: [] }
      }
    }));
    try {
      const preview = await backend().PreviewFile(path);
      const current = getTab(tabID);
      if (!current || current.mode !== 'new') return;
      const sourcePatch = selectedSourcePatch(current, side, preview.path, 'file');
      updateSourceTab(tabID, {
        ...sourcePatch,
        browsers: {
          ...current.browsers,
          [side]: {
            mode: 'file',
            path: preview.path,
            parent: preview.parent,
            entries: [],
            lines: preview.lines || [],
            warning: preview.warning || '',
            loading: false,
            error: ''
          }
        }
      });
      if (sourcePatch.leftSource?.type === 'file' && sourcePatch.rightSource?.type === 'file') {
        await openFileComparison({ left: sourcePatch.leftSource.path, right: sourcePatch.rightSource.path }, { tabID });
      }
    } catch (err) {
      updateSourceTab(tabID, (current) => ({
        browsers: {
          ...current.browsers,
          [side]: {
            ...current.browsers[side],
            loading: false,
            error: err?.message || String(err)
          }
        }
      }));
    }
  }

  async function openBrowserEntry(side, entry) {
    if (entry.type === 'folder') {
      await loadBrowser(side, entry.path);
    } else if (entry.type === 'file') {
      await selectFileSource(side, entry.path);
    }
  }

  function clearSource(side) {
    updateSourceTab(activeTabID, (tab) => {
      const nextLeft = side === 'left' ? null : tab.leftSource;
      const nextRight = side === 'right' ? null : tab.rightSource;
      const nextLock = nextLeft || nextRight ? (nextLeft || nextRight).type : '';
      return {
        leftSource: nextLeft,
        rightSource: nextRight,
        sourceLock: nextLock,
        title: sourceTitle(nextLeft, nextRight, nextLock),
        browsers: { ...tab.browsers, [side]: emptyBrowser() }
      };
    });
  }

  async function selectWithNativeDialog(side, type) {
    const tabID = activeTabID;
    const tab = getTab(tabID);
    if (!canSelectTypeFor(tab, type)) return;
    try {
      const api = backend();
      const path = type === 'folder' ? await api.ChooseDirectory() : await api.ChooseFile();
      if (path) {
        if (type === 'folder') {
          const patch = selectSource(tabID, side, path, 'folder');
          if (patch) {
            await loadBrowser(side, path, tabID);
            if (patch.leftSource?.type === 'folder' && patch.rightSource?.type === 'folder') {
              await openFolderComparison({ left: patch.leftSource.path, right: patch.rightSource.path }, { tabID });
            }
          }
        } else {
          activeTabID = tabID;
          await selectFileSource(side, path);
        }
      }
    } catch (err) {
      error = err?.message || String(err);
    }
  }

  function paneHeader(side) {
    const source = side === 'left' ? leftSource : rightSource;
    if (source?.type === 'file') return source.path;
    if (browsers[side].mode === 'folder' && browsers[side].path) return browsers[side].path;
    return side === 'left' ? 'Left' : 'Right';
  }

  function syncComparisonScroll(event) {
    const scrollTop = event.currentTarget.scrollTop;
    if (leftComparePane) leftComparePane.scrollTop = scrollTop;
    if (rightComparePane) rightComparePane.scrollTop = scrollTop;
    updateMapViewport(event.currentTarget);
  }

  function scrollComparisonFromSide(event) {
    if (!centerComparePane || Math.abs(event.deltaY) === 0) return;
    event.preventDefault();
    centerComparePane.scrollTop += event.deltaY;
    if (leftComparePane) leftComparePane.scrollTop = centerComparePane.scrollTop;
    if (rightComparePane) rightComparePane.scrollTop = centerComparePane.scrollTop;
    updateMapViewport(centerComparePane);
  }

  function updateMapViewport(pane) {
    mapScrollTop = pane?.scrollTop || 0;
    mapViewportHeight = pane?.clientHeight || 0;
  }

  function indicator(status) {
    return {
      equal: '=',
      different: '≠',
      changed: '≠',
      left_only: '→',
      right_only: '←',
      type_mismatch: '!',
      error: '!',
      unknown: '?'
    }[status] || '';
  }

  function typeLabel(type) {
    return {
      file: 'file',
      folder: 'folder',
      symlink: 'link',
      other: 'other'
    }[type] || '';
  }

  function lineSegments(row, side) {
    const segments = side === 'left' ? row.leftSegments : row.rightSegments;
    if (segments?.length) return segments;
    const text = side === 'left' ? row.leftText : row.rightText;
    return text ? [{ text, isDiffToken: row.status !== 'equal', changed: row.status !== 'equal' }] : [];
  }

  function segmentIsDiff(segment) {
    return segment.isDiffToken ?? segment.changed;
  }

  function semanticState(row, side) {
    const state = side === 'left' ? row.leftSemanticState : row.rightSemanticState;
    if (state) return state;
    if (row.status === 'left_only') return side === 'left' ? 'IMPORTANT_DIFF' : 'ORPHAN_GAP';
    if (row.status === 'right_only') return side === 'right' ? 'IMPORTANT_DIFF' : 'ORPHAN_GAP';
    if (row.status === 'equal') return 'MATCH';
    return 'IMPORTANT_DIFF';
  }

  function semanticClass(row, side) {
    return `semantic-${semanticState(row, side).toLowerCase()}`;
  }

  function mapColor(row) {
    if (row.status === 'different' || row.status === 'left_only' || row.status === 'right_only' || row.status === 'type_mismatch' || row.status === 'error') return '#A00000';
    if (!row.leftSemanticState && !row.rightSemanticState) return '#FFFFFF';
    if (row.leftSemanticState === 'IMPORTANT_DIFF' || row.rightSemanticState === 'IMPORTANT_DIFF') return '#A00000';
    if (row.leftSemanticState === 'UNIMPORTANT_DIFF' || row.rightSemanticState === 'UNIMPORTANT_DIFF') return '#004080';
    if (row.leftSemanticState === 'ORPHAN_GAP' && row.rightSemanticState === 'ORPHAN_GAP') return '#999999';
    return '#FFFFFF';
  }

  function viewportWindowStyle(tab) {
    const totalRows = tab?.result?.rows?.length || 0;
    if (!totalRows || !mapViewportHeight) return 'top: 0%; height: 100%;';
    const visibleRows = mapViewportHeight / fileRowHeight;
    const top = Math.min(100, (mapScrollTop / fileRowHeight / totalRows) * 100);
    const height = Math.max(6, Math.min(100, (visibleRows / totalRows) * 100));
    return `top: ${top}%; height: ${height}%;`;
  }

  function focusIndicatorStyle(tab) {
    const totalRows = tab?.result?.rows?.length || 0;
    if (!totalRows) return 'display: none;';
    const focusRow = tab?.mode === 'folder' ? tab.selectedRow : selectedFileRange(tab)?.start;
    if (focusRow === null || focusRow === undefined) return 'display: none;';
    const top = Math.min(100, (focusRow / totalRows) * 100);
    return `top: ${top}%;`;
  }

  function selectFolderRow(tab, rowIndex) {
    updateTab(tab.id, { selectedRow: rowIndex });
  }

  function entryIsFile(type) {
    return String(type || '').trim().toLowerCase() === 'file';
  }

  function canOpenFolderFile(row) {
    const backendAllowsCompare = row.canCompareFiles === true || row.CanCompareFiles === true;
    const inferredFilePair = entryIsFile(row.leftType) && entryIsFile(row.rightType);
    return Boolean(row.leftPath && row.rightPath && (backendAllowsCompare || inferredFilePair));
  }

  async function openFolderFile(row) {
    if (!canOpenFolderFile(row)) {
      error = 'This row does not have matching files to compare.';
      return;
    }
    hideContext();
    await openFileComparison({ left: row.leftPath, right: row.rightPath });
  }

  function canCopyFolderFile(row, direction) {
    if (direction === 'ltr') {
      return entryIsFile(row.leftType) && (!row.rightExists || entryIsFile(row.rightType)) && row.leftPath && row.rightPath;
    }
    return entryIsFile(row.rightType) && (!row.leftExists || entryIsFile(row.leftType)) && row.leftPath && row.rightPath;
  }

  function canRevealFolderSide(row, side) {
    return side === 'left' ? row.leftExists && row.leftPath : row.rightExists && row.rightPath;
  }

  function hasFolderMenuActions(row) {
    return (
      canOpenFolderFile(row) ||
      canCopyFolderFile(row, 'ltr') ||
      canCopyFolderFile(row, 'rtl') ||
      canRevealFolderSide(row, 'left') ||
      canRevealFolderSide(row, 'right')
    );
  }

  function keySelectFolder(event, tab, rowIndex, row) {
    if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault();
      selectFolderRow(tab, rowIndex);
    }
    if (event.key === 'Enter' && canOpenFolderFile(row)) {
      openFolderFile(row);
    }
  }

  function selectFileRow(tab, rowIndex, event) {
    let selectionStart = rowIndex;
    let selectionEnd = rowIndex;
    if (event.shiftKey && shiftAnchor !== null) {
      selectionStart = shiftAnchor;
      selectionEnd = rowIndex;
    } else {
      shiftAnchor = rowIndex;
    }
    updateTab(tab.id, { selectionStart, selectionEnd });
  }

  function selectedFileRange(tab) {
    if (tab.selectionStart === null || tab.selectionEnd === null) return null;
    const start = Math.min(tab.selectionStart, tab.selectionEnd);
    const end = Math.max(tab.selectionStart, tab.selectionEnd);
    return { start, end };
  }

  function rowSelected(tab, index) {
    if (tab.mode === 'folder') return tab.selectedRow === index;
    const range = selectedFileRange(tab);
    return range && index >= range.start && index <= range.end;
  }

  function keySelectFile(event, tab, rowIndex) {
    if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault();
      selectFileRow(tab, rowIndex, event);
    }
  }

  const contextMenuMargin = 8;

  async function showContext(event, tab, row, rowIndex, side = '') {
    event.preventDefault();
    contextMenu = {
      x: event.clientX,
      y: event.clientY,
      tabID: tab.id,
      mode: tab.mode,
      side,
      row,
      rowIndex
    };
    if (tab.mode === 'folder') {
      selectFolderRow(tab, rowIndex);
    } else {
      selectFileRow(tab, rowIndex, event);
    }
    await tick();
    fitContextMenuToViewport();
  }

  function fitContextMenuToViewport() {
    if (!contextMenu || !contextMenuElement) return;
    const rect = contextMenuElement.getBoundingClientRect();
    const viewportWidth = window.innerWidth;
    const viewportHeight = window.innerHeight;
    let nextX = contextMenu.x;
    let nextY = contextMenu.y;

    if (nextX + rect.width + contextMenuMargin > viewportWidth) {
      nextX = contextMenu.x - rect.width;
    }
    if (nextY + rect.height + contextMenuMargin > viewportHeight) {
      nextY = contextMenu.y - rect.height;
    }

    nextX = Math.max(contextMenuMargin, Math.min(nextX, viewportWidth - rect.width - contextMenuMargin));
    nextY = Math.max(contextMenuMargin, Math.min(nextY, viewportHeight - rect.height - contextMenuMargin));
    contextMenu = { ...contextMenu, x: nextX, y: nextY };
  }

  function hideContext() {
    contextMenu = null;
  }

  async function copyFolder(direction) {
    if (!contextMenu) return;
    const tab = tabs.find((item) => item.id === contextMenu.tabID);
    const row = contextMenu.row;
    hideContext();
    if (!tab || !row) return;
    const overwrite = direction === 'ltr' ? row.rightExists : row.leftExists;
    if (overwrite && !confirm('Overwrite the destination file?')) return;
    try {
      if (direction === 'ltr') {
        await backend().CopyFileLeftToRight(row.leftPath, row.rightPath, overwrite);
      } else {
        await backend().CopyFileRightToLeft(row.rightPath, row.leftPath, overwrite);
      }
      activeTabID = tab.id;
      await refreshActive();
    } catch (err) {
      failActive(err);
    }
  }

  async function copyLines(direction) {
    const tab = activeTab;
    const range = selectedFileRange(tab);
    hideContext();
    if (!tab || !range) return;
    try {
      const api = backend();
      const result =
        direction === 'ltr'
          ? await api.ApplyLinesLeftToRight(tab.id, range.start, range.end)
          : await api.ApplyLinesRightToLeft(tab.id, range.start, range.end);
      updateTab(tab.id, { result, selectionStart: range.start, selectionEnd: Math.min(range.end, result.rows.length - 1) });
    } catch (err) {
      failActive(err);
    }
  }

  async function copySelectedText() {
    const tab = activeTab;
    const range = selectedFileRange(tab);
    hideContext();
    if (!tab || !range) return;
    const text = tab.result.rows
      .slice(range.start, range.end + 1)
      .map((row) => row.leftText || row.rightText)
      .join('\n');
    await navigator.clipboard?.writeText(text);
  }

  async function reveal(path) {
    hideContext();
    if (!path) return;
    try {
      await backend().RevealPath(path);
    } catch (err) {
      failActive(err);
    }
  }

  createSourceTab();
</script>

<svelte:window on:click={hideContext} />

<main class="app-shell">
  <nav class="tab-bar" aria-label="Open tabs">
    {#each tabs as tab}
      <div class:active={tab.id === activeTabID} class="tab">
        <button class="tab-main" on:click={() => (activeTabID = tab.id)}>
          <span class="dirty">{tabDirty(tab) ? '•' : ''}</span>
          <span>{tab.title}</span>
          <span class="mode">{tabModeLabel(tab)}</span>
        </button>
        <button class="tab-close" aria-label={`Close ${tab.title}`} on:click={() => requestClose(tab)}>×</button>
      </div>
    {/each}
    <button class="new-tab-button" aria-label="New tab" on:click={createSourceTab}>+</button>
  </nav>

  {#if error}
    <div class="error-banner">{error}</div>
  {/if}

  {#if !activeTab}
    <section class="empty-tabs">
      <button class="primary" on:click={createSourceTab}>New tab</button>
    </section>
  {:else if activeTab.mode === 'new'}
    <section class="source-picker">
      <div class="browser-pair">
        <section class="browser-pane" aria-label="Left source browser">
          <div class="browser-top">
            <strong class="pane-heading" title={paneHeader('left')}>{paneHeader('left')}</strong>
            <span class="selected-source" title={leftSource?.path || ''}>
              {leftSource ? `${leftSource.type}: ${leftSource.path}` : 'No source selected'}
            </span>
            <button on:click={() => clearSource('left')} disabled={!leftSource}>Clear</button>
          </div>
          <div class="browser-buttons">
            <button on:click={() => loadBrowser('left', browsers.left.parent)} disabled={!browsers.left.parent}>Up</button>
            <button on:click={() => selectFolderSource('left', browsers.left.path)} disabled={!canSelectType('folder') || !browsers.left.path}>Select current folder</button>
            <button on:click={() => selectWithNativeDialog('left', 'folder')} disabled={sourceLock === 'file'}>Choose folder</button>
            <button on:click={() => selectWithNativeDialog('left', 'file')} disabled={sourceLock === 'folder'}>Choose file</button>
          </div>
          <div class="browser-path" title={browsers.left.path}>{browsers.left.path}</div>
          {#if browsers.left.error}
            <div class="browser-error">{browsers.left.error}</div>
          {:else if browsers.left.loading}
            <div class="browser-loading">Loading…</div>
          {:else if browsers.left.mode === 'empty'}
            <div class="blank-pane">Waiting for a file or folder.</div>
          {:else if browsers.left.mode === 'file'}
            <div class="file-preview-panel">
              {#if browsers.left.warning}
                <div class="browser-warning">{browsers.left.warning}</div>
              {/if}
              <div class="preview-list">
                {#each browsers.left.lines as line, index}
                  <div class="preview-line">
                    <span class="line-number">{index + 1}</span>
                    <code>{line}</code>
                  </div>
                {/each}
              </div>
            </div>
          {:else}
            <div class="browser-list">
              {#each browsers.left.entries as entry}
                <div
                  class:selected-entry={leftSource?.path === entry.path}
                  class:locked-entry={!canSelectType(entry.type)}
                  class="browser-entry"
                >
                  <button
                    class="entry-main"
                    on:click={() => openBrowserEntry('left', entry)}
                    on:dblclick={() => entry.type === 'folder' && loadBrowser('left', entry.path)}
                  >
                    <span class="entry-type">{typeLabel(entry.type)}</span>
                    <span>{entry.name}</span>
                  </button>
                  <button
                    on:click={() => entry.type === 'folder' ? selectFolderSource('left', entry.path) : selectFileSource('left', entry.path)}
                    disabled={!canSelectType(entry.type)}
                  >Select</button>
                </div>
              {/each}
            </div>
          {/if}
        </section>

        <section class="browser-pane" aria-label="Right source browser">
          <div class="browser-top">
            <strong class="pane-heading" title={paneHeader('right')}>{paneHeader('right')}</strong>
            <span class="selected-source" title={rightSource?.path || ''}>
              {rightSource ? `${rightSource.type}: ${rightSource.path}` : 'No source selected'}
            </span>
            <button on:click={() => clearSource('right')} disabled={!rightSource}>Clear</button>
          </div>
          <div class="browser-buttons">
            <button on:click={() => loadBrowser('right', browsers.right.parent)} disabled={!browsers.right.parent}>Up</button>
            <button on:click={() => selectFolderSource('right', browsers.right.path)} disabled={!canSelectType('folder') || !browsers.right.path}>Select current folder</button>
            <button on:click={() => selectWithNativeDialog('right', 'folder')} disabled={sourceLock === 'file'}>Choose folder</button>
            <button on:click={() => selectWithNativeDialog('right', 'file')} disabled={sourceLock === 'folder'}>Choose file</button>
          </div>
          <div class="browser-path" title={browsers.right.path}>{browsers.right.path}</div>
          {#if browsers.right.error}
            <div class="browser-error">{browsers.right.error}</div>
          {:else if browsers.right.loading}
            <div class="browser-loading">Loading…</div>
          {:else if browsers.right.mode === 'empty'}
            <div class="blank-pane">Waiting for a file or folder.</div>
          {:else if browsers.right.mode === 'file'}
            <div class="file-preview-panel">
              {#if browsers.right.warning}
                <div class="browser-warning">{browsers.right.warning}</div>
              {/if}
              <div class="preview-list">
                {#each browsers.right.lines as line, index}
                  <div class="preview-line">
                    <span class="line-number">{index + 1}</span>
                    <code>{line}</code>
                  </div>
                {/each}
              </div>
            </div>
          {:else}
            <div class="browser-list">
              {#each browsers.right.entries as entry}
                <div
                  class:selected-entry={rightSource?.path === entry.path}
                  class:locked-entry={!canSelectType(entry.type)}
                  class="browser-entry"
                >
                  <button
                    class="entry-main"
                    on:click={() => openBrowserEntry('right', entry)}
                    on:dblclick={() => entry.type === 'folder' && loadBrowser('right', entry.path)}
                  >
                    <span class="entry-type">{typeLabel(entry.type)}</span>
                    <span>{entry.name}</span>
                  </button>
                  <button
                    on:click={() => entry.type === 'folder' ? selectFolderSource('right', entry.path) : selectFileSource('right', entry.path)}
                    disabled={!canSelectType(entry.type)}
                  >Select</button>
                </div>
              {/each}
            </div>
          {/if}
        </section>
      </div>
    </section>
  {:else}
    <section class="comparison">
      <div class="path-strip">
        <span title={activeTab.leftPath}>{activeTab.leftPath}</span>
        <span title={activeTab.rightPath}>{activeTab.rightPath}</span>
      </div>

      {#if activeTab.loading}
        <div class="loading">Comparing…</div>
      {:else if activeTab.error}
        <div class="error-panel">{activeTab.error}</div>
      {:else if activeTab.mode === 'folder'}
        <div class="split-grid-header">
          <div>Left folder</div>
          <div>Status</div>
          <div>Right folder</div>
        </div>
        <div class="folder-compare-body">
          <div class="split-rows">
            <div class="split-pane side-pane folder-side-pane" bind:this={leftComparePane} on:wheel={scrollComparisonFromSide}>
              {#each activeTab.result?.rows || [] as row, index}
                <div
                  role="button"
                  tabindex="0"
                  class:selected={rowSelected(activeTab, index)}
                  class:odd-row={index % 2 === 1}
                  class={`compare-side-row folder-cell status-${row.status}`}
                  on:click={() => selectFolderRow(activeTab, index)}
                  on:keydown={(event) => keySelectFolder(event, activeTab, index, row)}
                  on:dblclick={() => openFolderFile(row)}
                  on:contextmenu={(event) => showContext(event, activeTab, row, index, 'left')}
                >
                  {#if row.leftExists}
                    <span class="entry-name">{row.name}</span>
                    <span class="entry-type">{typeLabel(row.leftType)}</span>
                  {/if}
                </div>
              {/each}
            </div>
            <div class="split-pane center-scroll-pane folder-center-pane" bind:this={centerComparePane} on:scroll={syncComparisonScroll}>
              {#each activeTab.result?.rows || [] as row, index}
                <div
                  role="button"
                  tabindex="0"
                  class:selected={rowSelected(activeTab, index)}
                  class:odd-row={index % 2 === 1}
                  class={`compare-status-row status-${row.status}`}
                  title={row.error || row.status}
                  on:click={() => selectFolderRow(activeTab, index)}
                  on:keydown={(event) => keySelectFolder(event, activeTab, index, row)}
                  on:contextmenu={(event) => showContext(event, activeTab, row, index, 'center')}
                >
                  {indicator(row.status)}
                </div>
              {/each}
            </div>
            <div class="split-pane side-pane folder-side-pane" bind:this={rightComparePane} on:wheel={scrollComparisonFromSide}>
              {#each activeTab.result?.rows || [] as row, index}
                <div
                  role="button"
                  tabindex="0"
                  class:selected={rowSelected(activeTab, index)}
                  class:odd-row={index % 2 === 1}
                  class={`compare-side-row folder-cell status-${row.status}`}
                  on:click={() => selectFolderRow(activeTab, index)}
                  on:keydown={(event) => keySelectFolder(event, activeTab, index, row)}
                  on:dblclick={() => openFolderFile(row)}
                  on:contextmenu={(event) => showContext(event, activeTab, row, index, 'right')}
                >
                  {#if row.rightExists}
                    <span class="entry-name">{row.name}</span>
                    <span class="entry-type">{typeLabel(row.rightType)}</span>
                  {/if}
                </div>
              {/each}
            </div>
          </div>
          <div class="diff-map-gutter" aria-label="Folder difference overview">
            {#each activeTab.result?.rows || [] as row}
              <div class="diff-map-pixel" style={`background: ${mapColor(row)}`}></div>
            {/each}
            <div class="diff-map-window" style={viewportWindowStyle(activeTab)}></div>
            <div class="diff-map-focus" style={focusIndicatorStyle(activeTab)}></div>
          </div>
        </div>
      {:else}
        {#if activeTab.result?.warning}
          <div class="warning-banner">{activeTab.result.warning}</div>
        {/if}
        <div class="split-grid-header">
          <div>Left file {activeTab.result?.leftDirty ? '•' : ''}</div>
          <div>Status</div>
          <div>Right file {activeTab.result?.rightDirty ? '•' : ''}</div>
        </div>
        <div class="file-compare-body">
          <div class="split-rows">
            <div class="split-pane side-pane file-side-pane" bind:this={leftComparePane} on:wheel={scrollComparisonFromSide}>
              {#each activeTab.result?.rows || [] as row, index}
                <div
                  role="button"
                  tabindex="0"
                  class:selected={rowSelected(activeTab, index)}
                  class:odd-row={index % 2 === 1}
                  class={`compare-side-row line-cell status-${row.status} ${semanticClass(row, 'left')}`}
                  on:click={(event) => selectFileRow(activeTab, index, event)}
                  on:keydown={(event) => keySelectFile(event, activeTab, index)}
                  on:contextmenu={(event) => showContext(event, activeTab, row, index)}
                >
                  <span class="line-number">{row.leftLineNumber || ''}</span>
                  <code>{#each lineSegments(row, 'left') as segment}<span class:diff-token={segmentIsDiff(segment)}>{segment.text}</span>{/each}</code>
                </div>
              {/each}
            </div>
            <div class="split-pane center-scroll-pane file-center-pane" bind:this={centerComparePane} on:scroll={syncComparisonScroll}>
              {#each activeTab.result?.rows || [] as row, index}
                <div
                  role="button"
                  tabindex="0"
                  class:selected={rowSelected(activeTab, index)}
                  class:odd-row={index % 2 === 1}
                  class={`compare-status-row status-${row.status} semantic-${(row.semanticState || 'MATCH').toLowerCase()}`}
                  on:click={(event) => selectFileRow(activeTab, index, event)}
                  on:keydown={(event) => keySelectFile(event, activeTab, index)}
                  on:contextmenu={(event) => showContext(event, activeTab, row, index)}
                >
                  {indicator(row.status)}
                </div>
              {/each}
            </div>
            <div class="split-pane side-pane file-side-pane" bind:this={rightComparePane} on:wheel={scrollComparisonFromSide}>
              {#each activeTab.result?.rows || [] as row, index}
                <div
                  role="button"
                  tabindex="0"
                  class:selected={rowSelected(activeTab, index)}
                  class:odd-row={index % 2 === 1}
                  class={`compare-side-row line-cell status-${row.status} ${semanticClass(row, 'right')}`}
                  on:click={(event) => selectFileRow(activeTab, index, event)}
                  on:keydown={(event) => keySelectFile(event, activeTab, index)}
                  on:contextmenu={(event) => showContext(event, activeTab, row, index)}
                >
                  <span class="line-number">{row.rightLineNumber || ''}</span>
                  <code>{#each lineSegments(row, 'right') as segment}<span class:diff-token={segmentIsDiff(segment)}>{segment.text}</span>{/each}</code>
                </div>
              {/each}
            </div>
          </div>
          <div class="diff-map-gutter" aria-label="Difference overview">
            {#each activeTab.result?.rows || [] as row}
              <div class="diff-map-pixel" style={`background: ${mapColor(row)}`}></div>
            {/each}
            <div class="diff-map-window" style={viewportWindowStyle(activeTab)}></div>
            <div class="diff-map-focus" style={focusIndicatorStyle(activeTab)}></div>
          </div>
        </div>
      {/if}
    </section>
  {/if}

  {#if contextMenu}
    <div
      bind:this={contextMenuElement}
      class="context-menu"
      role="menu"
      tabindex="-1"
      style={`left:${contextMenu.x}px;top:${contextMenu.y}px`}
      on:pointerdown|stopPropagation
    >
      {#if contextMenu.mode === 'folder'}
        {#if canCopyFolderFile(contextMenu.row, 'ltr')}
          <button on:click={() => copyFolder('ltr')}>Copy left to right</button>
        {/if}
        {#if canCopyFolderFile(contextMenu.row, 'rtl')}
          <button on:click={() => copyFolder('rtl')}>Copy right to left</button>
        {/if}
        {#if canOpenFolderFile(contextMenu.row)}
          <button on:click={() => openFolderFile(contextMenu.row)}>Compare files</button>
        {/if}
        {#if canRevealFolderSide(contextMenu.row, 'left')}
          <button on:click={() => reveal(contextMenu.row.leftPath)}>Show left in folder</button>
        {/if}
        {#if canRevealFolderSide(contextMenu.row, 'right')}
          <button on:click={() => reveal(contextMenu.row.rightPath)}>Show right in folder</button>
        {/if}
        {#if !hasFolderMenuActions(contextMenu.row)}
          <button disabled>No file actions</button>
        {/if}
      {:else}
        <button on:click={() => copyLines('ltr')}>Copy selected line(s) left to right</button>
        <button on:click={() => copyLines('rtl')}>Copy selected line(s) right to left</button>
        <button on:click={copySelectedText}>Copy text</button>
        <button on:click={() => reveal(activeTab.leftPath)}>Show left file in folder</button>
        <button on:click={() => reveal(activeTab.rightPath)}>Show right file in folder</button>
      {/if}
    </div>
  {/if}

  {#if pendingClose}
    <div class="dialog-backdrop">
      <div class="dialog">
        <h2>Unsaved changes</h2>
        <p>This comparison has unsaved changes. Save, discard, or cancel?</p>
        <div class="dialog-actions">
          <button on:click={closeWithSave}>Save</button>
          <button on:click={closeWithDiscard}>Discard</button>
          <button on:click={() => (pendingClose = null)}>Cancel</button>
        </div>
      </div>
    </div>
  {/if}
</main>
