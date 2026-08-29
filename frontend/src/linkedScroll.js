function maxScroll(scroller, axis) {
  return axis === 'x'
    ? Math.max(0, scroller.scrollWidth - scroller.clientWidth)
    : Math.max(0, scroller.scrollHeight - scroller.clientHeight);
}

function position(scroller, axis) {
  return axis === 'x' ? scroller.scrollLeft : scroller.scrollTop;
}

function setPosition(scroller, axis, value) {
  if (axis === 'x') {
    scroller.scrollLeft = value;
  } else {
    scroller.scrollTop = value;
  }
}

function sharedDelta(scrollers, requested, axis) {
  if (!scrollers.length || !requested) return 0;
  const minimum = Math.max(...scrollers.map((scroller) => -position(scroller, axis)));
  const maximum = Math.min(...scrollers.map((scroller) => maxScroll(scroller, axis) - position(scroller, axis)));
  return Math.max(minimum, Math.min(maximum, requested));
}

export function applyLinkedScrollDelta(scrollers, deltaX, deltaY) {
  const active = scrollers.filter(Boolean);
  const appliedX = sharedDelta(active, deltaX, 'x');
  const appliedY = sharedDelta(active, deltaY, 'y');
  for (const scroller of active) {
    setPosition(scroller, 'x', position(scroller, 'x') + appliedX);
    setPosition(scroller, 'y', position(scroller, 'y') + appliedY);
  }
  return { deltaX: appliedX, deltaY: appliedY };
}

export function syncLinkedScrollPosition(scrollers, source) {
  const active = scrollers.filter(Boolean);
  if (!source || !active.length) return { scrollLeft: 0, scrollTop: 0 };
  const maximumLeft = Math.min(...active.map((scroller) => maxScroll(scroller, 'x')));
  const maximumTop = Math.min(...active.map((scroller) => maxScroll(scroller, 'y')));
  const scrollLeft = Math.max(0, Math.min(maximumLeft, source.scrollLeft));
  const scrollTop = Math.max(0, Math.min(maximumTop, source.scrollTop));
  for (const scroller of active) {
    if (scroller.scrollLeft !== scrollLeft) scroller.scrollLeft = scrollLeft;
    if (scroller.scrollTop !== scrollTop) scroller.scrollTop = scrollTop;
  }
  return { scrollLeft, scrollTop };
}

export function wheelDeltaPixels(event, scroller, lineHeight = 20) {
  let scaleX = 1;
  let scaleY = 1;
  if (event.deltaMode === 1) {
    scaleX = lineHeight;
    scaleY = lineHeight;
  } else if (event.deltaMode === 2) {
    scaleX = scroller?.clientWidth || 1;
    scaleY = scroller?.clientHeight || 1;
  }
  let deltaX = event.deltaX * scaleX;
  let deltaY = event.deltaY * scaleY;
  if (event.shiftKey && deltaX === 0) {
    deltaX = deltaY;
    deltaY = 0;
  }
  return { deltaX, deltaY };
}
