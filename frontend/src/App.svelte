<script>
  import { onDestroy, onMount, tick } from 'svelte';
  import { EventsOn } from '../wailsjs/runtime/runtime';
  import DiffEditor from './DiffEditor.svelte';
  import { formatStructuredText } from './structuredText.js';

  const appApi = () => window.go?.main?.App;

  let tabs = [];
  let activeTabID = '';
  let nextID = 1;
  let error = '';
  let contextMenu = null;
  let contextMenuElement;
  let pendingClose = null;
  let textCompareRequests = {};
  let fileEditTimers = {};
  let shiftAnchor = null;
  let leftComparePane;
  let centerComparePane;
  let rightComparePane;
  let diffEditor;
  let mapScrollTop = 0;
  let mapViewportHeight = 0;
  let unsubscribeFolderUpdates = null;
  let unsubscribeFileChanges = null;
  const fileRowHeight = 28;
  const emptyBrowser = () => ({
    mode: 'empty',
    path: '',
    parent: '',
    entries: [],
    expanded: {},
    loadedFolders: {},
    loadingFolders: {},
    lines: [],
    warning: '',
    loading: false,
    error: ''
  });
  const emptyTextComparison = () => ({
    left: '',
    right: '',
    leftPath: '',
    rightPath: '',
    leftSaved: '',
    rightSaved: '',
    result: null,
    loading: false,
    error: ''
  });
  const emptySourceState = () => ({
    sourceLock: '',
    leftSource: null,
    rightSource: null,
    textComparison: emptyTextComparison(),
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

  function textTitle(left, right) {
    return left || right ? 'Text comparison' : 'New comparison';
  }

  function tabDirty(tab) {
    if (tab?.mode === 'file') return Boolean(tab?.result?.leftDirty || tab?.result?.rightDirty);
    return textComparisonDirty(tab);
  }

  function tabModeLabel(tab) {
    if (tab?.mode === 'new' && hasTextComparison(tab)) return 'text';
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
      selectionStart: null,
      selectionEnd: null,
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
        selectedRow: null,
        expanded: {},
        loadedFolders: {},
        loadingFolders: {}
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
        selectionEnd: null,
        externalChanges: {}
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
        updateTab(activeTab.id, {
          result,
          loading: false,
          selectedRow: null,
          expanded: {},
          loadedFolders: {},
          loadingFolders: {}
        });
      } else {
        const result = await backend().RefreshFileComparison(activeTab.id);
        updateTab(activeTab.id, { result, loading: false });
      }
    } catch (err) {
      failActive(err);
    }
  }

  function folderProgress(tab) {
    const rows = tab?.result?.rows || [];
    const completed = rows.filter((row) => row.status !== 'pending').length;
    return { completed, total: rows.length, pending: rows.length - completed };
  }

  function folderComparisonRunning(tab) {
    return tab?.mode === 'folder' && folderProgress(tab).pending > 0;
  }

  function refreshLabel(tab) {
    if (!folderComparisonRunning(tab)) return 'Refresh';
    const progress = folderProgress(tab);
    return `Comparing ${progress.completed}/${progress.total}`;
  }

  async function saveActive(side = 'both') {
    const tab = activeTab;
    if (!tab || tab.mode !== 'file') return;
    error = '';
    try {
      const result = await backend().SaveFileComparison(tab.id, side);
      updateTab(tab.id, (current) => {
        const externalChanges = { ...(current.externalChanges || {}) };
        if (side === 'both' || side === 'left') delete externalChanges.left;
        if (side === 'both' || side === 'right') delete externalChanges.right;
        return { result, externalChanges };
      });
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
    if (tabDirty(tab)) {
      pendingClose = tab;
      return;
    }
    closeTab(tab.id);
  }

  async function closeWithSave() {
    if (!pendingClose) return;
    const tabID = pendingClose.id;
    const tab = pendingClose;
    pendingClose = null;
    try {
      if (tab.mode === 'new') {
        const saved = await saveTextComparison(tabID, 'both', { closeAfter: true });
        if (!saved) {
          pendingClose = tab;
          return;
        }
      } else {
        await backend().SaveFileComparison(tabID, 'both');
      }
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
    tabs = tabs.map((tab) => {
      if (tab.id !== tabID) return tab;
      const nextPatch = typeof patch === 'function' ? patch(tab) : patch;
      return { ...tab, ...nextPatch };
    });
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

  function browserTreeEntries(entries, parentID = '', depth = 0) {
    return (entries || []).map((entry) => ({
      ...entry,
      id: entry.path,
      parentID,
      depth,
      hasChildren: entry.type === 'folder'
    }));
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
            entries: browserTreeEntries(listing.entries || []),
            expanded: {},
            loadedFolders: {},
            loadingFolders: {},
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

  function canUseTextComparison(tab) {
    return tab?.mode === 'new' && !tab.sourceLock && !tab.leftSource && !tab.rightSource;
  }

  function hasTextComparison(tab) {
    const text = tab?.textComparison;
    return Boolean(text?.left || text?.right || text?.result || text?.loading || text?.error);
  }

  function textSideDirty(tab, side) {
    const text = tab?.textComparison;
    if (!text) return false;
    const value = text[side] || '';
    const path = text[`${side}Path`] || '';
    const saved = text[`${side}Saved`] || '';
    return path ? value !== saved : value !== '';
  }

  function textComparisonDirty(tab) {
    return tab?.mode === 'new' && (textSideDirty(tab, 'left') || textSideDirty(tab, 'right'));
  }

  function textSideNeedsSave(tab, side) {
    const text = tab?.textComparison;
    if (!text) return false;
    return text[`${side}Path`] ? textSideDirty(tab, side) : hasTextComparison(tab);
  }

  function comparisonRows(tab) {
    return tab?.result?.rows || tab?.textComparison?.result?.rows || [];
  }

  function rowIsDifference(row) {
    if (!row) return false;
    if (row.status && row.status !== 'equal') return true;
    return [row.semanticState, row.leftSemanticState, row.rightSemanticState].some(
      (state) => state === 'IMPORTANT_DIFF' || state === 'UNIMPORTANT_DIFF' || state === 'ORPHAN_GAP'
    );
  }

  function differenceGroups(tab) {
    const groups = [];
    let current = null;
    const rows = comparisonRows(tab);
    for (let index = 0; index < rows.length; index++) {
      if (!rowIsDifference(rows[index])) {
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

  function differenceCount(tab) {
    return differenceGroups(tab).length;
  }

  function differenceLabel(tab) {
    const count = differenceCount(tab);
    return `${count} difference${count === 1 ? '' : 's'}`;
  }

  function navigateDifference(direction) {
    diffEditor?.goToDifference(direction);
  }

  function updateTextSource(tabID, side, value) {
    const tab = getTab(tabID);
    if (!canUseTextComparison(tab)) return;
    updateSourceTab(tabID, (current) => {
      const nextText = { ...current.textComparison, [side]: value, error: '' };
      const hasText = Boolean(nextText.left || nextText.right);
      return {
        title: textTitle(nextText.left, nextText.right),
        selectionStart: null,
        selectionEnd: null,
        textComparison: {
          ...nextText,
          result: hasText ? nextText.result : null,
          loading: hasText
        }
      };
    });
    runTextComparison(tabID);
  }

  function beautifyText(tab, side) {
    if (!canUseTextComparison(tab)) return;
    const value = tab.textComparison?.[side] || '';
    try {
      const formatted = formatStructuredText(value);
      updateTextSource(tab.id, side, formatted.text);
    } catch (err) {
      const requestID = (textCompareRequests[tab.id] || 0) + 1;
      textCompareRequests = { ...textCompareRequests, [tab.id]: requestID };
      updateSourceTab(tab.id, (current) => ({
        textComparison: {
          ...current.textComparison,
          loading: false,
          error: err?.message || String(err)
        }
      }));
    }
  }

  async function runTextComparison(tabID) {
    const tab = getTab(tabID);
    if (!canUseTextComparison(tab)) return;

    const left = tab.textComparison?.left || '';
    const right = tab.textComparison?.right || '';
    if (!left && !right) {
      updateSourceTab(tabID, (current) => ({
        textComparison: { ...current.textComparison, result: null, loading: false, error: '' }
      }));
      return;
    }

    const requestID = (textCompareRequests[tabID] || 0) + 1;
    textCompareRequests = { ...textCompareRequests, [tabID]: requestID };
    try {
      const result = await backend().CompareText(left, right);
      const current = getTab(tabID);
      if (
        textCompareRequests[tabID] !== requestID ||
        !canUseTextComparison(current) ||
        current.textComparison.left !== left ||
        current.textComparison.right !== right
      ) {
        return;
      }
      updateSourceTab(tabID, (latest) => ({
        textComparison: { ...latest.textComparison, result, loading: false, error: '' }
      }));
    } catch (err) {
      if (textCompareRequests[tabID] !== requestID) return;
      updateSourceTab(tabID, (current) => ({
        textComparison: {
          ...current.textComparison,
          loading: false,
          error: err?.message || String(err)
        }
      }));
    }
  }

  function defaultTextFilename(side) {
    return side === 'left' ? 'left.txt' : 'right.txt';
  }

  async function saveTextSide(tabID, side) {
    const tab = getTab(tabID);
    if (!canUseTextComparison(tab)) return false;
    const text = tab.textComparison;
    const value = text[side] || '';
    let path = text[`${side}Path`] || '';
    if (!path) {
      path = await backend().ChooseSaveFile(defaultTextFilename(side));
      if (!path) return false;
    }

    await backend().WriteTextFile(path, value);
    updateSourceTab(tabID, (current) => ({
      textComparison: {
        ...current.textComparison,
        [`${side}Path`]: path,
        [`${side}Saved`]: value,
        error: ''
      }
    }));
    return true;
  }

  async function saveTextComparison(tabID, side = 'both', options = {}) {
    const tab = getTab(tabID);
    if (!canUseTextComparison(tab)) return false;
    error = '';
    try {
      const sides = side === 'both' ? ['left', 'right'] : [side];
      for (const nextSide of sides) {
        const current = getTab(tabID);
        if (textSideNeedsSave(current, nextSide)) {
          const saved = await saveTextSide(tabID, nextSide);
          if (!saved) return false;
        }
      }

      const savedTab = getTab(tabID);
      const leftPath = savedTab?.textComparison?.leftPath;
      const rightPath = savedTab?.textComparison?.rightPath;
      if (options.closeAfter) return true;
      if (leftPath && rightPath && !textComparisonDirty(savedTab)) {
        await openFileComparison({ left: leftPath, right: rightPath }, { tabID });
      }
      return true;
    } catch (err) {
      updateSourceTab(tabID, (current) => ({
        textComparison: {
          ...current.textComparison,
          loading: false,
          error: err?.message || String(err)
        }
      }));
      return false;
    }
  }

  function sideTextFromRows(result, side) {
    const indexKey = side === 'left' ? 'leftIndex' : 'rightIndex';
    const textKey = side === 'left' ? 'leftText' : 'rightText';
    return (result?.rows || [])
      .filter((row) => row[indexKey] !== undefined && row[indexKey] !== null)
      .sort((a, b) => a[indexKey] - b[indexKey])
      .map((row) => row[textKey] || '');
  }

  function sideTextValue(tab, side) {
    if (tab?.mode === 'file') return sideTextFromRows(tab.result, side).join('\n');
    if (tab?.mode === 'new') return tab.textComparison?.[side] || '';
    return '';
  }

  function updateFileSideText(tabID, side, text) {
    const key = `${tabID}:${side}`;
    clearTimeout(fileEditTimers[key]);
    fileEditTimers[key] = setTimeout(async () => {
      try {
        const result = await backend().UpdateFileComparisonText(tabID, side, text);
        updateTab(tabID, { result });
      } catch (err) {
        failTab(tabID, err);
      }
    }, 150);
  }

  function updateEditorSource(tab, side, value) {
    if (!tab) return;
    if (tab.mode === 'new') {
      updateTextSource(tab.id, side, value);
    } else if (tab.mode === 'file') {
      updateFileSideText(tab.id, side, value);
    }
  }

  function selectEditorRange(tab, range) {
    if (!tab || !range) return;
    updateTab(tab.id, { selectionStart: range.start, selectionEnd: range.end });
  }

  function updateEditorViewport(viewport) {
    mapScrollTop = viewport?.scrollTop || 0;
    mapViewportHeight = viewport?.clientHeight || 0;
  }

  async function showEditorContext(tab, details) {
    if (!tab || tab.mode !== 'file' || !details?.row) return;
    contextMenu = {
      x: details.x,
      y: details.y,
      tabID: tab.id,
      mode: tab.mode,
      side: details.side,
      row: details.row,
      rowIndex: details.rowIndex
    };
    updateTab(tab.id, { selectionStart: details.rowIndex, selectionEnd: details.rowIndex });
    await tick();
    fitContextMenuToViewport();
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
      title: sourceTitle(nextLeft, nextRight, type),
      textComparison: emptyTextComparison()
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
      await toggleBrowserFolder(side, entry);
    } else if (entry.type === 'file') {
      await selectFileSource(side, entry.path);
    }
  }

  function browserEntryExpanded(browser, entry) {
    return Boolean(browser?.expanded?.[entry.id]);
  }

  function browserIndent(entry) {
    return `${Math.max(0, entry.depth || 0) * 18}px`;
  }

  function visibleBrowserEntries(browser) {
    const expanded = browser?.expanded || {};
    const visibleByID = {};
    const visible = [];
    for (const entry of browser?.entries || []) {
      const parentVisible = !entry.parentID || (visibleByID[entry.parentID] && expanded[entry.parentID]);
      visibleByID[entry.id] = parentVisible;
      if (parentVisible) visible.push(entry);
    }
    return visible;
  }

  function descendantEndIndex(entries, entry) {
    const start = entries.findIndex((item) => item.id === entry.id);
    if (start === -1) return entries.length - 1;
    let end = start;
    for (let index = start + 1; index < entries.length; index++) {
      if ((entries[index].depth || 0) <= (entry.depth || 0)) break;
      end = index;
    }
    return end;
  }

  async function toggleBrowserFolder(side, entry) {
    if (entry.type !== 'folder') return;
    const tabID = activeTabID;
    const tab = getTab(tabID);
    if (tab?.mode !== 'new') return;
    const browser = tab.browsers?.[side];
    const nextExpanded = !browserEntryExpanded(browser, entry);
    updateSourceTab(tabID, (current) => ({
      browsers: {
        ...current.browsers,
        [side]: {
          ...current.browsers[side],
          expanded: { ...(current.browsers[side].expanded || {}), [entry.id]: nextExpanded }
        }
      }
    }));
    if (!nextExpanded || browser?.loadedFolders?.[entry.id] || browser?.loadingFolders?.[entry.id]) return;

    updateSourceTab(tabID, (current) => ({
      browsers: {
        ...current.browsers,
        [side]: {
          ...current.browsers[side],
          loadingFolders: { ...(current.browsers[side].loadingFolders || {}), [entry.id]: true }
        }
      }
    }));
    try {
      const listing = await backend().ListDirectory(entry.path);
      updateSourceTab(tabID, (current) => {
        const browser = current.browsers[side];
        if (browser.loadedFolders?.[entry.id]) {
          return {
            browsers: {
              ...current.browsers,
              [side]: {
                ...browser,
                loadingFolders: { ...(browser.loadingFolders || {}), [entry.id]: false }
              }
            }
          };
        }
        const childEntries = browserTreeEntries(listing.entries || [], entry.id, (entry.depth || 0) + 1);
        const insertAt = descendantEndIndex(browser.entries || [], entry) + 1;
        const nextEntries = [...(browser.entries || [])];
        nextEntries.splice(insertAt, 0, ...childEntries);
        return {
          browsers: {
            ...current.browsers,
            [side]: {
              ...browser,
              entries: nextEntries,
              loadedFolders: { ...(browser.loadedFolders || {}), [entry.id]: true },
              loadingFolders: { ...(browser.loadingFolders || {}), [entry.id]: false }
            }
          }
        };
      });
    } catch (err) {
      updateSourceTab(tabID, (current) => ({
        browsers: {
          ...current.browsers,
          [side]: {
            ...current.browsers[side],
            loadingFolders: { ...(current.browsers[side].loadingFolders || {}), [entry.id]: false },
            error: err?.message || String(err)
          }
        }
      }));
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
      pending: '⌛',
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

  function statusLabel(status) {
    return {
      pending: 'Pending',
      equal: 'Equal',
      different: 'Different',
      changed: 'Different',
      left_only: 'Left only',
      right_only: 'Right only',
      type_mismatch: 'Type mismatch',
      error: 'Error',
      unknown: 'Unknown'
    }[status] || status || '';
  }

  function typeLabel(type) {
    return {
      file: 'file',
      folder: 'folder',
      symlink: 'link',
      other: 'other'
    }[type] || '';
  }

  function nodeType(row, side) {
    return side === 'left' ? row.leftType : row.rightType;
  }

  function nodeExists(row, side) {
    return side === 'left' ? row.leftExists : row.rightExists;
  }

  function nodePath(row, side) {
    return side === 'left' ? row.leftPath : row.rightPath;
  }

  function rowIsFolder(row) {
    return row?.leftType === 'folder' || row?.rightType === 'folder';
  }

  function sideIsFolder(row, side) {
    return nodeType(row, side) === 'folder';
  }

  function folderRowExpanded(tab, row) {
    return Boolean(tab?.expanded?.[row.id]);
  }

  function visibleFolderRows(tab) {
    const rows = comparisonRows(tab);
    const expanded = tab?.expanded || {};
    const visibleByID = {};
    const visible = [];
    for (const row of rows) {
      const parentVisible = !row.parentID || (visibleByID[row.parentID] && expanded[row.parentID]);
      visibleByID[row.id] = parentVisible;
      if (parentVisible) visible.push(row);
    }
    return visible;
  }

  function folderIndent(row) {
    return `${Math.max(0, row.depth || 0) * 18}px`;
  }

  async function toggleFolderNode(tab, row, event) {
    event?.stopPropagation();
    if (!rowIsFolder(row)) return;
    const nextExpanded = !folderRowExpanded(tab, row);
    updateTab(tab.id, (current) => ({
      expanded: {
        ...(current.expanded || {}),
        [row.id]: nextExpanded
      }
    }));
    if (!nextExpanded || tab.loadedFolders?.[row.id] || tab.loadingFolders?.[row.id]) return;

    updateTab(tab.id, (current) => ({
      loadingFolders: { ...(current.loadingFolders || {}), [row.id]: true }
    }));
    try {
      const result = await backend().ExpandFolderComparisonNode(tab.id, row.id);
      updateTab(tab.id, (current) => ({
        result,
        loadedFolders: { ...(current.loadedFolders || {}), [row.id]: true },
        loadingFolders: { ...(current.loadingFolders || {}), [row.id]: false }
      }));
    } catch (err) {
      updateTab(tab.id, (current) => ({
        loadingFolders: { ...(current.loadingFolders || {}), [row.id]: false }
      }));
      failTab(tab.id, err);
    }
  }

  function clickFolderSideRow(tab, row, event) {
    selectFolderRow(tab, row.rowIndex);
    if (rowIsFolder(row)) {
      toggleFolderNode(tab, row, event);
    }
  }

  function updateFolderNode(update) {
    if (!update?.tabID || !update.nodeID) return;
    updateTab(update.tabID, (tab) => {
      if (tab.mode !== 'folder' || !tab.result?.rows || update.revision !== tab.result.revision) return {};
      return {
        result: {
          ...tab.result,
          rows: tab.result.rows.map((row) =>
            row.id === update.nodeID ? { ...row, status: update.status, error: update.error || '' } : row
          )
        }
      };
    });
  }

  function updateFileChange(update) {
    if (!update?.tabID || !update.side) return;
    updateTab(update.tabID, (tab) => {
      if (tab.mode !== 'file') return {};
      return {
        externalChanges: {
          ...(tab.externalChanges || {}),
          [update.side]: update.path || (update.side === 'left' ? tab.leftPath : tab.rightPath)
        }
      };
    });
  }

  function externalChangeSides(tab) {
    return Object.keys(tab?.externalChanges || {}).filter((side) => tab.externalChanges[side]);
  }

  function externalChangeMessage(tab) {
    const sides = externalChangeSides(tab);
    if (sides.length === 2) return 'Both files changed on disk.';
    return `${sides[0] === 'left' ? 'The left file' : 'The right file'} changed on disk.`;
  }

  async function reloadExternalChanges(tab) {
    if (!tab || tab.mode !== 'file') return;
    if (tabDirty(tab) && !confirm('Discard local edits and reload both files from disk?')) return;
    error = '';
    updateTab(tab.id, { loading: true, error: '' });
    try {
      const result = await backend().ReloadFileComparison(tab.id);
      updateTab(tab.id, { result, loading: false, externalChanges: {} });
    } catch (err) {
      failTab(tab.id, err);
    }
  }

  function keepExternalEdits(tab) {
    if (!tab) return;
    error = '';
    updateTab(tab.id, { externalChanges: {} });
  }

  async function saveAfterExternalChange(tab) {
    if (!tab || tab.mode !== 'file' || !tabDirty(tab)) return;
    error = '';
    try {
      const result = await backend().SaveFileComparison(tab.id, 'both');
      updateTab(tab.id, { result });
    } catch (err) {
      error = err?.message || String(err);
    }
  }

  function mapColor(row) {
    if (row.status === 'pending') return '#999999';
    if (row.status === 'different' || row.status === 'left_only' || row.status === 'right_only' || row.status === 'type_mismatch' || row.status === 'error') return '#A00000';
    if (!row.leftSemanticState && !row.rightSemanticState) return '#FFFFFF';
    if (row.leftSemanticState === 'IMPORTANT_DIFF' || row.rightSemanticState === 'IMPORTANT_DIFF') return '#A00000';
    if (row.leftSemanticState === 'UNIMPORTANT_DIFF' || row.rightSemanticState === 'UNIMPORTANT_DIFF') return '#004080';
    if (row.leftSemanticState === 'ORPHAN_GAP' && row.rightSemanticState === 'ORPHAN_GAP') return '#999999';
    return '#FFFFFF';
  }

  function viewportWindowStyle(tab) {
    const totalRows = comparisonRows(tab).length;
    if (!totalRows || !mapViewportHeight) return 'top: 0%; height: 100%;';
    const visibleRows = mapViewportHeight / fileRowHeight;
    const top = Math.min(100, (mapScrollTop / fileRowHeight / totalRows) * 100);
    const height = Math.max(6, Math.min(100, (visibleRows / totalRows) * 100));
    return `top: ${top}%; height: ${height}%;`;
  }

  function focusIndicatorStyle(tab) {
    const totalRows = comparisonRows(tab).length;
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
      rowIsFolder(row) ||
      canOpenFolderFile(row) ||
      canCopyFolderFile(row, 'ltr') ||
      canCopyFolderFile(row, 'rtl') ||
      canRevealFolderSide(row, 'left') ||
      canRevealFolderSide(row, 'right')
    );
  }

  async function refreshContextFolder() {
    if (!contextMenu) return;
    const tab = getTab(contextMenu.tabID);
    const row = contextMenu.row;
    hideContext();
    if (!tab || tab.mode !== 'folder' || !rowIsFolder(row)) return;
    updateTab(tab.id, { error: '' });
    try {
      const result = await backend().RefreshFolderComparisonNode(tab.id, row.id);
      updateTab(tab.id, { result });
    } catch (err) {
      failTab(tab.id, err);
    }
  }

  function keySelectFolder(event, tab, rowIndex, row) {
    if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault();
      selectFolderRow(tab, rowIndex);
      if (rowIsFolder(row)) {
        toggleFolderNode(tab, row, event);
        return;
      }
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

  function preventDirtyExit(event) {
    if (!tabs.some(tabDirty)) return;
    event.preventDefault();
    event.returnValue = '';
  }

  onMount(() => {
    unsubscribeFolderUpdates = EventsOn('folder-comparison:update', updateFolderNode);
    unsubscribeFileChanges = EventsOn('file-comparison:changed', updateFileChange);
  });

  onDestroy(() => {
    if (typeof unsubscribeFolderUpdates === 'function') {
      unsubscribeFolderUpdates();
    }
    if (typeof unsubscribeFileChanges === 'function') {
      unsubscribeFileChanges();
    }
  });

  createSourceTab();
</script>

<svelte:window on:beforeunload={preventDirtyExit} on:click={hideContext} />

<main class="app-shell">
  <div class="tab-bar" aria-label="Open tabs" role="tablist">
    {#each tabs as tab}
      <div class:active={tab.id === activeTabID} class="tab">
        <button class="tab-main" role="tab" aria-selected={tab.id === activeTabID} on:click={() => (activeTabID = tab.id)}>
          <span class="dirty">{tabDirty(tab) ? '•' : ''}</span>
          <span>{tab.title}</span>
          <span class="mode">{tabModeLabel(tab)}</span>
        </button>
        <button class="tab-close" aria-label={`Close ${tab.title}`} on:click={() => requestClose(tab)}>×</button>
      </div>
    {/each}
    <button class="new-tab-button" aria-label="New tab" on:click={createSourceTab}>+</button>
  </div>

  {#if error}
    <div class="error-banner">{error}</div>
  {/if}

  {#if !activeTab}
    <section class="empty-tabs">
      <button class="primary" on:click={createSourceTab}>New tab</button>
    </section>
  {:else if activeTab.mode === 'new'}
    {#if canUseTextComparison(sourceTab)}
      <section class="comparison text-comparison">
        <div class="text-source-toolbar">
          <div>
            <button on:click={() => selectWithNativeDialog('left', 'folder')}>Choose left folder</button>
            <button on:click={() => selectWithNativeDialog('left', 'file')}>Choose left file</button>
          </div>
          <div>
            <button on:click={() => selectWithNativeDialog('right', 'folder')}>Choose right folder</button>
            <button on:click={() => selectWithNativeDialog('right', 'file')}>Choose right file</button>
          </div>
        </div>
        {#if sourceTab.textComparison.error}
          <div class="error-banner">{sourceTab.textComparison.error}</div>
        {/if}
        <div class="editor-grid-header">
          <div class="header-with-action">
            <span title={sourceTab.textComparison.leftPath || 'Left text'}>
              {sourceTab.textComparison.leftPath || 'Left text'} {textSideDirty(sourceTab, 'left') ? '•' : ''}
            </span>
            <div class="pane-actions">
              <button class="format-button" aria-label="Beautify left JSON or Python text" title="Beautify JSON or Python" disabled={!sourceTab.textComparison.left.trim()} on:click={() => beautifyText(sourceTab, 'left')}>Beautify</button>
              {#if textSideNeedsSave(sourceTab, 'left')}
                <button class="icon-button" aria-label="Save left text" title="Save left text" on:click={() => saveTextComparison(sourceTab.id, 'left')}>
                  <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M5 3h12l2 2v16H5V3Zm3 0v6h8V3H8Zm0 14h8v-5H8v5Z"/></svg>
                </button>
              {/if}
            </div>
          </div>
          <div class="header-with-action">
            <span title={sourceTab.textComparison.rightPath || 'Right text'}>
              {sourceTab.textComparison.rightPath || 'Right text'} {textSideDirty(sourceTab, 'right') ? '•' : ''}
            </span>
            <div class="difference-nav" aria-label="Difference navigation">
              <span class="difference-count" title={differenceLabel(sourceTab)}>{differenceLabel(sourceTab)}</span>
              <button class="icon-button" aria-label="Previous difference" title="Previous difference" disabled={!differenceCount(sourceTab)} on:click={() => navigateDifference('previous')}>
                <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 5 5 12h4v7h6v-7h4L12 5Z"/></svg>
              </button>
              <button class="icon-button" aria-label="Next difference" title="Next difference" disabled={!differenceCount(sourceTab)} on:click={() => navigateDifference('next')}>
                <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 19 5 12h4V5h6v7h4l-7 7Z"/></svg>
              </button>
            </div>
            <div class="pane-actions">
              <button class="format-button" aria-label="Beautify right JSON or Python text" title="Beautify JSON or Python" disabled={!sourceTab.textComparison.right.trim()} on:click={() => beautifyText(sourceTab, 'right')}>Beautify</button>
              {#if textSideNeedsSave(sourceTab, 'right')}
                <button class="icon-button" aria-label="Save right text" title="Save right text" on:click={() => saveTextComparison(sourceTab.id, 'right')}>
                  <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M5 3h12l2 2v16H5V3Zm3 0v6h8V3H8Zm0 14h8v-5H8v5Z"/></svg>
                </button>
              {/if}
            </div>
          </div>
        </div>
        <div class="code-diff-body">
          {#key sourceTab.id}
            <DiffEditor
              bind:this={diffEditor}
              leftText={sourceTab.textComparison.left}
              rightText={sourceTab.textComparison.right}
              rows={comparisonRows(sourceTab)}
              selectedRange={selectedFileRange(sourceTab)}
              onChange={(side, value) => updateEditorSource(sourceTab, side, value)}
              onSelectRange={(range) => selectEditorRange(sourceTab, range)}
              onViewportChange={updateEditorViewport}
            />
          {/key}
          <div class="diff-map-gutter" aria-label="Text difference overview">
            {#each comparisonRows(sourceTab) as row}
              <div class="diff-map-pixel" style={`background: ${mapColor(row)}`}></div>
            {/each}
            <div class="diff-map-window" style={viewportWindowStyle(sourceTab)}></div>
            <div class="diff-map-focus" style={focusIndicatorStyle(sourceTab)}></div>
          </div>
        </div>
      </section>
    {:else}
    <section class:has-text-compare={hasTextComparison(sourceTab)} class="source-picker">
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
              {#each visibleBrowserEntries(browsers.left) as entry}
                <div
                  class:selected-entry={leftSource?.path === entry.path}
                  class:locked-entry={!canSelectType(entry.type)}
                  class="browser-entry browser-tree-entry"
                >
                  <button
                    class="entry-main"
                    on:click={() => openBrowserEntry('left', entry)}
                  >
                    <span class="tree-spacer" style={`width: ${browserIndent(entry)}`}></span>
                    {#if entry.type === 'folder'}
                      <span class="tree-toggle" aria-hidden="true">
                        {browserEntryExpanded(browsers.left, entry) ? '▾' : '▸'}
                      </span>
                      <span class="folder-icon" aria-hidden="true"></span>
                    {:else}
                      <span class="tree-toggle-spacer"></span>
                    {/if}
                    <span class="entry-name" title={entry.path}>{entry.name}</span>
                    <span class="entry-type">{typeLabel(entry.type)}</span>
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
              {#each visibleBrowserEntries(browsers.right) as entry}
                <div
                  class:selected-entry={rightSource?.path === entry.path}
                  class:locked-entry={!canSelectType(entry.type)}
                  class="browser-entry browser-tree-entry"
                >
                  <button
                    class="entry-main"
                    on:click={() => openBrowserEntry('right', entry)}
                  >
                    <span class="tree-spacer" style={`width: ${browserIndent(entry)}`}></span>
                    {#if entry.type === 'folder'}
                      <span class="tree-toggle" aria-hidden="true">
                        {browserEntryExpanded(browsers.right, entry) ? '▾' : '▸'}
                      </span>
                      <span class="folder-icon" aria-hidden="true"></span>
                    {:else}
                      <span class="tree-toggle-spacer"></span>
                    {/if}
                    <span class="entry-name" title={entry.path}>{entry.name}</span>
                    <span class="entry-type">{typeLabel(entry.type)}</span>
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
      {#if hasTextComparison(sourceTab)}
        <section class="text-compare-preview" aria-label="Pasted text comparison">
          {#if sourceTab.textComparison.error}
            <div class="error-panel">{sourceTab.textComparison.error}</div>
          {:else if sourceTab.textComparison.loading && !sourceTab.textComparison.result}
            <div class="loading">Comparing…</div>
          {:else if sourceTab.textComparison.result}
            <div class="editor-grid-header">
              <div>Left text</div>
              <div class="header-with-action">
                <span>Right text</span>
                <div class="difference-nav" aria-label="Difference navigation">
                  <span class="difference-count" title={differenceLabel(sourceTab)}>{differenceLabel(sourceTab)}</span>
                  <button class="icon-button" aria-label="Previous difference" title="Previous difference" disabled={!differenceCount(sourceTab)} on:click={() => navigateDifference('previous')}>
                    <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 5 5 12h4v7h6v-7h4L12 5Z"/></svg>
                  </button>
                  <button class="icon-button" aria-label="Next difference" title="Next difference" disabled={!differenceCount(sourceTab)} on:click={() => navigateDifference('next')}>
                    <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 19 5 12h4V5h6v7h4l-7 7Z"/></svg>
                  </button>
                </div>
              </div>
            </div>
            <div class="code-diff-body">
              {#key sourceTab.id}
                <DiffEditor
                  bind:this={diffEditor}
                  leftText={sourceTab.textComparison.left}
                  rightText={sourceTab.textComparison.right}
                  rows={comparisonRows(sourceTab)}
                  selectedRange={selectedFileRange(sourceTab)}
                  onChange={(side, value) => updateEditorSource(sourceTab, side, value)}
                  onSelectRange={(range) => selectEditorRange(sourceTab, range)}
                  onViewportChange={updateEditorViewport}
                />
              {/key}
              <div class="diff-map-gutter" aria-label="Text difference overview">
                {#each comparisonRows(sourceTab) as row}
                  <div class="diff-map-pixel" style={`background: ${mapColor(row)}`}></div>
                {/each}
                <div class="diff-map-window" style={viewportWindowStyle(sourceTab)}></div>
                <div class="diff-map-focus" style={focusIndicatorStyle(sourceTab)}></div>
              </div>
            </div>
          {/if}
        </section>
      {/if}
    </section>
    {/if}
  {:else}
    <section class="comparison">
      <div class="path-strip">
        <span title={activeTab.leftPath}>{activeTab.leftPath}</span>
        <span title={activeTab.rightPath}>{activeTab.rightPath}</span>
        <button
          class="refresh-button"
          title="Refresh comparison"
          disabled={activeTab.loading || folderComparisonRunning(activeTab)}
          on:click={refreshActive}
        >{refreshLabel(activeTab)}</button>
        {#if externalChangeSides(activeTab).length}
          <div class="external-change-banner" role="alert">
            <span>{externalChangeMessage(activeTab)}</span>
            <div>
              <button on:click={() => reloadExternalChanges(activeTab)}>Refresh from disk</button>
              <button on:click={() => keepExternalEdits(activeTab)}>Keep my edits</button>
              {#if tabDirty(activeTab)}
                <button on:click={() => saveAfterExternalChange(activeTab)}>Save my edits</button>
              {/if}
            </div>
          </div>
        {/if}
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
              {#each visibleFolderRows(activeTab) as row}
                <div
                  role="button"
                  tabindex="0"
                  class:selected={rowSelected(activeTab, row.rowIndex)}
                  class:odd-row={row.rowIndex % 2 === 1}
                  class={`compare-side-row folder-cell status-${row.status}`}
                  on:click={(event) => clickFolderSideRow(activeTab, row, event)}
                  on:keydown={(event) => keySelectFolder(event, activeTab, row.rowIndex, row)}
                  on:dblclick={() => openFolderFile(row)}
                  on:contextmenu={(event) => showContext(event, activeTab, row, row.rowIndex, 'left')}
                >
                  {#if nodeExists(row, 'left')}
                    <span class="tree-spacer" style={`width: ${folderIndent(row)}`}></span>
                    {#if rowIsFolder(row)}
                      <button class="tree-toggle" aria-label={`${folderRowExpanded(activeTab, row) ? 'Collapse' : 'Expand'} ${row.name}`} on:click={(event) => toggleFolderNode(activeTab, row, event)}>
                        {folderRowExpanded(activeTab, row) ? '▾' : '▸'}
                      </button>
                    {:else}
                      <span class="tree-toggle-spacer"></span>
                    {/if}
                    {#if sideIsFolder(row, 'left')}
                      <span class="folder-icon" aria-hidden="true"></span>
                    {/if}
                    <span class="entry-name" title={nodePath(row, 'left')}>{row.name}</span>
                    <span class="entry-type">{typeLabel(nodeType(row, 'left'))}</span>
                  {/if}
                </div>
              {/each}
            </div>
            <div class="split-pane center-scroll-pane folder-center-pane" bind:this={centerComparePane} on:scroll={syncComparisonScroll}>
              {#each visibleFolderRows(activeTab) as row}
                <div
                  role="button"
                  tabindex="0"
                  class:selected={rowSelected(activeTab, row.rowIndex)}
                  class:odd-row={row.rowIndex % 2 === 1}
                  class={`compare-status-row status-${row.status}`}
                  title={row.error || statusLabel(row.status)}
                  on:click={(event) => clickFolderSideRow(activeTab, row, event)}
                  on:keydown={(event) => keySelectFolder(event, activeTab, row.rowIndex, row)}
                  on:contextmenu={(event) => showContext(event, activeTab, row, row.rowIndex, 'center')}
                >
                  {indicator(row.status)}
                </div>
              {/each}
            </div>
            <div class="split-pane side-pane folder-side-pane" bind:this={rightComparePane} on:wheel={scrollComparisonFromSide}>
              {#each visibleFolderRows(activeTab) as row}
                <div
                  role="button"
                  tabindex="0"
                  class:selected={rowSelected(activeTab, row.rowIndex)}
                  class:odd-row={row.rowIndex % 2 === 1}
                  class={`compare-side-row folder-cell status-${row.status}`}
                  on:click={(event) => clickFolderSideRow(activeTab, row, event)}
                  on:keydown={(event) => keySelectFolder(event, activeTab, row.rowIndex, row)}
                  on:dblclick={() => openFolderFile(row)}
                  on:contextmenu={(event) => showContext(event, activeTab, row, row.rowIndex, 'right')}
                >
                  {#if nodeExists(row, 'right')}
                    <span class="tree-spacer" style={`width: ${folderIndent(row)}`}></span>
                    {#if rowIsFolder(row)}
                      <button class="tree-toggle" aria-label={`${folderRowExpanded(activeTab, row) ? 'Collapse' : 'Expand'} ${row.name}`} on:click={(event) => toggleFolderNode(activeTab, row, event)}>
                        {folderRowExpanded(activeTab, row) ? '▾' : '▸'}
                      </button>
                    {:else}
                      <span class="tree-toggle-spacer"></span>
                    {/if}
                    {#if sideIsFolder(row, 'right')}
                      <span class="folder-icon" aria-hidden="true"></span>
                    {/if}
                    <span class="entry-name" title={nodePath(row, 'right')}>{row.name}</span>
                    <span class="entry-type">{typeLabel(nodeType(row, 'right'))}</span>
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
        <div class="editor-grid-header">
          <div class="header-with-action">
            <span title={activeTab.leftPath}>Left file {activeTab.result?.leftDirty ? '•' : ''}</span>
            {#if activeTab.result?.leftDirty}
              <button class="icon-button" aria-label="Save left file" title="Save left file" on:click={() => saveActive('left')}>
                <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M5 3h12l2 2v16H5V3Zm3 0v6h8V3H8Zm0 14h8v-5H8v5Z"/></svg>
              </button>
            {/if}
          </div>
          <div class="header-with-action">
            <span title={activeTab.rightPath}>Right file {activeTab.result?.rightDirty ? '•' : ''}</span>
            <div class="difference-nav" aria-label="Difference navigation">
              <span class="difference-count" title={differenceLabel(activeTab)}>{differenceLabel(activeTab)}</span>
              <button class="icon-button" aria-label="Previous difference" title="Previous difference" disabled={!differenceCount(activeTab)} on:click={() => navigateDifference('previous')}>
                <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 5 5 12h4v7h6v-7h4L12 5Z"/></svg>
              </button>
              <button class="icon-button" aria-label="Next difference" title="Next difference" disabled={!differenceCount(activeTab)} on:click={() => navigateDifference('next')}>
                <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 19 5 12h4V5h6v7h4l-7 7Z"/></svg>
              </button>
            </div>
            {#if activeTab.result?.rightDirty}
              <button class="icon-button" aria-label="Save right file" title="Save right file" on:click={() => saveActive('right')}>
                <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M5 3h12l2 2v16H5V3Zm3 0v6h8V3H8Zm0 14h8v-5H8v5Z"/></svg>
              </button>
            {/if}
          </div>
        </div>
        <div class="code-diff-body">
          {#key activeTab.id}
            <DiffEditor
              bind:this={diffEditor}
              leftText={sideTextValue(activeTab, 'left')}
              rightText={sideTextValue(activeTab, 'right')}
              rows={comparisonRows(activeTab)}
              selectedRange={selectedFileRange(activeTab)}
              onChange={(side, value) => updateEditorSource(activeTab, side, value)}
              onSelectRange={(range) => selectEditorRange(activeTab, range)}
              onContextMenu={(details) => showEditorContext(activeTab, details)}
              onViewportChange={updateEditorViewport}
            />
          {/key}
          <div class="diff-map-gutter" aria-label="Difference overview">
            {#each comparisonRows(activeTab) as row}
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
        {#if rowIsFolder(contextMenu.row)}
          <button disabled={folderComparisonRunning(getTab(contextMenu.tabID))} on:click={refreshContextFolder}>Refresh this folder</button>
        {/if}
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
